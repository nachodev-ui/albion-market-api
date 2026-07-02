[CmdletBinding()]
param(
    [string]$DatabaseUrl = $env:DATABASE_URL,

    [ValidateRange(1, 32)]
    [int]$SampleLocations = 8,

    [ValidateRange(1, 2000)]
    [int]$SampleEntries = 100,

    [ValidateRange(1, 100000)]
    [int]$RetentionBatchSize = 5000,

    [string]$OutputDirectory
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

function Get-PlanSection {
    param(
        [Parameter(Mandatory = $true)][string]$Report,
        [Parameter(Mandatory = $true)][string]$Name
    )

    $escapedName = [regex]::Escape($Name)
    $match = [regex]::Match(
        $Report,
        "(?ms)^=== PLAN:${escapedName} ===\r?\n(?<body>.*?)(?=^=== PLAN:|\z)"
    )

    if (-not $match.Success) {
        return ''
    }

    return $match.Groups['body'].Value
}

function Get-IndexStat {
    param(
        [Parameter(Mandatory = $true)][string]$Report,
        [Parameter(Mandatory = $true)][string]$IndexName
    )

    $escapedName = [regex]::Escape($IndexName)
    $match = [regex]::Match(
        $Report,
        "INDEX_STAT\|${escapedName}\|(?<scan>\d+)\|(?<read>\d+)\|(?<fetch>\d+)\|(?<bytes>\d+)"
    )

    if (-not $match.Success) {
        return $null
    }

    return [pscustomobject]@{
        IndexName   = $IndexName
        ScanCount   = [long]$match.Groups['scan'].Value
        TuplesRead  = [long]$match.Groups['read'].Value
        TuplesFetch = [long]$match.Groups['fetch'].Value
        SizeBytes   = [long]$match.Groups['bytes'].Value
    }
}

$DatabaseUrl = Get-DatabaseUrlFromLocalEnvironment -CurrentValue $DatabaseUrl -Root $repoRoot
if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    throw 'DATABASE_URL is not defined. Use -DatabaseUrl, set the environment variable, or add it to .env.local.'
}

$psql = Get-Command psql -ErrorAction SilentlyContinue
if ($null -eq $psql) {
    throw 'psql was not found in PATH. Add the PostgreSQL client tools before running the review.'
}

$clientScript = Join-Path $PSScriptRoot 'postgres-client.ps1'
$sqlPath = Join-Path $PSScriptRoot 'sql\postgres-index-review.sql'
foreach ($requiredPath in @($clientScript, $sqlPath)) {
    if (-not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) {
        throw "Required file was not found: $requiredPath"
    }
}
. $clientScript

if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $repoRoot 'artifacts\postgres-index-review'
}
$OutputDirectory = [System.IO.Path]::GetFullPath($OutputDirectory)
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null

$runId = "$((Get-Date).ToString('yyyyMMdd-HHmmss-fff'))-$PID"
$textPath = Join-Path $OutputDirectory "postgres-index-review-$runId.txt"
$jsonPath = Join-Path $OutputDirectory "postgres-index-review-$runId.json"

Write-Host 'Running read-only EXPLAIN (ANALYZE, BUFFERS) review against real query shapes...'
Write-Warning 'EXPLAIN ANALYZE executes the SELECT statements. Run during low activity when the database becomes large.'

$result = Invoke-PostgresTool `
    -ToolPath $psql.Source `
    -ConnectionString $DatabaseUrl `
    -ApplicationName 'albion-market-api-index-review' `
    -Arguments @(
        '-X',
        '--set', 'ON_ERROR_STOP=1',
        '--set', "sample_locations=$SampleLocations",
        '--set', "sample_entries=$SampleEntries",
        '--set', "retention_batch_size=$RetentionBatchSize",
        '--file', $sqlPath
    )

$report = ($result.Output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
$utf8NoBom = [System.Text.UTF8Encoding]::new($false)
[System.IO.File]::WriteAllText($textPath, $report + [Environment]::NewLine, $utf8NoBom)

if ($result.ExitCode -ne 0) {
    throw "PostgreSQL index review failed with exit code $($result.ExitCode). Review: $textPath"
}

$defaultPricePlan = Get-PlanSection -Report $report -Name 'current_market_prices:default'
$indexPreferredPricePlan = Get-PlanSection -Report $report -Name 'current_market_prices:index_preferred'
$secondaryIndexName = 'current_market_prices_item_loc_idx'
$primaryIndexName = 'current_market_prices_pkey'
$secondaryStats = Get-IndexStat -Report $report -IndexName $secondaryIndexName
$primaryStats = Get-IndexStat -Report $report -IndexName $primaryIndexName

$secondaryUsedDefault = $defaultPricePlan -match [regex]::Escape($secondaryIndexName)
$secondaryUsedIndexPreferred = $indexPreferredPricePlan -match [regex]::Escape($secondaryIndexName)
$primaryUsedDefault = $defaultPricePlan -match [regex]::Escape($primaryIndexName)
$primaryUsedIndexPreferred = $indexPreferredPricePlan -match [regex]::Escape($primaryIndexName)

$recommendation = 'manual-review-required'
$reason = 'The representative and index-preferred plans did not provide enough evidence to decide safely.'

if ($secondaryUsedDefault -or $secondaryUsedIndexPreferred) {
    $recommendation = 'keep'
    $reason = 'The real current-price query used current_market_prices_item_loc_idx in at least one measured plan.'
} elseif ($primaryUsedIndexPreferred -and $null -ne $secondaryStats -and $secondaryStats.ScanCount -eq 0) {
    $recommendation = 'candidate-for-removal'
    $reason = 'The index-preferred diagnostic selected the primary key and the secondary index had no prior scans at report start.'
} elseif ($primaryUsedIndexPreferred) {
    $recommendation = 'observe-before-removal'
    $reason = 'The primary key covers the measured query, but the secondary index has historical scans that must be explained before removal.'
} elseif ($primaryUsedDefault) {
    $recommendation = 'observe-before-removal'
    $reason = 'The default plan used the primary key, but the forced-index diagnostic was inconclusive.'
}

$summary = [ordered]@{
    run_id                                  = $runId
    generated_at_utc                        = [DateTimeOffset]::UtcNow.ToString('o')
    sample_locations                        = $SampleLocations
    sample_entries                          = $SampleEntries
    retention_batch_size                    = $RetentionBatchSize
    current_market_prices_index_decision    = $recommendation
    decision_reason                         = $reason
    secondary_index                         = if ($null -eq $secondaryStats) { $null } else { $secondaryStats }
    primary_index                           = if ($null -eq $primaryStats) { $null } else { $primaryStats }
    default_plan_uses_secondary             = $secondaryUsedDefault
    index_preferred_plan_uses_secondary     = $secondaryUsedIndexPreferred
    default_plan_uses_primary               = $primaryUsedDefault
    index_preferred_plan_uses_primary       = $primaryUsedIndexPreferred
    schema_changed                          = $false
    text_report                             = $textPath
}
[System.IO.File]::WriteAllText(
    $jsonPath,
    ($summary | ConvertTo-Json -Depth 6) + [Environment]::NewLine,
    $utf8NoBom
)

Write-Host ''
Write-Host 'Index review completed.'
Write-Host "TextReport=$textPath"
Write-Host "JsonSummary=$jsonPath"
Write-Host "CurrentPriceSecondaryIndexDecision=$recommendation"
Write-Host "Reason=$reason"
Write-Host 'No index was created or dropped by this review.'
