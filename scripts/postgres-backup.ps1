[CmdletBinding()]
param(
    [string]$DatabaseUrl = $env:DATABASE_URL,

    [string]$BackupDirectory,

    [ValidateRange(1, 3650)]
    [int]$RetentionDays = 30,

    [ValidateRange(1, 1000)]
    [int]$MinimumBackups = 7,

    [DateTimeOffset]$ReferenceTimeUtc = [DateTimeOffset]::UtcNow,

    [switch]$SkipRetentionCleanup,

    [switch]$PassThru
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot
. (Join-Path $PSScriptRoot 'postgres-client.ps1')

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
        throw "psql snapshot query failed.$([Environment]::NewLine)$message"
    }

    $lines = @(
        $result.Output |
            ForEach-Object { $_.ToString().Trim() } |
            Where-Object { -not [string]::IsNullOrWhiteSpace($_) }
    )

    if ($lines.Count -eq 0) {
        throw 'psql snapshot query returned no JSON.'
    }

    try {
        return ($lines[$lines.Count - 1] | ConvertFrom-Json)
    } catch {
        throw "psql snapshot query returned invalid JSON: $($lines[$lines.Count - 1])"
    }
}

function Get-ComparableSnapshotJson {
    param([pscustomobject]$Snapshot)

    return ([ordered]@{
        schema       = $Snapshot.schema
        table_counts = $Snapshot.table_counts
        query_checks = $Snapshot.query_checks
    } | ConvertTo-Json -Depth 10 -Compress)
}

function Get-ClientMajorVersion {
    param(
        [string]$ToolPath,
        [string]$ToolName
    )

    $versionOutput = & $ToolPath --version 2>&1
    if ($LASTEXITCODE -ne 0) {
        throw "$ToolName --version failed."
    }

    $versionText = ($versionOutput | ForEach-Object { $_.ToString() }) -join ' '
    $match = [regex]::Match($versionText, '(\d+)(?:\.\d+)?')
    if (-not $match.Success) {
        throw "Could not parse $ToolName version from: $versionText"
    }

    return [pscustomobject]@{
        Text  = $versionText.Trim()
        Major = [int]$match.Groups[1].Value
    }
}

function Convert-ToSafeFileName {
    param([string]$Value)

    $safe = $Value -replace '[^A-Za-z0-9._-]', '_'
    if ([string]::IsNullOrWhiteSpace($safe)) {
        return 'postgresql'
    }
    return $safe
}

function Remove-ExpiredBackupSets {
    param(
        [string]$Directory,
        [DateTimeOffset]$ReferenceUtc,
        [int]$Days,
        [int]$KeepMinimum
    )

    $manifestFiles = @(Get-ChildItem -LiteralPath $Directory -Filter '*.manifest.json' -File -ErrorAction SilentlyContinue)
    $sets = @()

    foreach ($manifestFile in $manifestFiles) {
        try {
            $manifest = Get-Content -LiteralPath $manifestFile.FullName -Raw | ConvertFrom-Json
            if ($manifest.status -ne 'completed') {
                continue
            }

            $createdAt = [DateTimeOffset]::Parse($manifest.created_at_utc).ToUniversalTime()
            $baseName = $manifestFile.Name.Substring(0, $manifestFile.Name.Length - '.manifest.json'.Length)
            $requiredPaths = @(
                (Join-Path $Directory ($baseName + '.dump')),
                (Join-Path $Directory ($baseName + '.dump.sha256')),
                (Join-Path $Directory ($baseName + '.toc.txt')),
                $manifestFile.FullName
            )
            $missingRequiredFile = $false
            foreach ($requiredPath in $requiredPaths) {
                if (-not (Test-Path -LiteralPath $requiredPath -PathType Leaf)) {
                    $missingRequiredFile = $true
                    break
                }
            }
            if ($missingRequiredFile) {
                Write-Warning "Skipping incomplete backup set during retention: $baseName"
                continue
            }

            $sets += [pscustomobject]@{
                ManifestPath = $manifestFile.FullName
                BaseName     = $baseName
                CreatedAtUtc = $createdAt
            }
        } catch {
            Write-Warning "Skipping unreadable backup manifest during retention: $($manifestFile.FullName)"
        }
    }

    $ordered = @($sets | Sort-Object CreatedAtUtc -Descending)
    $protectedNames = @(
        $ordered |
            Select-Object -First $KeepMinimum |
            ForEach-Object { $_.BaseName }
    )
    $cutoff = $ReferenceUtc.AddDays(-$Days)
    $deletedSets = 0

    foreach ($set in $ordered) {
        if ($protectedNames -contains $set.BaseName) {
            continue
        }
        if ($set.CreatedAtUtc -ge $cutoff) {
            continue
        }

        foreach ($suffix in @('.dump', '.dump.sha256', '.toc.txt', '.manifest.json')) {
            $path = Join-Path $Directory ($set.BaseName + $suffix)
            if (Test-Path -LiteralPath $path -PathType Leaf) {
                Remove-Item -LiteralPath $path -Force
            }
        }
        $deletedSets++
        Write-Host "Removed expired backup set: $($set.BaseName)"
    }

    $partialCutoff = $ReferenceUtc.AddHours(-24).UtcDateTime
    Get-ChildItem -LiteralPath $Directory -Filter '*.partial' -File -ErrorAction SilentlyContinue |
        Where-Object { $_.LastWriteTimeUtc -lt $partialCutoff } |
        ForEach-Object {
            Remove-Item -LiteralPath $_.FullName -Force
            Write-Host "Removed stale partial backup file: $($_.Name)"
        }

    return $deletedSets
}

