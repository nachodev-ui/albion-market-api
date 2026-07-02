[CmdletBinding()]
param(
    [string]$AdminDatabaseUrl = $env:POSTGRES_ADMIN_URL,
    [switch]$KeepDatabaseOnFailure
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot

function Get-DatabaseUrlFromLocalEnvironment {
    param([string]$CurrentValue)

    if (-not [string]::IsNullOrWhiteSpace($CurrentValue)) {
        return $CurrentValue
    }

    if (-not [string]::IsNullOrWhiteSpace($env:DATABASE_URL)) {
        return $env:DATABASE_URL
    }

    $localEnvPath = Join-Path $repoRoot '.env.local'
    if (-not (Test-Path -LiteralPath $localEnvPath -PathType Leaf)) {
        return $null
    }

    $databaseLine = Get-Content -LiteralPath $localEnvPath |
        Where-Object { $_ -match '^\s*DATABASE_URL\s*=' } |
        Select-Object -First 1

    if ($null -eq $databaseLine) {
        return $null
    }

    $value = ($databaseLine -split '=', 2)[1].Trim()
    if (
        $value.Length -ge 2 -and
        (($value.StartsWith('"') -and $value.EndsWith('"')) -or
         ($value.StartsWith("'") -and $value.EndsWith("'")))
    ) {
        $value = $value.Substring(1, $value.Length - 2)
    }

    return $value
}

function Set-DatabaseNameInUrl {
    param(
        [string]$Url,
        [string]$DatabaseName
    )

    try {
        $builder = New-Object System.UriBuilder -ArgumentList $Url
        $builder.Path = "/$DatabaseName"
        return $builder.Uri.AbsoluteUri
    } catch {
        throw 'AdminDatabaseUrl must be a valid postgres:// or postgresql:// URL.'
    }
}

function Invoke-PsqlFile {
    param(
        [string]$DatabaseUrl,
        [string]$Path,
        [hashtable]$Variables = @{}
    )

    $arguments = @(
        '-X'
        '-q'
        '--set', 'ON_ERROR_STOP=1'
        '--dbname', $DatabaseUrl
        '--file', $Path
    )

    foreach ($key in ($Variables.Keys | Sort-Object)) {
        $arguments += @('--set', "$key=$($Variables[$key])")
    }

    & $script:PsqlPath @arguments
    if ($LASTEXITCODE -ne 0) {
        throw "psql failed while executing $Path."
    }
}

function Invoke-PsqlCommand {
    param(
        [string]$DatabaseUrl,
        [string]$Sql
    )

    & $script:PsqlPath -X -q --set ON_ERROR_STOP=1 --dbname $DatabaseUrl --command $Sql
    if ($LASTEXITCODE -ne 0) {
        throw 'psql command failed.'
    }
}

function Invoke-PsqlScalar {
    param(
        [string]$DatabaseUrl,
        [string]$Sql
    )

    $output = & $script:PsqlPath -X -q -A -t --set ON_ERROR_STOP=1 --dbname $DatabaseUrl --command $Sql 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "psql scalar query failed: $($output -join [Environment]::NewLine)"
    }

    $lines = @(
        $output |
            ForEach-Object { $_.ToString().Trim() } |
            Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    )

    if ($lines.Count -eq 0) {
        throw 'psql scalar query returned no value.'
    }

    return $lines[$lines.Count - 1]
}

function Assert-Equal {
    param(
        [object]$Expected,
        [object]$Actual,
        [string]$Message
    )

    if ([string]$Expected -ne [string]$Actual) {
        throw "Assertion failed: $Message. Expected=$Expected Actual=$Actual"
    }
}

function Get-SummaryRow {
    param(
        [object[]]$Summary,
        [string]$Table
    )

    $row = $Summary | Where-Object { $_.Table -eq $Table } | Select-Object -First 1
    if ($null -eq $row) {
        throw "Summary row was not found for $Table."
    }
    return $row
}

$AdminDatabaseUrl = Get-DatabaseUrlFromLocalEnvironment -CurrentValue $AdminDatabaseUrl
if ([string]::IsNullOrWhiteSpace($AdminDatabaseUrl)) {
    throw 'POSTGRES_ADMIN_URL or DATABASE_URL is required. It must allow CREATE DATABASE and DROP DATABASE.'
}

$psql = Get-Command psql -ErrorAction SilentlyContinue
if ($null -eq $psql) {
    throw 'psql was not found in PATH.'
}
$script:PsqlPath = $psql.Source

