[CmdletBinding()]
param(
    [string]$BackupDirectory,
    [string]$AdminDatabaseUrl = $env:POSTGRES_ADMIN_URL,
    [string]$OutputDirectory,
    [switch]$SkipMigrations,
    [switch]$KeepDatabaseOnFailure,
    [switch]$PassThru
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot

if ([string]::IsNullOrWhiteSpace($BackupDirectory)) {
    $BackupDirectory = Join-Path $repoRoot 'artifacts\postgres-backups'
}
$BackupDirectory = [System.IO.Path]::GetFullPath($BackupDirectory)
if (-not (Test-Path -LiteralPath $BackupDirectory -PathType Container)) {
    throw "Backup directory was not found: $BackupDirectory"
}

$candidates = @()
foreach ($manifestFile in @(Get-ChildItem -LiteralPath $BackupDirectory -Filter '*.manifest.json' -File)) {
    try {
        $manifest = Get-Content -LiteralPath $manifestFile.FullName -Raw | ConvertFrom-Json
        if ($manifest.status -ne 'completed') {
            continue
        }

        $archiveName = [string]$manifest.archive_filename
        if ([System.IO.Path]::GetFileName($archiveName) -ne $archiveName) {
            Write-Warning "Skipping manifest with unsafe archive filename: $($manifestFile.Name)"
            continue
        }

        $archivePath = Join-Path $BackupDirectory $archiveName
        if (-not (Test-Path -LiteralPath $archivePath -PathType Leaf)) {
            Write-Warning "Skipping manifest whose archive is missing: $($manifestFile.Name)"
            continue
        }

        $candidates += [pscustomobject]@{
            CreatedAtUtc = [DateTimeOffset]::Parse($manifest.created_at_utc).ToUniversalTime()
            ArchivePath  = $archivePath
            ManifestPath = $manifestFile.FullName
        }
    } catch {
        Write-Warning "Skipping unreadable backup manifest: $($manifestFile.FullName)"
    }
}

$latest = $candidates | Sort-Object CreatedAtUtc -Descending | Select-Object -First 1
if ($null -eq $latest) {
    throw "No completed PostgreSQL backup set was found in $BackupDirectory"
}

Write-Host "Verifying latest PostgreSQL backup: $($latest.ArchivePath)"
$restoreScript = Join-Path $PSScriptRoot 'postgres-restore-verify.ps1'
$arguments = @{
    BackupPath = $latest.ArchivePath
}
if (-not [string]::IsNullOrWhiteSpace($AdminDatabaseUrl)) {
    $arguments.AdminDatabaseUrl = $AdminDatabaseUrl
}
if (-not [string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $arguments.OutputDirectory = $OutputDirectory
}
if ($SkipMigrations) {
    $arguments.SkipMigrations = $true
}
if ($KeepDatabaseOnFailure) {
    $arguments.KeepDatabaseOnFailure = $true
}
if ($PassThru) {
    $arguments.PassThru = $true
}

& $restoreScript @arguments
