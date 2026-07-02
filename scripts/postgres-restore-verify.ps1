[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string]$BackupPath,

    [string]$AdminDatabaseUrl = $env:POSTGRES_ADMIN_URL,

    [string]$ChecksumPath,

    [string]$ManifestPath,

    [string]$OutputDirectory,

    [switch]$SkipMigrations,

    [switch]$KeepDatabaseOnSuccess,

    [switch]$KeepDatabaseOnFailure,

    [switch]$PassThru
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
        if ($builder.Scheme -notin @('postgres', 'postgresql')) {
            throw 'Unsupported scheme.'
        }
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
        $message = ($result.Output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
        throw "psql command failed.$([Environment]::NewLine)$message"
    }
}

function Invoke-PsqlFile {
    param(
        [string]$ConnectionString,
        [string]$Path
    )

    $result = Invoke-PostgresTool -ToolPath $script:PsqlPath -ConnectionString $ConnectionString -Arguments @(
        '-X'
        '-q'
        '--no-password'
        '--set', 'ON_ERROR_STOP=1'
        '--file', $Path
    )
    if ($result.ExitCode -ne 0) {
        $message = ($result.Output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
        throw "psql failed while executing $Path.$([Environment]::NewLine)$message"
    }
}

function Invoke-PsqlJson {
    param(
        [string]$ConnectionString,
        [string]$SqlPath
    )

    $result = Invoke-PostgresTool -ToolPath $script:PsqlPath -ConnectionString $ConnectionString -Arguments @(
        '-X'
        '-q'
        '-A'
        '-t'
        '--no-password'
        '--set', 'ON_ERROR_STOP=1'
        '--file', $SqlPath
    )
    if ($result.ExitCode -ne 0) {
        $message = ($result.Output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
        throw "psql validation query failed.$([Environment]::NewLine)$message"
    }

    $lines = @(
        $result.Output |
            ForEach-Object { $_.ToString().Trim() } |
            Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    )
    if ($lines.Count -eq 0) {
        throw 'psql validation query returned no JSON.'
    }
    return ($lines[$lines.Count - 1] | ConvertFrom-Json)
}

function Assert-Equal {
    param(
        [object]$Expected,
        [object]$Actual,
        [string]$Message
    )

    if ([string]$Expected -ne [string]$Actual) {
        throw "Validation failed: $Message. Expected=$Expected Actual=$Actual"
    }
}

function Compare-PropertyBag {
    param(
        [pscustomobject]$Expected,
        [pscustomobject]$Actual,
        [string]$Label
    )

    foreach ($property in $Expected.PSObject.Properties) {
        $actualProperty = $Actual.PSObject.Properties[$property.Name]
        if ($null -eq $actualProperty) {
            throw "Validation failed: $Label is missing property $($property.Name)."
        }
        Assert-Equal $property.Value $actualProperty.Value "$Label.$($property.Name)"
    }
}

$BackupPath = [System.IO.Path]::GetFullPath($BackupPath)
if (-not (Test-Path -LiteralPath $BackupPath -PathType Leaf)) {
    throw "Backup archive was not found: $BackupPath"
}
if ([string]::IsNullOrWhiteSpace($ChecksumPath)) {
    $ChecksumPath = "$BackupPath.sha256"
}
if ([string]::IsNullOrWhiteSpace($ManifestPath)) {
    $baseWithoutDump = $BackupPath.Substring(0, $BackupPath.Length - [System.IO.Path]::GetExtension($BackupPath).Length)
    $ManifestPath = "$baseWithoutDump.manifest.json"
}
$ChecksumPath = [System.IO.Path]::GetFullPath($ChecksumPath)
$ManifestPath = [System.IO.Path]::GetFullPath($ManifestPath)
if (-not (Test-Path -LiteralPath $ChecksumPath -PathType Leaf)) {
    throw "Checksum file was not found: $ChecksumPath"
}
if (-not (Test-Path -LiteralPath $ManifestPath -PathType Leaf)) {
    throw "Backup manifest was not found: $ManifestPath"
}

$manifest = Get-Content -LiteralPath $ManifestPath -Raw | ConvertFrom-Json
if ($manifest.status -ne 'completed') {
    throw 'Backup manifest does not describe a completed backup.'
}
if ([System.IO.Path]::GetFileName($BackupPath) -ne [string]$manifest.archive_filename) {
    throw 'Backup filename does not match the manifest.'
}

$checksumLine = (Get-Content -LiteralPath $ChecksumPath | Select-Object -First 1).Trim()
$checksumMatch = [regex]::Match($checksumLine, '^([A-Fa-f0-9]{64})\s+\*?(.+)$')
if (-not $checksumMatch.Success) {
    throw 'Checksum file has an invalid SHA256 format.'
}
$expectedHash = $checksumMatch.Groups[1].Value.ToLowerInvariant()
$checksumFileName = [System.IO.Path]::GetFileName($checksumMatch.Groups[2].Value.Trim())
if ($checksumFileName -ne [System.IO.Path]::GetFileName($BackupPath)) {
    throw 'Checksum filename does not match the selected backup.'
}
if ($expectedHash -ne ([string]$manifest.archive_sha256).ToLowerInvariant()) {
    throw 'Checksum file and manifest contain different SHA256 values.'
}
$actualHash = (Get-FileHash -LiteralPath $BackupPath -Algorithm SHA256).Hash.ToLowerInvariant()
if ($actualHash -ne $expectedHash) {
    throw "Backup checksum verification failed. Expected=$expectedHash Actual=$actualHash"
}

$AdminDatabaseUrl = Get-DatabaseUrlFromLocalEnvironment -CurrentValue $AdminDatabaseUrl
if ([string]::IsNullOrWhiteSpace($AdminDatabaseUrl)) {
    throw 'POSTGRES_ADMIN_URL or DATABASE_URL is required. It must allow CREATE DATABASE and DROP DATABASE.'
}

$psql = Get-Command psql -ErrorAction SilentlyContinue
$pgRestore = Get-Command pg_restore -ErrorAction SilentlyContinue
if ($null -eq $psql -or $null -eq $pgRestore) {
    throw 'psql and pg_restore must both be available in PATH.'
}
$script:PsqlPath = $psql.Source
$script:PgRestorePath = $pgRestore.Source

$listResult = Invoke-PostgresTool -ToolPath $script:PgRestorePath -NoConnection -Arguments @(
    '--list'
    $BackupPath
)
if ($listResult.ExitCode -ne 0) {
    $message = ($listResult.Output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
    throw "pg_restore could not read the backup archive.$([Environment]::NewLine)$message"
}

if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $repoRoot 'artifacts\postgres-restore-verification'
}
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null

$databaseName = "albion_market_restore_verify_$((Get-Date).ToString('yyyyMMdd_HHmmss'))_$PID"
$restoreDatabaseUrl = Set-DatabaseNameInUrl -Url $AdminDatabaseUrl -DatabaseName $databaseName
$snapshotSqlPath = Join-Path $PSScriptRoot 'sql\postgres-backup-snapshot.sql'
$reportPath = Join-Path $OutputDirectory "postgres-restore-verification-$((Get-Date).ToString('yyyyMMdd-HHmmss-fff'))-$PID.json"
$created = $false
$succeeded = $false
$validationSnapshot = $null

try {
    Write-Host "Creating disposable restore database $databaseName..."
    Invoke-PsqlCommand -ConnectionString $AdminDatabaseUrl -Sql "create database `"$databaseName`" template template0;"
    $created = $true

    Write-Host 'Restoring custom-format archive in a single transaction...'
    $restoreResult = Invoke-PostgresTool -ToolPath $script:PgRestorePath -ConnectionString $restoreDatabaseUrl -Arguments @(
        '--exit-on-error'
        '--single-transaction'
        '--no-owner'
        '--no-privileges'
        '--no-password'
        $BackupPath
    )
    if ($restoreResult.ExitCode -ne 0) {
        $message = ($restoreResult.Output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
        throw "pg_restore failed with exit code $($restoreResult.ExitCode).$([Environment]::NewLine)$message"
    }

    if (-not $SkipMigrations) {
        Write-Host 'Applying repository migrations after restore...'
        Get-ChildItem -LiteralPath (Join-Path $repoRoot 'migrations') -Filter '*.sql' |
            Sort-Object Name |
            ForEach-Object {
                Write-Host "  $($_.Name)"
                Invoke-PsqlFile -ConnectionString $restoreDatabaseUrl -Path $_.FullName
            }
    }

    Write-Host 'Refreshing optimizer statistics in the restored database...'
    Invoke-PsqlCommand -ConnectionString $restoreDatabaseUrl -Sql 'analyze;'

    Write-Host 'Validating restored schema, counts, sequences, and representative queries...'
    $validationSnapshot = Invoke-PsqlJson -ConnectionString $restoreDatabaseUrl -SqlPath $snapshotSqlPath

    Assert-Equal 6 $validationSnapshot.schema.required_table_count 'required table count'
    Assert-Equal 'True' $validationSnapshot.query_checks.market_raw_sequence_valid 'market raw sequence'
    Assert-Equal 'True' $validationSnapshot.query_checks.history_raw_sequence_valid 'history raw sequence'

    Compare-PropertyBag -Expected $manifest.source_snapshot_after.schema -Actual $validationSnapshot.schema -Label 'schema'

    $countsCompared = $false
    if ([bool]$manifest.source_stable_during_backup) {
        Compare-PropertyBag -Expected $manifest.source_snapshot_after.table_counts -Actual $validationSnapshot.table_counts -Label 'table_counts'
        Compare-PropertyBag -Expected $manifest.source_snapshot_after.query_checks -Actual $validationSnapshot.query_checks -Label 'query_checks'
        $countsCompared = $true
    } else {
        Write-Warning 'Source changed while pg_dump was running. Exact count comparison is skipped; schema, sequences, and representative queries were still validated.'
    }

    $report = [ordered]@{
        status                       = 'passed'
        verified_at_utc              = [DateTimeOffset]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ss.fff'Z'")
        backup_id                    = $manifest.backup_id
        backup_path                  = $BackupPath
        checksum_verified            = $true
        archive_readable             = $true
        disposable_database          = $databaseName
        migrations_applied           = (-not $SkipMigrations)
        analyze_completed             = $true
        exact_counts_compared         = $countsCompared
        source_stable_during_backup  = [bool]$manifest.source_stable_during_backup
        restored_snapshot            = $validationSnapshot
    }
    $report | ConvertTo-Json -Depth 12 | Set-Content -LiteralPath $reportPath -Encoding UTF8

    $succeeded = $true
    Write-Host "Restore verification passed. Report=$reportPath"

    if ($PassThru) {
        [pscustomobject]@{
            Status                      = 'passed'
            BackupId                    = $manifest.backup_id
            BackupPath                  = $BackupPath
            ReportPath                  = $reportPath
            DisposableDatabase          = $databaseName
            ExactCountsCompared         = $countsCompared
            SourceStableDuringBackup    = [bool]$manifest.source_stable_during_backup
            RestoredSnapshot            = $validationSnapshot
        }
    }
} catch {
    $failureReport = [ordered]@{
        status                       = 'failed'
        verified_at_utc              = [DateTimeOffset]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ss.fff'Z'")
        backup_id                    = $manifest.backup_id
        backup_path                  = $BackupPath
        checksum_verified            = $true
        archive_readable             = $true
        disposable_database          = $databaseName
        migrations_applied           = (-not $SkipMigrations)
        source_stable_during_backup  = [bool]$manifest.source_stable_during_backup
        error                         = $_.Exception.Message
    }
    $failureReport | ConvertTo-Json -Depth 8 | Set-Content -LiteralPath $reportPath -Encoding UTF8
    Write-Warning "Restore verification failed. Report=$reportPath"
    throw
} finally {
    if ($created) {
        $keepDatabase = ($succeeded -and $KeepDatabaseOnSuccess) -or (-not $succeeded -and $KeepDatabaseOnFailure)
        if ($keepDatabase) {
            Write-Warning "Disposable restore database was kept for inspection: $databaseName"
        } else {
            Write-Host "Dropping disposable restore database $databaseName..."
            try {
                Invoke-PsqlCommand -ConnectionString $AdminDatabaseUrl -Sql "drop database if exists `"$databaseName`" with (force);"
            } catch {
                if ($succeeded) {
                    throw
                }
                Write-Warning "Restore verification also failed to drop disposable database ${databaseName}: $($_.Exception.Message)"
            }
        }
    }
}
