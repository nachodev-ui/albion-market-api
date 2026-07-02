[CmdletBinding()]
param(
    [string]$BackupDirectory,

    [string]$PostgresBin = 'C:\Program Files\PostgreSQL\18\bin',

    [ValidateRange(1, 3650)]
    [int]$RetentionDays = 30,

    [ValidateRange(1, 1000)]
    [int]$MinimumBackups = 7,

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
New-Item -ItemType Directory -Path $BackupDirectory -Force | Out-Null
New-Item -ItemType Directory -Path $LogDirectory -Force | Out-Null

$timestamp = [DateTimeOffset]::UtcNow.ToString('yyyyMMddTHHmmssfffZ')
$logPath = Join-Path $LogDirectory "postgres-backup-task-$timestamp.log"

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

    $env:Path = "$PostgresBin;$env:Path"
    $backupScript = Join-Path $PSScriptRoot 'postgres-backup.ps1'
    if (-not (Test-Path -LiteralPath $backupScript -PathType Leaf)) {
        throw "Backup script was not found: $backupScript"
    }

    Push-Location $repoRoot
    $locationPushed = $true

    Write-Host "Scheduled PostgreSQL backup started at $([DateTimeOffset]::Now.ToString('o'))."
    Write-Host "Repository=$repoRoot"
    Write-Host "BackupDirectory=$BackupDirectory"

    & $backupScript `
        -BackupDirectory $BackupDirectory `
        -RetentionDays $RetentionDays `
        -MinimumBackups $MinimumBackups

    Write-Host "Scheduled PostgreSQL backup completed successfully at $([DateTimeOffset]::Now.ToString('o'))."
} catch {
    $exitCode = 1
    Write-Error "Scheduled PostgreSQL backup failed: $($_.Exception.Message)" -ErrorAction Continue
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