$databaseName = "albion_market_retention_test_$((Get-Date).ToString('yyyyMMdd_HHmmss'))_$PID"
$testDatabaseUrl = Set-DatabaseNameInUrl -Url $AdminDatabaseUrl -DatabaseName $databaseName
$retentionScript = Join-Path $PSScriptRoot 'postgres-retention.ps1'
$seedSql = Join-Path $PSScriptRoot 'sql\postgres-retention-test-seed.sql'
$testOutputDirectory = Join-Path $repoRoot "artifacts\postgres-retention-test\$databaseName"
$referenceTime = [DateTimeOffset]::Parse('2026-07-02T12:00:00Z')
$created = $false
$succeeded = $false

try {
    Write-Host "Creating disposable database $databaseName..."
    Invoke-PsqlCommand -DatabaseUrl $AdminDatabaseUrl -Sql "create database `"$databaseName`";"
    $created = $true

    Write-Host 'Applying migrations...'
    Get-ChildItem -LiteralPath (Join-Path $repoRoot 'migrations') -Filter '*.sql' |
        Sort-Object Name |
        ForEach-Object {
            Write-Host "  $($_.Name)"
            Invoke-PsqlFile -DatabaseUrl $testDatabaseUrl -Path $_.FullName
        }

    Write-Host 'Loading deterministic retention fixtures...'
    Invoke-PsqlFile -DatabaseUrl $testDatabaseUrl -Path $seedSql -Variables @{
        reference_time = $referenceTime.ToString("yyyy-MM-ddTHH:mm:ss'Z'")
    }

    foreach ($indexName in @(
        'market_history_ingest_raw_received_at_id_idx',
        'market_ingest_raw_received_at_id_idx',
        'market_history_buckets_bucket_at_idx'
    )) {
        $exists = Invoke-PsqlScalar -DatabaseUrl $testDatabaseUrl -Sql "select count(*) from pg_class where relname = '$indexName' and relkind = 'i';"
        Assert-Equal 1 $exists "retention index $indexName exists"
    }

    Write-Host 'Running dry-run...'
    $dryRunSummary = @(
        & $retentionScript `
            -DatabaseUrl $testDatabaseUrl `
            -Mode DryRun `
            -ReferenceTimeUtc $referenceTime `
            -BatchSize 1 `
            -PauseMilliseconds 0 `
            -OutputDirectory $testOutputDirectory `
            -PassThru
    )

    $dryHistoryRaw = Get-SummaryRow $dryRunSummary 'market_history_ingest_raw'
    $dryPriceRaw = Get-SummaryRow $dryRunSummary 'market_ingest_raw'
    $dryHistoryRequests = Get-SummaryRow $dryRunSummary 'market_history_ingest_requests'
    $dryPriceRequests = Get-SummaryRow $dryRunSummary 'market_ingest_requests'
    $dryHistoryBuckets = Get-SummaryRow $dryRunSummary 'market_history_buckets'

    Assert-Equal 2 $dryHistoryRaw.EligibleBefore 'history raw dry-run candidates'
    Assert-Equal 2 $dryPriceRaw.EligibleBefore 'price raw dry-run candidates'
    Assert-Equal 1 $dryHistoryRequests.EligibleBefore 'history request dry-run candidates after simulated raw cleanup'
    Assert-Equal 1 $dryPriceRequests.EligibleBefore 'price request dry-run candidates after simulated raw cleanup'
    Assert-Equal 1 $dryHistoryBuckets.EligibleBefore 'history bucket dry-run candidates'
    foreach ($row in $dryRunSummary) {
        Assert-Equal 0 $row.Deleted "dry-run deleted rows for $($row.Table)"
        Assert-Equal 'dry-run' $row.Status "dry-run status for $($row.Table)"
    }

    Assert-Equal 4 (Invoke-PsqlScalar $testDatabaseUrl 'select count(*) from market_history_ingest_raw;') 'dry-run preserves history raw rows'
    Assert-Equal 4 (Invoke-PsqlScalar $testDatabaseUrl 'select count(*) from market_ingest_raw;') 'dry-run preserves price raw rows'
    Assert-Equal 4 (Invoke-PsqlScalar $testDatabaseUrl 'select count(*) from market_history_ingest_requests;') 'dry-run preserves history requests'
    Assert-Equal 4 (Invoke-PsqlScalar $testDatabaseUrl 'select count(*) from market_ingest_requests;') 'dry-run preserves price requests'
    Assert-Equal 3 (Invoke-PsqlScalar $testDatabaseUrl 'select count(*) from market_history_buckets;') 'dry-run preserves history buckets'
    Assert-Equal 1 (Invoke-PsqlScalar $testDatabaseUrl 'select count(*) from current_market_prices;') 'dry-run preserves current prices'

    Write-Host 'Running apply mode with one-row batches...'
    $applySummary = @(
        & $retentionScript `
            -DatabaseUrl $testDatabaseUrl `
            -Mode Apply `
            -ReferenceTimeUtc $referenceTime `
            -BatchSize 1 `
            -PauseMilliseconds 0 `
            -OutputDirectory $testOutputDirectory `
            -PassThru
    )

    $applyHistoryRaw = Get-SummaryRow $applySummary 'market_history_ingest_raw'
    $applyPriceRaw = Get-SummaryRow $applySummary 'market_ingest_raw'
    $applyHistoryRequests = Get-SummaryRow $applySummary 'market_history_ingest_requests'
    $applyPriceRequests = Get-SummaryRow $applySummary 'market_ingest_requests'
    $applyHistoryBuckets = Get-SummaryRow $applySummary 'market_history_buckets'

    Assert-Equal 2 $applyHistoryRaw.Deleted 'history raw rows deleted'
    Assert-Equal 2 $applyPriceRaw.Deleted 'price raw rows deleted'
    Assert-Equal 1 $applyHistoryRequests.Deleted 'history requests deleted'
    Assert-Equal 1 $applyPriceRequests.Deleted 'price requests deleted'
    Assert-Equal 1 $applyHistoryBuckets.Deleted 'history buckets deleted'
    Assert-Equal 2 $applyHistoryRaw.Batches 'history raw uses multiple small transactions'
    Assert-Equal 2 $applyPriceRaw.Batches 'price raw uses multiple small transactions'
    foreach ($row in $applySummary) {
        Assert-Equal 0 $row.EligibleAfter "no eligible rows remain for $($row.Table)"
        Assert-Equal 'completed' $row.Status "apply status for $($row.Table)"
    }

    Assert-Equal 2 (Invoke-PsqlScalar $testDatabaseUrl 'select count(*) from market_history_ingest_raw;') 'history raw rows after apply'
    Assert-Equal 2 (Invoke-PsqlScalar $testDatabaseUrl 'select count(*) from market_ingest_raw;') 'price raw rows after apply'
    Assert-Equal 3 (Invoke-PsqlScalar $testDatabaseUrl 'select count(*) from market_history_ingest_requests;') 'history requests after apply'
    Assert-Equal 3 (Invoke-PsqlScalar $testDatabaseUrl 'select count(*) from market_ingest_requests;') 'price requests after apply'
    Assert-Equal 2 (Invoke-PsqlScalar $testDatabaseUrl 'select count(*) from market_history_buckets;') 'history buckets after apply'
    Assert-Equal 1 (Invoke-PsqlScalar $testDatabaseUrl 'select count(*) from current_market_prices;') 'current prices remain untouched'

    Assert-Equal 0 (Invoke-PsqlScalar $testDatabaseUrl "select count(*) from market_history_ingest_requests where request_id = '00000000-0000-0000-0000-000000000001';") 'old orphan history request removed'
    Assert-Equal 1 (Invoke-PsqlScalar $testDatabaseUrl "select count(*) from market_history_ingest_requests where request_id = '00000000-0000-0000-0000-000000000003' and status = 'processing';") 'processing history request preserved'
    Assert-Equal 0 (Invoke-PsqlScalar $testDatabaseUrl "select count(*) from market_ingest_requests where request_id = '10000000-0000-0000-0000-000000000001';") 'old orphan price request removed'
    Assert-Equal 1 (Invoke-PsqlScalar $testDatabaseUrl "select count(*) from market_ingest_requests where request_id = '10000000-0000-0000-0000-000000000003' and status = 'processing';") 'processing price request preserved'
    Assert-Equal 1 (Invoke-PsqlScalar $testDatabaseUrl "select count(*) from market_history_buckets where item_key = 'T4_BUCKET_BOUNDARY';") '400-day boundary bucket preserved'
    Assert-Equal 0 (Invoke-PsqlScalar $testDatabaseUrl "select count(*) from market_history_buckets where item_key = 'T4_BUCKET_OLD';") '401-day bucket removed'

    $succeeded = $true
    Write-Host 'Retention integration test passed.'
} finally {
    if ($created) {
        if (-not $succeeded -and $KeepDatabaseOnFailure) {
            Write-Warning "Test failed. Disposable database was kept for inspection: $databaseName"
        } else {
            Write-Host "Dropping disposable database $databaseName..."
            try {
                Invoke-PsqlCommand -DatabaseUrl $AdminDatabaseUrl -Sql "drop database if exists `"$databaseName`" with (force);"
            } catch {
                if ($succeeded) {
                    throw
                }
                Write-Warning "The test also failed to drop disposable database ${databaseName}: $($_.Exception.Message)"
            }
        }
    }
}
