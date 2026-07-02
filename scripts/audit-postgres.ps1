[CmdletBinding()]
param(
    [string]$DatabaseUrl = $env:DATABASE_URL,
    [string]$OutputDirectory,
    [switch]$IncludeExactCounts
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
    throw 'DATABASE_URL no está definido. Use -DatabaseUrl, configure la variable de entorno o añádalo a .env.local.'
}

$psql = Get-Command psql -ErrorAction SilentlyContinue
if ($null -eq $psql) {
    throw 'No se encontró psql en PATH. Instale las herramientas cliente de PostgreSQL antes de ejecutar la auditoría.'
}

$sqlPath = Join-Path $PSScriptRoot 'sql\postgres-audit.sql'
if (-not (Test-Path -LiteralPath $sqlPath -PathType Leaf)) {
    throw "No se encontró el SQL de auditoría: $sqlPath"
}

if ([string]::IsNullOrWhiteSpace($OutputDirectory)) {
    $OutputDirectory = Join-Path $repoRoot 'artifacts\postgres-audit'
}

New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null

$timestamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$outputPath = Join-Path $OutputDirectory "postgres-audit-$timestamp.txt"
$includeExact = if ($IncludeExactCounts) { 'true' } else { 'false' }

$arguments = @(
    '-X'
    '--set', 'ON_ERROR_STOP=1'
    '--set', "include_exact_counts=$includeExact"
    '--dbname', $DatabaseUrl
    '--file', $sqlPath
)

Write-Host 'Ejecutando auditoría PostgreSQL de solo lectura...'
if ($IncludeExactCounts) {
    Write-Warning 'Se solicitaron conteos y rangos exactos; pueden provocar escaneos completos.'
}

& $psql.Source @arguments 2>&1 | Tee-Object -FilePath $outputPath
$exitCode = $LASTEXITCODE

if ($exitCode -ne 0) {
    throw "La auditoría PostgreSQL falló con código $exitCode. Revise el reporte parcial: $outputPath"
}

Write-Host "Auditoría completada: $outputPath"
