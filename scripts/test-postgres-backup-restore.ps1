[CmdletBinding()]
param(
    [string]$AdminDatabaseUrl = $env:POSTGRES_ADMIN_URL,
    [switch]$KeepDatabaseOnFailure
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot 'postgres-client.ps1')

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

function Invoke-PsqlCommand {
    param(
        [string]$ConnectionString,
        [string]$Sql
    )

    $result = Invoke-PostgresTool -ToolPath $script:PsqlPath -ConnectionString $ConnectionString -Arguments @(
        '-X'
        '-q'
        '--no-password'
        '--set', 'ON_ERROR_STOP=1'
        '--command', $Sql
    )
    if ($result.ExitCode -ne 0) {
        throw "psql command failed: $($result.Output -join [Environment]::NewLine)"
    }
}

function Invoke-PsqlFile {
    param(
        [string]$ConnectionString,
        [string]$Path,
        [hashtable]$Variables = @{}
    )

    $arguments = @(
        '-X'
        '-q'
        '--no-password'
        '--set', 'ON_ERROR_STOP=1'
        '--file', $Path
    )
    foreach ($key in ($Variables.Keys | Sort-Object)) {
        $arguments += @('--set', "$key=$($Variables[$key])")
    }

    $result = Invoke-PostgresTool -ToolPath $script:PsqlPath -ConnectionString $ConnectionString -Arguments $arguments
    if ($result.ExitCode -ne 0) {
        throw "psql failed while executing ${Path}: $($result.Output -join [Environment]::NewLine)"
    }
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

$AdminDatabaseUrl = Get-DatabaseUrlFromLocalEnvironment -CurrentValue $AdminDatabaseUrl
if ([string]::IsNullOrWhiteSpace($AdminDatabaseUrl)) {
    throw 'POSTGRES_ADMIN_URL or DATABASE_URL is required. It must allow CREATE DATABASE and DROP DATABASE.'
}

$psql = Get-Command psql -ErrorAction SilentlyContinue
if ($null -eq $psql) {
    throw 'psql was not found in PATH.'
}
$script:PsqlPath = $psql.Source

$databaseName = "albion_market_backup_test_$((Get-Date).ToString('yyyyMMdd_HHmmss'))_$PID"
$sourceDatabaseUrl = Set-DatabaseNameInUrl -Url $AdminDatabaseUrl -DatabaseName $databaseName
$backupScript = Join-Path $PSScriptRoot 'postgres-backup.ps1'
$restoreScript = Join-Path $PSScriptRoot 'postgres-restore-verify.ps1'
$verifyLatestScript = Join-Path $PSScriptRoot 'verify-latest-postgres-backup.ps1'
$seedSql = Join-Path $PSScriptRoot 'sql\postgres-retention-test-seed.sql'
$testRoot = Join-Path $repoRoot "artifacts\postgres-backup-restore-test\$databaseName"
$backupDirectory = Join-Path $testRoot 'backups'
$restoreReportDirectory = Join-Path $testRoot 'restore-reports'
$referenceTime = [DateTimeOffset]::Parse('2026-07-02T12:00:00Z')
$created = $false
$succeeded = $false

try {
    Write-Host "Creating disposable source database $databaseName..."
    Invoke-PsqlCommand -ConnectionString $AdminDatabaseUrl -Sql "create database `"$databaseName`" template template0;"
    $created = $true

    Write-Host 'Applying migrations to disposable source database...'
    Get-ChildItem -LiteralPath (Join-Path $repoRoot 'migrations') -Filter '*.sql' |
        Sort-Object Name |
        ForEach-Object {
            Write-Host "  $($_.Name)"
            Invoke-PsqlFile -ConnectionString $sourceDatabaseUrl -Path $_.FullName
        }

    Write-Host 'Loading deterministic backup fixtures...'
    Invoke-PsqlFile -ConnectionString $sourceDatabaseUrl -Path $seedSql -Variables @{
        reference_time = $referenceTime.ToString("yyyy-MM-ddTHH:mm:ss'Z'")
    }

    Write-Host 'Creating three deterministic backup sets to test file retention...'
    $oldBackup = & $backupScript `
        -DatabaseUrl $sourceDatabaseUrl `
        -BackupDirectory $backupDirectory `
        -ReferenceTimeUtc ($referenceTime.AddDays(-40)) `
        -RetentionDays 30 `
        -MinimumBackups 2 `
        -PassThru

    $middleBackup = & $backupScript `
        -DatabaseUrl $sourceDatabaseUrl `
        -BackupDirectory $backupDirectory `
        -ReferenceTimeUtc ($referenceTime.AddDays(-20)) `
        -RetentionDays 30 `
        -MinimumBackups 2 `
        -PassThru

    $newestBackup = & $backupScript `
        -DatabaseUrl $sourceDatabaseUrl `
        -BackupDirectory $backupDirectory `
        -ReferenceTimeUtc $referenceTime `
        -RetentionDays 30 `
        -MinimumBackups 2 `
        -PassThru

    Assert-Equal $false (Test-Path -LiteralPath $oldBackup.BackupPath) 'expired oldest archive is removed'
    Assert-Equal $true (Test-Path -LiteralPath $middleBackup.BackupPath) 'minimum backup policy keeps middle archive'
    Assert-Equal $true (Test-Path -LiteralPath $newestBackup.BackupPath) 'newest archive exists'
    Assert-Equal 1 $newestBackup.ExpiredBackupSetsRemoved 'one expired backup set removed'
    Assert-Equal $true $newestBackup.SourceStableDuringBackup 'disposable source stayed stable during backup'

    Write-Host 'Selecting, restoring, and validating the newest backup in another disposable database...'
    $restoreResult = & $verifyLatestScript `
        -BackupDirectory $backupDirectory `
        -AdminDatabaseUrl $AdminDatabaseUrl `
        -OutputDirectory $restoreReportDirectory `
        -PassThru

    Assert-Equal 'passed' $restoreResult.Status 'restore verification status'
    Assert-Equal $true $restoreResult.ExactCountsCompared 'exact counts were compared'
    Assert-Equal 1 $restoreResult.RestoredSnapshot.table_counts.current_market_prices 'restored current price rows'
    Assert-Equal 3 $restoreResult.RestoredSnapshot.table_counts.market_history_buckets 'restored history bucket rows'
    Assert-Equal 4 $restoreResult.RestoredSnapshot.table_counts.market_history_ingest_raw 'restored history raw rows'
    Assert-Equal 4 $restoreResult.RestoredSnapshot.table_counts.market_history_ingest_requests 'restored history request rows'
    Assert-Equal 4 $restoreResult.RestoredSnapshot.table_counts.market_ingest_raw 'restored market raw rows'
    Assert-Equal 4 $restoreResult.RestoredSnapshot.table_counts.market_ingest_requests 'restored market request rows'

    Write-Host 'Verifying that a tampered checksum is rejected before restore...'
    $originalChecksum = Get-Content -LiteralPath $newestBackup.ChecksumPath -Raw
    try {
        ('0' * 64) + " *$([System.IO.Path]::GetFileName($newestBackup.BackupPath))" |
            Set-Content -LiteralPath $newestBackup.ChecksumPath -Encoding ASCII

        $checksumRejected = $false
        try {
            & $restoreScript `
                -BackupPath $newestBackup.BackupPath `
                -AdminDatabaseUrl $AdminDatabaseUrl `
                -OutputDirectory $restoreReportDirectory
        } catch {
            $checksumRejected = $true
        }
        Assert-Equal $true $checksumRejected 'tampered checksum is rejected'
    } finally {
        $originalChecksum | Set-Content -LiteralPath $newestBackup.ChecksumPath -Encoding ASCII -NoNewline
    }

    $succeeded = $true
    Write-Host 'PostgreSQL backup and restore integration test passed.'
} finally {
    if ($created) {
        if (-not $succeeded -and $KeepDatabaseOnFailure) {
            Write-Warning "Test failed. Disposable source database was kept for inspection: $databaseName"
        } else {
            Write-Host "Dropping disposable source database $databaseName..."
            try {
                Invoke-PsqlCommand -ConnectionString $AdminDatabaseUrl -Sql "drop database if exists `"$databaseName`" with (force);"
            } catch {
                if ($succeeded) {
                    throw
                }
                Write-Warning "The test also failed to drop disposable source database ${databaseName}: $($_.Exception.Message)"
            }
        }
    }
}
