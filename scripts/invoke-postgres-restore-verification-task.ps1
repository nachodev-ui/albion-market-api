[CmdletBinding()]
param(
    [string]$BackupDirectory,

    [string]$PostgresBin = 'C:\Program Files\PostgreSQL\18\bin',

    [string]$LogDirectory,

    [ValidateRange(1, 3650)]
    [int]$LogRetentionDays = 60
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
$exitCode = 0
$transcriptStarted = $false
$locationPushed = $false

if ([string]::IsNullOrWhiteSpace($BackupDirectory)) {
    $BackupDirectory = Join-Path $env:USERPROFILE 'Documents\AlbionBackups\PostgreSQL'
}
if ([string]::IsNullOrWhiteSpace($LogDirectory)) {
    $LogDirectory = Join-Path $repoRoot 'artifacts\postgres-scheduled-tasks'
}

$BackupDirectory = [System.IO.Path]::GetFullPath($BackupDirectory)
$LogDirectory = [System.IO.Path]::GetFullPath($LogDirectory)
New-Item -ItemType Directory -Path $LogDirectory -Force | Out-Null

$timestamp = [DateTimeOffset]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')
$logPath = Join-Path $LogDirectory "postgres-restore-verification-task-$timestamp.log"

try {
    Start-Transcript -LiteralPath $logPath -Append | Out-Null
    $transcriptStarted = $true

    $requiredTools = @('psql.exe', 'pg_dump.exe', 'pg_restore.exe')
    foreach ($tool in $requiredTools) {
        $toolPath = Join-Path $PostgresBin $tool
        if (-not (Test-Path -LiteralPath $toolPath -PathType Leaf)) {
            throw "Required PostgreSQL tool was not found: $toolPath"
        }
    }

    if (-not (Test-Path -LiteralPath $BackupDirectory -PathType Container)) {
        throw "Backup directory was not found: $BackupDirectory"
    }

    $env:Path = "$PostgresBin;$env:Path"
    $verificationScript = Join-Path $PSScriptRoot 'verify-latest-postgres-backup.ps1'
    if (-not (Test-Path -LiteralPath $verificationScript -PathType Leaf)) {
        throw "Restore verification script was not found: $verificationScript"
    }

    Push-Location $repoRoot
    $locationPushed = $true

    Write-Host "Scheduled PostgreSQL restore verification started at $([DateTimeOffset]::Now.ToString('o'))."
    Write-Host "Repository=$repoRoot"
    Write-Host "BackupDirectory=$BackupDirectory"

    & $verificationScript -BackupDirectory $BackupDirectory

    Write-Host "Scheduled PostgreSQL restore verification completed successfully at $([DateTimeOffset]::Now.ToString('o'))."
} catch {
    $exitCode = 1
    Write-Error "Scheduled PostgreSQL restore verification failed: $($_.Exception.Message)" -ErrorAction Continue
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
