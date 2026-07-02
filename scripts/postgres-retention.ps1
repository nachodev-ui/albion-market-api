[CmdletBinding()]
param(
    [ValidateSet('DryRun', 'Apply')]
    [string]$Mode = 'DryRun',

    [string]$DatabaseUrl = $env:DATABASE_URL,

    [ValidateRange(1, 36500)]
    [int]$MarketHistoryRawRetentionDays = 30,

    [ValidateRange(1, 36500)]
    [int]$MarketRawRetentionDays = 30,

    [ValidateRange(1, 36500)]
    [int]$MarketHistoryRequestsRetentionDays = 90,

    [ValidateRange(1, 36500)]
    [int]$MarketRequestsRetentionDays = 90,

    [ValidateRange(366, 36500)]
    [int]$MarketHistoryBucketsRetentionDays = 400,

    [ValidateRange(1, 100000)]
    [int]$BatchSize = 5000,

    [ValidateRange(0, 60000)]
    [int]$PauseMilliseconds = 100,

    [ValidateRange(1, 3600)]
    [int]$LockTimeoutSeconds = 5,

    [ValidateRange(1, 86400)]
    [int]$StatementTimeoutSeconds = 120,

    [ValidateRange(0, 1000000)]
    [int]$MaxBatchesPerTable = 0,

    [DateTimeOffset]$ReferenceTimeUtc = [DateTimeOffset]::UtcNow,

    [string]$OutputDirectory,

    [switch]$PassThru
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot

function Get-DatabaseUrlFromLocalEnvironment {
    param(
        [string]$CurrentValue,
        [string]$Root
    )

    if (-not [string]::IsNullOrWhiteSpace($CurrentValue)) {
        return $CurrentValue
    }

    $localEnvPath = Join-Path $Root '.env.local'
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

function Convert-ToPostgresTimestamp {
    param([DateTimeOffset]$Value)

    return $Value.ToUniversalTime().ToString(
        "yyyy-MM-ddTHH:mm:ss.ffffff'Z'",
        [System.Globalization.CultureInfo]::InvariantCulture
    )
}

function Invoke-PsqlScalar {
    param(
        [string]$SqlPath,
        [hashtable]$Variables
    )

    $arguments = @(
        '-X'
        '-q'
        '-A'
        '-t'
        '--set', 'ON_ERROR_STOP=1'
        '--dbname', $script:DatabaseUrlResolved
        '--file', $SqlPath
    )

    foreach ($key in ($Variables.Keys | Sort-Object)) {
        $arguments += @('--set', "$key=$($Variables[$key])")
    }

    $rawOutput = & $script:PsqlPath @arguments 2>&1
    $exitCode = $LASTEXITCODE

    if ($exitCode -ne 0) {
        $message = ($rawOutput | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
        throw "psql failed with exit code $exitCode while executing $SqlPath.$([Environment]::NewLine)$message"
    }

    $lines = @(
        $rawOutput |
            ForEach-Object { $_.ToString().Trim() } |
            Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    )

    if ($lines.Count -eq 0) {
        throw "psql returned no scalar value while executing $SqlPath."
    }

    [long]$value = 0
    if (-not [long]::TryParse($lines[$lines.Count - 1], [ref]$value)) {
        throw "psql returned a non-numeric scalar value: $($lines[$lines.Count - 1])"
    }

    return $value
}

function Get-EligibleCount {
    param(
        [pscustomobject]$Plan,
        [bool]$SimulateRawCleanup
    )

    $rawCutoff = if ($null -ne $Plan.RawCutoffUtc) {
        Convert-ToPostgresTimestamp $Plan.RawCutoffUtc
    } else {
        Convert-ToPostgresTimestamp $Plan.CutoffUtc
    }

    $simulateRawCleanupValue = if ($SimulateRawCleanup) { 'true' } else { 'false' }

    return Invoke-PsqlScalar -SqlPath $script:CountSqlPath -Variables @{
        target               = $Plan.Target
        cutoff               = Convert-ToPostgresTimestamp $Plan.CutoffUtc
        raw_cutoff           = $rawCutoff
        simulate_raw_cleanup = $simulateRawCleanupValue
    }
}

function Remove-RetentionBatch {
    param([pscustomobject]$Plan)

    return Invoke-PsqlScalar -SqlPath $script:DeleteSqlPath -Variables @{
        target            = $Plan.Target
        cutoff            = Convert-ToPostgresTimestamp $Plan.CutoffUtc
        batch_size        = $BatchSize
        lock_timeout      = "${LockTimeoutSeconds}s"
        statement_timeout = "${StatementTimeoutSeconds}s"
    }
}

if ($MarketHistoryRequestsRetentionDays -lt $MarketHistoryRawRetentionDays) {
    throw 'MarketHistoryRequestsRetentionDays must be greater than or equal to MarketHistoryRawRetentionDays.'
}

if ($MarketRequestsRetentionDays -lt $MarketRawRetentionDays) {
    throw 'MarketRequestsRetentionDays must be greater than or equal to MarketRawRetentionDays.'
}

$script:DatabaseUrlResolved = Get-DatabaseUrlFromLocalEnvironment -CurrentValue $DatabaseUrl -Root $repoRoot
if ([string]::IsNullOrWhiteSpace($script:DatabaseUrlResolved)) {
    throw 'DATABASE_URL is not defined. Use -DatabaseUrl, set the environment variable, or add it to .env.local.'
}

$psql = Get-Command psql -ErrorAction SilentlyContinue
if ($null -eq $psql) {
    throw 'psql was not found in PATH. Install the PostgreSQL client tools first.'
}
$script:PsqlPath = $psql.Source

$script:PreflightSqlPath = Join-Path $PSScriptRoot 'sql\postgres-retention-preflight.sql'
$script:CountSqlPath = Join-Path $PSScriptRoot 'sql\postgres-retention-count.sql'
$script:DeleteSqlPath = Join-Path $PSScriptRoot 'sql\postgres-retention-delete-batch.sql'
foreach ($requiredPath in @($script:PreflightSqlPath, $script:CountSqlPath, $script:DeleteSqlPath)) {
    if (-not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) {
        throw "Required SQL file was not found: $requiredPath"
    }
}

if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $repoRoot 'artifacts\postgres-retention'
}
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null

$runId = "$((Get-Date).ToString('yyyyMMdd-HHmmss-fff'))-$PID"
$textLogPath = Join-Path $OutputDirectory "postgres-retention-$runId.log"
$csvPath = Join-Path $OutputDirectory "postgres-retention-$runId.csv"
$jsonPath = Join-Path $OutputDirectory "postgres-retention-$runId.json"

function Write-RunMessage {
    param([string]$Message)

    $line = "[$((Get-Date).ToString('s'))] $Message"
    Write-Host $line
    Add-Content -LiteralPath $textLogPath -Value $line -Encoding UTF8
}

$referenceUtc = $ReferenceTimeUtc.ToUniversalTime()
$historyRawCutoff = $referenceUtc.AddDays(-$MarketHistoryRawRetentionDays)
$priceRawCutoff = $referenceUtc.AddDays(-$MarketRawRetentionDays)

$plans = @(
    [pscustomobject]@{
        Target        = 'market_history_ingest_raw'
        Timestamp     = 'received_at'
        RetentionDays = $MarketHistoryRawRetentionDays
        CutoffUtc     = $historyRawCutoff
        RawCutoffUtc  = $null
    },
    [pscustomobject]@{
        Target        = 'market_ingest_raw'
        Timestamp     = 'received_at'
        RetentionDays = $MarketRawRetentionDays
        CutoffUtc     = $priceRawCutoff
        RawCutoffUtc  = $null
    },
    [pscustomobject]@{
        Target        = 'market_history_ingest_requests'
        Timestamp     = 'created_at'
        RetentionDays = $MarketHistoryRequestsRetentionDays
        CutoffUtc     = $referenceUtc.AddDays(-$MarketHistoryRequestsRetentionDays)
        RawCutoffUtc  = $historyRawCutoff
    },
    [pscustomobject]@{
        Target        = 'market_ingest_requests'
        Timestamp     = 'created_at'
        RetentionDays = $MarketRequestsRetentionDays
        CutoffUtc     = $referenceUtc.AddDays(-$MarketRequestsRetentionDays)
        RawCutoffUtc  = $priceRawCutoff
    },
    [pscustomobject]@{
        Target        = 'market_history_buckets'
        Timestamp     = 'bucket_at'
        RetentionDays = $MarketHistoryBucketsRetentionDays
        CutoffUtc     = $referenceUtc.AddDays(-$MarketHistoryBucketsRetentionDays)
        RawCutoffUtc  = $null
    }
)

$summary = @()

try {
    Write-RunMessage "Starting PostgreSQL retention. Mode=$Mode ReferenceUtc=$(Convert-ToPostgresTimestamp $referenceUtc) BatchSize=$BatchSize PauseMs=$PauseMilliseconds"
    Write-RunMessage 'current_market_prices is explicitly excluded from retention.'

    $missingObjects = Invoke-PsqlScalar -SqlPath $script:PreflightSqlPath -Variables @{}
    if ($missingObjects -ne 0) {
        throw "Retention preflight found $missingObjects missing tables or indexes. Apply migrations through 000005_retention_indexes.sql first."
    }
    Write-RunMessage 'Preflight passed: required tables and retention indexes are present.'

    foreach ($plan in $plans) {
        $simulateRawCleanup = ($Mode -eq 'DryRun' -and $plan.Target -like '*_requests')
        $eligibleBefore = Get-EligibleCount -Plan $plan -SimulateRawCleanup $simulateRawCleanup
        $deleted = [long]0
        $batches = 0
        $status = 'dry-run'

        Write-RunMessage "Target=$($plan.Target) Column=$($plan.Timestamp) RetentionDays=$($plan.RetentionDays) CutoffUtc=$(Convert-ToPostgresTimestamp $plan.CutoffUtc) Eligible=$eligibleBefore"

        if ($Mode -eq 'Apply') {
            $status = 'completed'

            while ($true) {
                if ($MaxBatchesPerTable -gt 0 -and $batches -ge $MaxBatchesPerTable) {
                    $status = 'batch-limit-reached'
                    break
                }

                $deletedInBatch = Remove-RetentionBatch -Plan $plan
                if ($deletedInBatch -eq 0) {
                    break
                }

                $batches++
                $deleted += $deletedInBatch
                Write-RunMessage "Target=$($plan.Target) Batch=$batches DeletedInBatch=$deletedInBatch DeletedTotal=$deleted"

                if ($PauseMilliseconds -gt 0) {
                    Start-Sleep -Milliseconds $PauseMilliseconds
                }
            }
        }

        $eligibleAfter = if ($Mode -eq 'DryRun') {
            $eligibleBefore
        } else {
            Get-EligibleCount -Plan $plan -SimulateRawCleanup $false
        }

        if ($Mode -eq 'Apply' -and $eligibleAfter -gt 0 -and $status -eq 'completed') {
            $status = 'eligible-rows-remain'
        }

        $summary += [pscustomobject]@{
            Table           = $plan.Target
            TimestampColumn = $plan.Timestamp
            RetentionDays   = $plan.RetentionDays
            CutoffUtc       = Convert-ToPostgresTimestamp $plan.CutoffUtc
            EligibleBefore  = $eligibleBefore
            Deleted         = $deleted
            EligibleAfter   = $eligibleAfter
            Batches         = $batches
            Status          = $status
        }
    }

    $summary | Export-Csv -LiteralPath $csvPath -NoTypeInformation -Encoding UTF8

    $report = [ordered]@{
        run_id                         = $runId
        mode                           = $Mode
        reference_time_utc             = Convert-ToPostgresTimestamp $referenceUtc
        batch_size                     = $BatchSize
        pause_milliseconds             = $PauseMilliseconds
        lock_timeout_seconds           = $LockTimeoutSeconds
        statement_timeout_seconds      = $StatementTimeoutSeconds
        max_batches_per_table          = $MaxBatchesPerTable
        current_market_prices_retention = 'disabled'
        summary                        = @($summary)
    }
    $report | ConvertTo-Json -Depth 6 | Set-Content -LiteralPath $jsonPath -Encoding UTF8

    $tableText = $summary |
        Format-Table Table, TimestampColumn, RetentionDays, EligibleBefore, Deleted, EligibleAfter, Batches, Status -AutoSize |
        Out-String
    Write-RunMessage 'Retention summary:'
    Write-Host $tableText.TrimEnd()
    Add-Content -LiteralPath $textLogPath -Value $tableText.TrimEnd() -Encoding UTF8

    Write-RunMessage "Completed. CSV=$csvPath JSON=$jsonPath"

    if ($PassThru) {
        $summary
    }
} catch {
    Write-RunMessage "FAILED: $($_.Exception.Message)"
    throw
}
