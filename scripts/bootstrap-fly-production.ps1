[CmdletBinding()]
param(
    [string]$AppName = "albion-market-api-nachodev",
    [string]$DatabaseUrlFile = ".\secrets\deployment\neon-database-url.secret",
    [string]$IngestTokenFile = ".\secrets\deployment\ingest-current.token",
    [switch]$SkipDeploy
)

$ErrorActionPreference = "Stop"
Set-StrictMode -Version Latest

function Assert-Command {
    param([Parameter(Mandatory)][string]$Name)

    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "No se encontró '$Name' en PATH. Instala flyctl e inicia sesión antes de continuar."
    }
}

function Read-RequiredSecretFile {
    param(
        [Parameter(Mandatory)][string]$Path,
        [Parameter(Mandatory)][string]$Label
    )

    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "Falta $Label en: $Path"
    }

    $value = (Get-Content -LiteralPath $Path -Raw).Trim()
    if ([string]::IsNullOrWhiteSpace($value)) {
        throw "$Label está vacío: $Path"
    }

    return $value
}

Assert-Command -Name "flyctl"

$databaseUrl = Read-RequiredSecretFile -Path $DatabaseUrlFile -Label "DATABASE_URL de Neon"
$ingestToken = Read-RequiredSecretFile -Path $IngestTokenFile -Label "token de ingesta"

if ($ingestToken.Length -lt 32) {
    throw "El token de ingesta debe tener al menos 32 caracteres."
}

& flyctl status --app $AppName *> $null
if ($LASTEXITCODE -ne 0) {
    Write-Host "Creando aplicación Fly.io '$AppName'..."
    & flyctl apps create $AppName
    if ($LASTEXITCODE -ne 0) {
        throw "No fue posible crear la aplicación Fly.io '$AppName'. Comprueba que el nombre esté disponible."
    }
}

Write-Host "Importando secretos al vault cifrado de Fly.io..."
$secretPayload = "DATABASE_URL=$databaseUrl`nINGEST_BEARER_TOKEN=$ingestToken`n"
$secretPayload | & flyctl secrets import --app $AppName
if ($LASTEXITCODE -ne 0) {
    throw "No fue posible importar los secretos de producción."
}

$secretPayload = $null
$databaseUrl = $null
$ingestToken = $null

if (-not $SkipDeploy) {
    Write-Host "Desplegando API y ejecutando migraciones de release..."
    & flyctl deploy --app $AppName --remote-only
    if ($LASTEXITCODE -ne 0) {
        throw "El despliegue de Fly.io falló. Revisa los logs anteriores."
    }
}

Write-Host "Verificando health y readiness..."
$baseUrl = "https://$AppName.fly.dev"
Invoke-RestMethod -Method Get -Uri "$baseUrl/healthz" | Out-Host
Invoke-RestMethod -Method Get -Uri "$baseUrl/readyz" | Out-Host

Write-Host "Producción preparada en $baseUrl"
Write-Host "Conserva $IngestTokenFile fuera de Git para configurar el receiver."
