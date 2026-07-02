[CmdletBinding()]
param(
    [string]$DatabaseUrl = $env:DATABASE_URL
)

$ErrorActionPreference = 'Stop'
$repoRoot = Split-Path -Parent $PSScriptRoot

if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    $localEnvPath = Join-Path $repoRoot '.env.local'
    if (Test-Path -LiteralPath $localEnvPath -PathType Leaf) {
        $databaseLine = Get-Content -LiteralPath $localEnvPath |
            Where-Object { $_ -match '^\s*DATABASE_URL\s*=' } |
            Select-Object -First 1

        if ($null -ne $databaseLine) {
            $DatabaseUrl = ($databaseLine -split '=', 2)[1].Trim()
            if (
                $DatabaseUrl.Length -ge 2 -and
                (($DatabaseUrl.StartsWith('"') -and $DatabaseUrl.EndsWith('"')) -or
                 ($DatabaseUrl.StartsWith("'") -and $DatabaseUrl.EndsWith("'")))
            ) {
                $DatabaseUrl = $DatabaseUrl.Substring(1, $DatabaseUrl.Length - 2)
            }
        }
    }
}

if ([string]::IsNullOrWhiteSpace($DatabaseUrl)) {
    throw 'DATABASE_URL is not defined. Use -DatabaseUrl, set the environment variable, or add it to .env.local.'
}

$psql = Get-Command psql -ErrorAction SilentlyContinue
if ($null -eq $psql) {
    throw 'psql was not found in PATH.'
}

$migrationPath = Join-Path $repoRoot 'migrations\000005_retention_indexes.sql'
if (-not (Test-Path -LiteralPath $migrationPath -PathType Leaf)) {
    throw "Migration was not found: $migrationPath"
}

$arguments = @(
    '-X'
    '--set', 'ON_ERROR_STOP=1'
    '--dbname', $DatabaseUrl
    '--file', $migrationPath
)

Write-Host 'Applying PostgreSQL retention indexes...'
& $psql.Source @arguments

if ($LASTEXITCODE -ne 0) {
    throw "Retention index migration failed with exit code $LASTEXITCODE."
}

Write-Host 'Retention index migration completed.'
