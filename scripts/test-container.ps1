[CmdletBinding()]
param(
    [string]$ImageTag = "albion-market-api:local-smoke",
    [string]$PostgresImage = "postgres:17.10-alpine3.23@sha256:3da929dcc3e63e3f0cc81fdb114c073ca48bfc7280e83a6324d5652fbee63742",
    [ValidateRange(1024, 65535)]
    [int]$HostPort = 18080,
    [switch]$KeepContainers
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$ProjectRoot = Split-Path -Parent $PSScriptRoot
$RunId = "{0}-{1}" -f $PID, ([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())
$NetworkName = "albion-market-api-smoke-$RunId"
$DatabaseContainer = "albion-market-postgres-$RunId"
$ApiContainer = "albion-market-api-$RunId"
$MigrationsPath = Join-Path $ProjectRoot "migrations"
$SecretDirectory = Join-Path ([System.IO.Path]::GetTempPath()) "albion-market-api-smoke-secrets-$RunId"
$PostgresPasswordPath = Join-Path $SecretDirectory "postgres-password"
$DatabaseUrlPath = Join-Path $SecretDirectory "database-url"
$IngestTokenPath = Join-Path $SecretDirectory "ingest-token"
$NetworkCreated = $false
$DatabaseCreated = $false
$ApiCreated = $false

function Invoke-Docker {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments,
        [switch]$CaptureOutput
    )

    if ($CaptureOutput) {
        $output = & docker @Arguments 2>&1
        if ($LASTEXITCODE -ne 0) {
            throw "docker $($Arguments -join ' ') failed:`n$($output -join [Environment]::NewLine)"
        }
        return ($output -join [Environment]::NewLine).Trim()
    }

    & docker @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "docker $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}


function Invoke-DockerCleanup {
    param(
        [Parameter(Mandatory = $true)]
        [string[]]$Arguments
    )

    $previousErrorActionPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = "SilentlyContinue"
        & docker @Arguments *> $null
    }
    catch {
        # Cleanup is best-effort and must not hide the original failure.
    }
    finally {
        $ErrorActionPreference = $previousErrorActionPreference
    }
}

function New-RandomHex {
    param([ValidateRange(16, 256)][int]$Bytes = 32)

    $buffer = New-Object byte[] $Bytes
    $generator = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try {
        $generator.GetBytes($buffer)
    }
    finally {
        $generator.Dispose()
    }
    return ([BitConverter]::ToString($buffer)).Replace("-", "").ToLowerInvariant()
}

function Write-SecretFile {
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
        & chmod 444 $Path
        if ($LASTEXITCODE -ne 0) {
            throw "Could not set read-only secret permissions on $Path"
        }
    }
}

function Wait-ContainerHealth {
    param(
        [Parameter(Mandatory = $true)][string]$Container,
        [ValidateRange(1, 300)][int]$TimeoutSeconds = 60
    )

    $deadline = [DateTime]::UtcNow.AddSeconds($TimeoutSeconds)
    do {
        $running = Invoke-Docker -Arguments @("inspect", "--format", "{{.State.Running}}", $Container) -CaptureOutput
        if ($running -ne "true") {
            $logs = Invoke-Docker -Arguments @("logs", $Container) -CaptureOutput
            throw "Container $Container stopped before becoming healthy:`n$logs"
        }

        $health = Invoke-Docker -Arguments @("inspect", "--format", "{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", $Container) -CaptureOutput
        if ($health -eq "healthy") {
            return
        }
        Start-Sleep -Seconds 1
    } while ([DateTime]::UtcNow -lt $deadline)

    $logs = Invoke-Docker -Arguments @("logs", $Container) -CaptureOutput
    throw "Container $Container did not become healthy within $TimeoutSeconds seconds:`n$logs"
}

$PostgresPassword = New-RandomHex -Bytes 24
$IngestToken = New-RandomHex -Bytes 32
$DatabaseUrl = "postgres://albion_market:${PostgresPassword}@${DatabaseContainer}:5432/albion_market?sslmode=disable"

