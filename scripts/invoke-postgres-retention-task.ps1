[CmdletBinding()]
param(
    [string]$PostgresBin = 'C:\Program Files\PostgreSQL\18\bin',

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

    [string]$LogDirectory,

    [ValidateRange(1, 3650)]
    [int]$LogRetentionDays = 60
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$exitCode = 0
$transcriptStarted = $false
$locationPushed = $false

if ([string]::IsNullOrWhiteSpace($LogDirectory)) {
    $LogDirectory = Join-Path $repoRoot 'artifacts\postgres-scheduled-tasks'
}

$PostgresBin = [System.IO.Path]::GetFullPath($PostgresBin)
$LogDirectory = [System.IO.Path]::GetFullPath($LogDirectory)
New-Item -ItemType Directory -Path $LogDirectory -Force | Out-Null

$timestamp = [DateTimeOffset]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')
$logPath = Join-Path $LogDirectory "postgres-retention-task-$timestamp.log"

try {
    Start-Transcript -LiteralPath $logPath -Append | Out-Null
    $transcriptStarted = $true

    $psqlPath = Join-Path $PostgresBin 'psql.exe'
    if (-not (Test-Path -LiteralPath $psqlPath -PathType Leaf)) {
        throw "Required PostgreSQL tool was not found: $psqlPath"
    }

    $env:Path = "$PostgresBin;$env:Path"
    $retentionScript = Join-Path $PSScriptRoot 'postgres-retention.ps1'
    if (-not (Test-Path -LiteralPath $retentionScript -PathType Leaf)) {
        throw "Retention script was not found: $retentionScript"
    }

    $localEnvPath = Join-Path $repoRoot '.env.local'
    if (-not (Test-Path -LiteralPath $localEnvPath -PathType Leaf)) {
        throw "Required local environment file was not found: $localEnvPath"
    }

    Push-Location $repoRoot
    $locationPushed = $true

    Write-Host "Scheduled PostgreSQL retention started at $([DateTimeOffset]::Now.ToString('o'))."
    Write-Host "Repository=$repoRoot"
    Write-Host "Policy=raw-history:$MarketHistoryRawRetentionDays raw-prices:$MarketRawRetentionDays history-requests:$MarketHistoryRequestsRetentionDays price-requests:$MarketRequestsRetentionDays history-buckets:$MarketHistoryBucketsRetentionDays"
    Write-Host "BatchSize=$BatchSize PauseMilliseconds=$PauseMilliseconds MaxBatchesPerTable=$MaxBatchesPerTable"

    & $retentionScript `
        -Mode Apply `
        -MarketHistoryRawRetentionDays $MarketHistoryRawRetentionDays `
        -MarketRawRetentionDays $MarketRawRetentionDays `
        -MarketHistoryRequestsRetentionDays $MarketHistoryRequestsRetentionDays `
        -MarketRequestsRetentionDays $MarketRequestsRetentionDays `
        -MarketHistoryBucketsRetentionDays $MarketHistoryBucketsRetentionDays `
        -BatchSize $BatchSize `
        -PauseMilliseconds $PauseMilliseconds `
        -LockTimeoutSeconds $LockTimeoutSeconds `
        -StatementTimeoutSeconds $StatementTimeoutSeconds `
        -MaxBatchesPerTable $MaxBatchesPerTable

    if ($LASTEXITCODE -ne 0) {
        throw "postgres-retention.ps1 returned exit code $LASTEXITCODE."
    }

    Write-Host "Scheduled PostgreSQL retention completed successfully at $([DateTimeOffset]::Now.ToString('o'))."
} catch {
    $exitCode = 1
    Write-Error "Scheduled PostgreSQL retention failed: $($_.Exception.Message)" -ErrorAction Continue
} finally {
    if ($locationPushed) {
        Pop-Location
    }

    if ($transcriptStarted) {
        try {
            Stop-Transcript | Out-Null
        } catch {
            Write-Warning "Could not stop transcript cleanly: $($_.Exception.Message)"
        }
    }

    $logCutoff = [DateTime]::UtcNow.AddDays(-$LogRetentionDays)
    Get-ChildItem -LiteralPath $LogDirectory -Filter 'postgres-*-task-*.log' -File -ErrorAction SilentlyContinue |
        Where-Object { $_.LastWriteTimeUtc -lt $logCutoff } |
        Remove-Item -Force -ErrorAction SilentlyContinue
}

exit $exitCode