$DatabaseUrl = Get-DatabaseUrlFromLocalEnvironment -CurrentValue $DatabaseUrl -Root $repoRoot
if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    throw 'DATABASE_URL is not defined. Use -DatabaseUrl, set the environment variable, or add it to .env.local.'
}

$psql = Get-Command psql -ErrorAction SilentlyContinue
$pgDump = Get-Command pg_dump -ErrorAction SilentlyContinue
$pgRestore = Get-Command pg_restore -ErrorAction SilentlyContinue
if ($null -eq $psql -or $null -eq $pgDump -or $null -eq $pgRestore) {
    throw 'psql, pg_dump, and pg_restore must all be available in PATH.'
}
$script:PsqlPath = $psql.Source
$script:PgDumpPath = $pgDump.Source
$script:PgRestorePath = $pgRestore.Source

$snapshotSqlPath = Join-Path $PSScriptRoot 'sql\postgres-backup-snapshot.sql'
if (-not (Test-Path -LiteralPath $snapshotSqlPath -PathType Leaf)) {
    throw "Backup snapshot SQL was not found: $snapshotSqlPath"
}

if ([string]::IsNullOrWhiteSpace($BackupDirectory)) {
    $BackupDirectory = Join-Path $repoRoot 'artifacts\postgres-backups'
}
$BackupDirectory = [System.IO.Path]::GetFullPath($BackupDirectory)
$directoryRoot = [System.IO.Path]::GetPathRoot($BackupDirectory)
$normalizedBackupDirectory = $BackupDirectory.TrimEnd([char[]]@('\', '/'))
$normalizedDirectoryRoot = $directoryRoot.TrimEnd([char[]]@('\', '/'))
if ($normalizedBackupDirectory -eq $normalizedDirectoryRoot) {
    throw 'BackupDirectory cannot be a filesystem root.'
}
New-Item -ItemType Directory -Path $BackupDirectory -Force | Out-Null

$referenceUtc = $ReferenceTimeUtc.ToUniversalTime()
$preSnapshot = Invoke-PsqlJson -ConnectionString $DatabaseUrl -SqlPath $snapshotSqlPath
if ([int]$preSnapshot.schema.required_table_count -ne 6) {
    throw "Backup preflight expected 6 required tables but found $($preSnapshot.schema.required_table_count)."
}

$pgDumpVersion = Get-ClientMajorVersion -ToolPath $script:PgDumpPath -ToolName 'pg_dump'
$pgRestoreVersion = Get-ClientMajorVersion -ToolPath $script:PgRestorePath -ToolName 'pg_restore'
$serverMajor = [math]::Floor([int]$preSnapshot.server_version_num / 10000)
if ($pgDumpVersion.Major -lt $serverMajor) {
    throw "pg_dump major version $($pgDumpVersion.Major) is older than server major version $serverMajor."
}

$safeDatabaseName = Convert-ToSafeFileName -Value $preSnapshot.database_name
$stamp = $referenceUtc.ToString("yyyyMMdd'T'HHmmssfff'Z'")
$baseName = "$safeDatabaseName-$stamp-$PID"
$dumpPath = Join-Path $BackupDirectory "$baseName.dump"
$checksumPath = Join-Path $BackupDirectory "$baseName.dump.sha256"
$tocPath = Join-Path $BackupDirectory "$baseName.toc.txt"
$manifestPath = Join-Path $BackupDirectory "$baseName.manifest.json"
$partialDumpPath = "$dumpPath.partial"
$partialChecksumPath = "$checksumPath.partial"
$partialTocPath = "$tocPath.partial"
$partialManifestPath = "$manifestPath.partial"

foreach ($path in @($partialDumpPath, $partialChecksumPath, $partialTocPath, $partialManifestPath)) {
    Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue
}
$backupPublished = $false

try {
    Write-Host "Creating custom-format PostgreSQL backup: $dumpPath"
    $dumpResult = Invoke-PostgresTool -ToolPath $script:PgDumpPath -ConnectionString $DatabaseUrl -Arguments @(
        '--format=custom'
        '--no-owner'
        '--no-privileges'
        '--no-password'
        '--file', $partialDumpPath
    )
    if ($dumpResult.ExitCode -ne 0) {
        $message = ($dumpResult.Output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
        throw "pg_dump failed with exit code $($dumpResult.ExitCode).$([Environment]::NewLine)$message"
    }
    foreach ($line in @($dumpResult.Output)) {
        if (-not [string]::IsNullOrWhiteSpace($line.ToString())) {
            Write-Warning "pg_dump: $($line.ToString())"
        }
    }

    if (-not (Test-Path -LiteralPath $partialDumpPath -PathType Leaf)) {
        throw 'pg_dump completed without creating the archive.'
    }
    if ((Get-Item -LiteralPath $partialDumpPath).Length -le 0) {
        throw 'pg_dump created an empty archive.'
    }

    $tocResult = Invoke-PostgresTool -ToolPath $script:PgRestorePath -NoConnection -Arguments @(
        '--list'
        $partialDumpPath
    )
    if ($tocResult.ExitCode -ne 0) {
        $message = ($tocResult.Output | ForEach-Object { $_.ToString() }) -join [Environment]::NewLine
        throw "pg_restore could not read the new archive.$([Environment]::NewLine)$message"
    }
    $tocResult.Output | Set-Content -LiteralPath $partialTocPath -Encoding UTF8

    $postSnapshot = Invoke-PsqlJson -ConnectionString $DatabaseUrl -SqlPath $snapshotSqlPath
    $sourceStable = (Get-ComparableSnapshotJson $preSnapshot) -eq (Get-ComparableSnapshotJson $postSnapshot)

    $hash = (Get-FileHash -LiteralPath $partialDumpPath -Algorithm SHA256).Hash.ToLowerInvariant()
    "$hash *$([System.IO.Path]::GetFileName($dumpPath))" |
        Set-Content -LiteralPath $partialChecksumPath -Encoding ASCII

    $manifest = [ordered]@{
        format_version              = 1
        status                      = 'completed'
        backup_id                   = $baseName
        created_at_utc              = $referenceUtc.ToString("yyyy-MM-ddTHH:mm:ss.fff'Z'")
        database_name               = $preSnapshot.database_name
        database_user               = $preSnapshot.database_user
        server_version              = $preSnapshot.server_version
        server_version_num          = [int]$preSnapshot.server_version_num
        pg_dump_version             = $pgDumpVersion.Text
        pg_restore_version          = $pgRestoreVersion.Text
        archive_filename            = [System.IO.Path]::GetFileName($dumpPath)
        archive_format              = 'custom'
        archive_size_bytes          = (Get-Item -LiteralPath $partialDumpPath).Length
        archive_sha256              = $hash
        source_stable_during_backup = $sourceStable
        source_snapshot_before      = $preSnapshot
        source_snapshot_after       = $postSnapshot
        retention_days              = $RetentionDays
        minimum_backups             = $MinimumBackups
        credentials_stored          = $false
    }
    $manifest | ConvertTo-Json -Depth 12 |
        Set-Content -LiteralPath $partialManifestPath -Encoding UTF8

    Move-Item -LiteralPath $partialDumpPath -Destination $dumpPath
    Move-Item -LiteralPath $partialChecksumPath -Destination $checksumPath
    Move-Item -LiteralPath $partialTocPath -Destination $tocPath
    Move-Item -LiteralPath $partialManifestPath -Destination $manifestPath
    $backupPublished = $true

    $deletedSets = 0
    if (-not $SkipRetentionCleanup) {
        $deletedSets = Remove-ExpiredBackupSets `
            -Directory $BackupDirectory `
            -ReferenceUtc $referenceUtc `
            -Days $RetentionDays `
            -KeepMinimum $MinimumBackups
    }

    $result = [pscustomobject]@{
        BackupId                   = $baseName
        BackupPath                 = $dumpPath
        ChecksumPath               = $checksumPath
        TocPath                    = $tocPath
        ManifestPath               = $manifestPath
        SizeBytes                  = (Get-Item -LiteralPath $dumpPath).Length
        Sha256                     = $hash
        SourceStableDuringBackup   = $sourceStable
        ExpiredBackupSetsRemoved   = $deletedSets
    }

    Write-Host "Backup completed. Archive=$dumpPath"
    Write-Host "SHA256=$hash"
    Write-Host "SourceStableDuringBackup=$sourceStable RetentionSetsRemoved=$deletedSets"

    if ($PassThru) {
        $result
    }
} catch {
    foreach ($path in @($partialDumpPath, $partialChecksumPath, $partialTocPath, $partialManifestPath)) {
        Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue
    }
    if (-not $backupPublished) {
        foreach ($path in @($dumpPath, $checksumPath, $tocPath, $manifestPath)) {
            Remove-Item -LiteralPath $path -Force -ErrorAction SilentlyContinue
        }
    }
    throw
}