try {
    Write-SecretFile -Path $PostgresPasswordPath -Value $PostgresPassword
    Write-SecretFile -Path $DatabaseUrlPath -Value $DatabaseUrl
    Write-SecretFile -Path $IngestTokenPath -Value $IngestToken

    $Created = (& git -C $ProjectRoot show -s --format=%cI HEAD).Trim()
    $Revision = (& git -C $ProjectRoot rev-parse HEAD).Trim()
    if ($LASTEXITCODE -ne 0 -or [string]::IsNullOrWhiteSpace($Revision)) {
        throw "Could not resolve the current Git revision."
    }

    Write-Host "[1/9] Creating isolated Docker network..."
    Invoke-Docker -Arguments @("network", "create", $NetworkName)
    $NetworkCreated = $true

    Write-Host "[2/9] Starting temporary PostgreSQL..."
    Invoke-Docker -Arguments @(
        "run", "--detach",
        "--name", $DatabaseContainer,
        "--network", $NetworkName,
        "--mount", "type=bind,source=$MigrationsPath,target=/migrations,readonly",
        "--mount", "type=bind,source=$PostgresPasswordPath,target=/run/secrets/postgres_password,readonly",
        "--env", "POSTGRES_DB=albion_market",
        "--env", "POSTGRES_USER=albion_market",
        "--env", "POSTGRES_PASSWORD_FILE=/run/secrets/postgres_password",
        "--health-cmd", "pg_isready -U albion_market -d albion_market",
        "--health-interval", "2s",
        "--health-timeout", "3s",
        "--health-retries", "30",
        $PostgresImage
    )
    $DatabaseCreated = $true
    Wait-ContainerHealth -Container $DatabaseContainer -TimeoutSeconds 90

    Write-Host "[3/9] Applying migrations in lexical order..."
    $Migrations = @(Get-ChildItem -LiteralPath $MigrationsPath -Filter "*.sql" -File | Sort-Object Name)
    if ($Migrations.Count -eq 0) {
        throw "No SQL migrations were found in $MigrationsPath."
    }

    foreach ($Migration in $Migrations) {
        Write-Host "Applying $($Migration.Name)"
        Invoke-Docker -Arguments @(
            "exec", $DatabaseContainer,
            "psql",
            "--username", "albion_market",
            "--dbname", "albion_market",
            "--set", "ON_ERROR_STOP=1",
            "--file", "/migrations/$($Migration.Name)"
        )
    }

    Write-Host "[4/9] Building the production image..."
    Invoke-Docker -Arguments @(
        "build", "--pull",
        "--tag", $ImageTag,
        "--build-arg", "VERSION=local-smoke",
        "--build-arg", "REVISION=$Revision",
        "--build-arg", "CREATED=$Created",
        $ProjectRoot
    )

    $ConfiguredUser = Invoke-Docker -Arguments @("image", "inspect", "--format", "{{.Config.User}}", $ImageTag) -CaptureOutput
    if ($ConfiguredUser -ne "65532:65532") {
        throw "Image user is '$ConfiguredUser'; expected 65532:65532."
    }

    Write-Host "[5/9] Starting the API with a hardened runtime..."
    Invoke-Docker -Arguments @(
        "run", "--detach",
        "--name", $ApiContainer,
        "--network", $NetworkName,
        "--publish", "127.0.0.1:${HostPort}:8080",
        "--read-only",
        "--cap-drop", "ALL",
        "--security-opt", "no-new-privileges",
        "--pids-limit", "128",
        "--tmpfs", "/tmp:rw,noexec,nosuid,size=16m",
        "--env", "APP_ENV=production",
        "--env", "HTTP_ADDR=:8080",
        "--env", "LOAD_DOTENV=false",
        "--env", "LOG_COLOR=never",
        "--env", "LOG_FORMAT=json",
        "--mount", "type=bind,source=$DatabaseUrlPath,target=/run/secrets/database_url,readonly",
        "--mount", "type=bind,source=$IngestTokenPath,target=/run/secrets/ingest_token,readonly",
        "--env", "DATABASE_URL_FILE=/run/secrets/database_url",
        "--env", "INGEST_BEARER_TOKEN_FILE=/run/secrets/ingest_token",
        "--env", "INGEST_BEARER_TOKEN_ID=local-smoke",
        "--env", "INGEST_REQUIRE_HTTPS=false",
        "--env", "CORS_ALLOWED_ORIGINS=http://127.0.0.1:5173",
        "--env", "RATE_LIMIT_ENABLED=false",
        $ImageTag
    )
    $ApiCreated = $true
    Wait-ContainerHealth -Container $ApiContainer -TimeoutSeconds 60

    $RuntimeEnvironment = Invoke-Docker -Arguments @("inspect", "--format", "{{json .Config.Env}}", $ApiContainer) -CaptureOutput
    foreach ($SecretValue in @($PostgresPassword, $DatabaseUrl, $IngestToken)) {
        if ($RuntimeEnvironment.Contains($SecretValue)) {
            throw "A secret value leaked into the API container environment."
        }
    }

    Write-Host "[6/9] Checking the liveness endpoint..."
    $Health = Invoke-RestMethod -Uri "http://127.0.0.1:$HostPort/healthz" -Method Get -TimeoutSec 5
    if ($Health.status -ne "ok") {
        throw "Unexpected health response: $($Health | ConvertTo-Json -Compress)"
    }

    Write-Host "[7/9] Checking the readiness endpoint..."
    $Readiness = Invoke-RestMethod -Uri "http://127.0.0.1:$HostPort/readyz" -Method Get -TimeoutSec 5
    if ($Readiness.status -ne "ok") {
        throw "Unexpected readiness response: $($Readiness | ConvertTo-Json -Compress)"
    }

    Write-Host "[8/9] Checking the Prometheus metrics endpoint..."
    $MetricsBody = (Invoke-WebRequest -UseBasicParsing -Uri "http://127.0.0.1:$HostPort/metrics" -Method Get -TimeoutSec 5).Content
    foreach ($ExpectedMetric in @(
        "albion_market_api_build_info",
        "albion_market_api_http_requests_total",
        "albion_market_api_database_ready 1"
    )) {
        if ($MetricsBody -notmatch [Regex]::Escape($ExpectedMetric)) {
            throw "Metrics output is missing $ExpectedMetric"
        }
    }
    foreach ($SecretValue in @($PostgresPassword, $DatabaseUrl, $IngestToken)) {
        if ($MetricsBody.Contains($SecretValue)) {
            throw "A secret value leaked into the metrics endpoint."
        }
    }

    Write-Host "[9/9] Verifying graceful SIGTERM shutdown..."
    Invoke-Docker -Arguments @("stop", "--timeout", "15", $ApiContainer)
    $ApiLogs = Invoke-Docker -Arguments @("logs", $ApiContainer) -CaptureOutput
    if ($ApiLogs -notmatch "api\.stopped") {
        throw "The API did not log a graceful shutdown:`n$ApiLogs"
    }

    Write-Host "[OK] Container image built and smoke-tested successfully."
    Write-Host "Image=$ImageTag"
    Write-Host "RuntimeUser=$ConfiguredUser"
}
catch {
    Write-Host "[ERROR] $($_.Exception.Message)" -ForegroundColor Red
    if ($ApiCreated) {
        try {
            $logs = Invoke-Docker -Arguments @("logs", $ApiContainer) -CaptureOutput
            if ($logs) {
                Write-Host "--- API logs ---"
                Write-Host $logs
            }
        }
        catch {
            # Diagnostics are best-effort.
        }
    }
    throw
}
finally {
    if (-not $KeepContainers) {
        if ($ApiCreated) {
            Invoke-DockerCleanup -Arguments @("rm", "--force", $ApiContainer)
        }
        if ($DatabaseCreated) {
            Invoke-DockerCleanup -Arguments @("rm", "--force", $DatabaseContainer)
        }
        if ($NetworkCreated) {
            Invoke-DockerCleanup -Arguments @("network", "rm", $NetworkName)
        }
        Remove-Item -LiteralPath $SecretDirectory -Recurse -Force -ErrorAction SilentlyContinue
    }
    else {
        Write-Host "Containers and network were preserved because -KeepContainers was used."
        Write-Host "API container=$ApiContainer"
        Write-Host "Database container=$DatabaseContainer"
        Write-Host "Network=$NetworkName"
        Write-Host "SecretDirectory=$SecretDirectory"
    }
}
