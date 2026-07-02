[CmdletBinding()]
param(
    [string]$SecretsDirectory,
    [string]$EnvironmentFile,
    [ValidatePattern('^[a-z0-9][a-z0-9_-]{0,62}$')]
    [string]$ComposeProjectName = "albion-market-api",
    [ValidatePattern('^[A-Za-z0-9._/-]+:[A-Za-z0-9._-]+$')]
    [string]$ApiImage = "albion-market-api:local",
    [ValidatePattern('^[A-Za-z0-9_]+$')]
    [string]$PostgresUser = "albion_market",
    [ValidatePattern('^[A-Za-z0-9_]+$')]
    [string]$PostgresDatabase = "albion_market",
    [ValidateRange(1024, 65535)]
    [int]$ApiHostPort = 18080,
    [string]$AllowedOrigins = "https://example.invalid",
    [ValidatePattern('^[A-Za-z0-9._-]{1,64}$')]
    [string]$IngestTokenId = "receiver-current",
    [bool]$IngestRequireHttps = $true,
    [bool]$TrustProxyHeaders = $false,
    [ValidatePattern('^[A-Za-z0-9._-]{1,128}$')]
    [string]$ImageVersion = "local",
    [switch]$Force
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$ProjectRoot = Split-Path -Parent $PSScriptRoot
if ([string]::IsNullOrWhiteSpace($SecretsDirectory)) {
    $SecretsDirectory = Join-Path $ProjectRoot "secrets\deployment"
}
if ([string]::IsNullOrWhiteSpace($EnvironmentFile)) {
    $EnvironmentFile = Join-Path $ProjectRoot "deploy\compose.env.local"
}

$SecretsDirectory = [System.IO.Path]::GetFullPath($SecretsDirectory)
$EnvironmentFile = [System.IO.Path]::GetFullPath($EnvironmentFile)
$PostgresPasswordFile = Join-Path $SecretsDirectory "postgres-password"
$DatabaseUrlFile = Join-Path $SecretsDirectory "database-url"
$IngestTokenFile = Join-Path $SecretsDirectory "ingest-current.token"
$ManagedFiles = @($PostgresPasswordFile, $DatabaseUrlFile, $IngestTokenFile, $EnvironmentFile)

if (-not $Force) {
    $Existing = @($ManagedFiles | Where-Object { Test-Path -LiteralPath $_ })
    if ($Existing.Count -gt 0) {
        throw "Deployment files already exist. Review them or run with -Force:`n$($Existing -join [Environment]::NewLine)"
    }
}

function New-RandomHex {
    param([ValidateRange(16, 256)][int]$Bytes = 32)

    $Buffer = New-Object byte[] $Bytes
    $Generator = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $Generator.GetBytes($Buffer)
    }
    finally {
        $Generator.Dispose()
    }
    return ([BitConverter]::ToString($Buffer)).Replace("-", "").ToLowerInvariant()
}

function Write-PrivateTextFile {
    param(
        [Parameter(Mandatory = $true)][string]$Path,
        [Parameter(Mandatory = $true)][string]$Value
    )

    $Parent = Split-Path -Parent $Path
    New-Item -ItemType Directory -Force -Path $Parent | Out-Null

    if ($env:OS -ne "Windows_NT") {
        & chmod 700 $Parent
        if ($LASTEXITCODE -ne 0) {
            throw "Could not restrict the secret directory $Parent"
        }
    }

    if (Test-Path -LiteralPath $Path) {
        Remove-Item -LiteralPath $Path -Force
    }

    $Utf8WithoutBom = New-Object System.Text.UTF8Encoding($false)
    [System.IO.File]::WriteAllText($Path, $Value, $Utf8WithoutBom)

    if ($env:OS -ne "Windows_NT") {
        # File-backed Compose secrets are bind mounts. The parent directory
        # remains private on the host, while the mounted file must be readable
        # by the non-root container UID.
        & chmod 444 $Path
        if ($LASTEXITCODE -ne 0) {
            throw "Could not set read-only Compose secret permissions on $Path"
        }
    }
}

function ConvertTo-ComposePath {
    param([Parameter(Mandatory = $true)][string]$Path)
    return ([System.IO.Path]::GetFullPath($Path)).Replace("\", "/")
}

$PostgresPassword = New-RandomHex -Bytes 32
$IngestToken = New-RandomHex -Bytes 32
$EscapedUser = [Uri]::EscapeDataString($PostgresUser)
$EscapedPassword = [Uri]::EscapeDataString($PostgresPassword)
$EscapedDatabase = [Uri]::EscapeDataString($PostgresDatabase)
$DatabaseUrl = "postgres://${EscapedUser}:${EscapedPassword}@postgres:5432/${EscapedDatabase}?sslmode=disable"

$Revision = (& git -C $ProjectRoot rev-parse HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($Revision)) {
    throw "Could not resolve the current Git revision."
}
$Created = (& git -C $ProjectRoot show -s --format=%cI HEAD).Trim()
if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($Created)) {
    throw "Could not resolve the current commit timestamp."
}

Write-PrivateTextFile -Path $PostgresPasswordFile -Value $PostgresPassword
Write-PrivateTextFile -Path $DatabaseUrlFile -Value $DatabaseUrl
Write-PrivateTextFile -Path $IngestTokenFile -Value $IngestToken

$EnvironmentParent = Split-Path -Parent $EnvironmentFile
New-Item -ItemType Directory -Force -Path $EnvironmentParent | Out-Null
$EnvironmentContent = @"
COMPOSE_PROJECT_NAME=$ComposeProjectName
API_IMAGE=$ApiImage
API_HOST_PORT=$ApiHostPort
API_ALLOWED_ORIGINS=$AllowedOrigins
INGEST_TOKEN_ID=$IngestTokenId
INGEST_REQUIRE_HTTPS=$($IngestRequireHttps.ToString().ToLowerInvariant())
TRUST_PROXY_HEADERS=$($TrustProxyHeaders.ToString().ToLowerInvariant())
POSTGRES_DB=$PostgresDatabase
POSTGRES_USER=$PostgresUser
POSTGRES_PASSWORD_SECRET_FILE=$(ConvertTo-ComposePath $PostgresPasswordFile)
DATABASE_URL_SECRET_FILE=$(ConvertTo-ComposePath $DatabaseUrlFile)
INGEST_TOKEN_SECRET_FILE=$(ConvertTo-ComposePath $IngestTokenFile)
IMAGE_VERSION=$ImageVersion
IMAGE_REVISION=$Revision
IMAGE_CREATED=$Created
"@
$Utf8WithoutBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText($EnvironmentFile, $EnvironmentContent.TrimStart(), $Utf8WithoutBom)

$PostgresPassword = $null
$IngestToken = $null
$DatabaseUrl = $null

Write-Host "[OK] Deployment configuration initialized."
Write-Host "EnvironmentFile=$EnvironmentFile"
Write-Host "SecretsDirectory=$SecretsDirectory"
Write-Host "No secret values were printed."
