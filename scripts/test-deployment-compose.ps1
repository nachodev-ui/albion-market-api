[CmdletBinding()]
param(
    [ValidateRange(30, 600)]
    [int]$TimeoutSeconds = 180,
    [switch]$KeepDeployment
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$ProjectRoot = Split-Path -Parent $PSScriptRoot
$ComposeFile = Join-Path $ProjectRoot "deploy\compose.yaml"
$Initializer = Join-Path $PSScriptRoot "initialize-deployment.ps1"
$RunId = "{0}-{1}" -f $PID, ([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())
$ProjectName = "albion-market-api-test-$RunId"
$ImageTag = "albion-market-api:compose-smoke-$RunId"
$TempRoot = Join-Path ([System.IO.Path]::GetTempPath()) $ProjectName
$SecretsDirectory = Join-Path $TempRoot "secrets"
$EnvironmentFile = Join-Path $TempRoot "compose.env"
$ComposeStarted = $false

function Invoke-Docker {
    param(
        [Parameter(Mandatory = $true)][string[]]$Arguments,
        [switch]$CaptureOutput
    )

    if ($CaptureOutput) {
        $Output = & docker @Arguments 2>&1
        if ($LASTEXITCODE -ne 0) {
            throw "docker $($Arguments -join ' ') failed:`n$($Output -join [Environment]::NewLine)"
        }
        return ($Output -join [Environment]::NewLine).Trim()
    }

    & docker @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "docker $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function Invoke-Compose {
    param(
        [Parameter(Mandatory = $true)][string[]]$Arguments,
        [switch]$CaptureOutput
    )

    $GlobalArguments = @("compose", "--env-file", $EnvironmentFile, "--file", $ComposeFile)
    if ($CaptureOutput) {
        $Output = & docker @GlobalArguments @Arguments 2>&1
        if ($LASTEXITCODE -ne 0) {
            throw "docker compose $($Arguments -join ' ') failed:`n$($Output -join [Environment]::NewLine)"
        }
        return ($Output -join [Environment]::NewLine).Trim()
    }

    & docker @GlobalArguments @Arguments
    if ($LASTEXITCODE -ne 0) {
        throw "docker compose $($Arguments -join ' ') failed with exit code $LASTEXITCODE"
    }
}

function Invoke-ComposeCleanup {
    param([Parameter(Mandatory = $true)][string[]]$Arguments)

    $PreviousPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = "SilentlyContinue"
        & docker compose --env-file $EnvironmentFile --file $ComposeFile @Arguments *> $null
    }
    catch {
        # Cleanup is best-effort and must not hide the original result.
    }
    finally {
        $ErrorActionPreference = $PreviousPreference
    }
}

function Get-FreeTcpPort {
    $Listener = [System.Net.Sockets.TcpListener]::new([System.Net.IPAddress]::Loopback, 0)
    try {
        $Listener.Start()
        return ([System.Net.IPEndPoint]$Listener.LocalEndpoint).Port
    }
    finally {
        $Listener.Stop()
    }
}

function Wait-ForApiHealth {
    param([Parameter(Mandatory = $true)][int]$Timeout)

    $Deadline = [DateTime]::UtcNow.AddSeconds($Timeout)
    do {
        $ApiId = Invoke-Compose -Arguments @("ps", "--quiet", "api") -CaptureOutput
        if (-not [string]::IsNullOrWhiteSpace($ApiId)) {
            $Running = Invoke-Docker -Arguments @("inspect", "--format", "{{.State.Running}}", $ApiId) -CaptureOutput
            if ($Running -ne "true") {
                $Logs = Invoke-Compose -Arguments @("logs", "--no-color", "api") -CaptureOutput
                throw "API stopped before becoming healthy:`n$Logs"
            }
            $Health = Invoke-Docker -Arguments @("inspect", "--format", "{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}", $ApiId) -CaptureOutput
            if ($Health -eq "healthy") {
                return $ApiId
            }
        }
        Start-Sleep -Seconds 1
    } while ([DateTime]::UtcNow -lt $Deadline)

    $Logs = Invoke-Compose -Arguments @("logs", "--no-color") -CaptureOutput
    throw "Compose deployment did not become healthy within $Timeout seconds:`n$Logs"
}

$HostPort = Get-FreeTcpPort

try {
    Write-Host "[1/8] Checking Docker and initializing temporary secrets..."
    Invoke-Docker -Arguments @("info") | Out-Null
    New-Item -ItemType Directory -Force -Path $TempRoot | Out-Null
    & $Initializer `
        -SecretsDirectory $SecretsDirectory `
        -EnvironmentFile $EnvironmentFile `
        -ComposeProjectName $ProjectName `
        -ApiImage $ImageTag `
        -ApiHostPort $HostPort `
        -AllowedOrigins "http://127.0.0.1:$HostPort" `
        -IngestTokenId "compose-smoke" `
        -IngestRequireHttps $false `
        -ImageVersion "compose-smoke"
    if ($LASTEXITCODE -ne 0) {
        throw "Deployment initializer failed with exit code $LASTEXITCODE"
    }

    Write-Host "[2/8] Validating the rendered Compose model..."
    Invoke-Compose -Arguments @("config", "--quiet")

    Write-Host "[3/8] Building and starting PostgreSQL, migrations and API..."
    Invoke-Compose -Arguments @("up", "--build", "--detach")
    $ComposeStarted = $true

    Write-Host "[4/8] Waiting for the API healthcheck..."
    $ApiId = Wait-ForApiHealth -Timeout $TimeoutSeconds

    $MigrationId = Invoke-Compose -Arguments @("ps", "--all", "--quiet", "migrate") -CaptureOutput
    if ([string]::IsNullOrWhiteSpace($MigrationId)) {
        throw "Migration container was not created."
    }
    $MigrationExitCode = Invoke-Docker -Arguments @("inspect", "--format", "{{.State.ExitCode}}", $MigrationId) -CaptureOutput
    if ($MigrationExitCode -ne "0") {
        $MigrationLogs = Invoke-Compose -Arguments @("logs", "--no-color", "migrate") -CaptureOutput
        throw "Migration service exited with code ${MigrationExitCode}:`n$MigrationLogs"
    }

    Write-Host "[5/8] Verifying the public health endpoint..."
    $Health = Invoke-RestMethod -Uri "http://127.0.0.1:$HostPort/healthz" -Method Get -TimeoutSec 5
    if ($Health.status -ne "ok") {
        throw "Unexpected health response: $($Health | ConvertTo-Json -Compress)"
    }

    Write-Host "[6/8] Verifying hardened runtime and mounted secrets..."
    $Inspect = (Invoke-Docker -Arguments @("inspect", $ApiId) -CaptureOutput | ConvertFrom-Json)[0]
    if ($Inspect.Config.User -ne "65532:65532") {
        throw "API runtime user is '$($Inspect.Config.User)'; expected 65532:65532."
    }
    if (-not $Inspect.HostConfig.ReadonlyRootfs) {
        throw "API root filesystem is not read-only."
    }
    if ($Inspect.HostConfig.CapDrop -notcontains "ALL") {
        throw "API container did not drop all Linux capabilities."
    }
    if ($Inspect.HostConfig.SecurityOpt -notcontains "no-new-privileges:true") {
        throw "API container is missing no-new-privileges."
    }

    $Environment = $Inspect.Config.Env -join "`n"
    foreach ($Expected in @(
        "DATABASE_URL_FILE=/run/secrets/database_url",
        "INGEST_BEARER_TOKEN_FILE=/run/secrets/ingest_token"
    )) {
        if ($Environment -notmatch [Regex]::Escape($Expected)) {
            throw "API environment is missing $Expected"
        }
    }

    $SecretDestinations = @($Inspect.Mounts | ForEach-Object { $_.Destination })
    foreach ($Destination in @("/run/secrets/database_url", "/run/secrets/ingest_token")) {
        if ($SecretDestinations -notcontains $Destination) {
            throw "Expected secret mount $Destination was not found."
        }
    }

    $PostgresPassword = [System.IO.File]::ReadAllText((Join-Path $SecretsDirectory "postgres-password")).Trim()
    $DatabaseUrl = [System.IO.File]::ReadAllText((Join-Path $SecretsDirectory "database-url")).Trim()
    $IngestToken = [System.IO.File]::ReadAllText((Join-Path $SecretsDirectory "ingest-current.token")).Trim()
    foreach ($SecretValue in @($PostgresPassword, $DatabaseUrl, $IngestToken)) {
        if ($Environment.Contains($SecretValue)) {
            throw "A secret value leaked into the API container environment."
        }
    }

    Write-Host "[7/8] Verifying graceful shutdown..."
    Invoke-Compose -Arguments @("stop", "--timeout", "15", "api")
    $ApiLogs = Invoke-Compose -Arguments @("logs", "--no-color", "api") -CaptureOutput
    if ($ApiLogs -notmatch "api\.stopped") {
        throw "The API did not log a graceful shutdown:`n$ApiLogs"
    }

    Write-Host "[8/8] Compose deployment smoke test completed."
    Write-Host "[OK] Reproducible deployment, migrations and secret mounts validated."
    Write-Host "Image=$ImageTag"
    Write-Host "RuntimeUser=$($Inspect.Config.User)"
}
catch {
    Write-Host "[ERROR] $($_.Exception.Message)" -ForegroundColor Red
    if ($ComposeStarted) {
        try {
            $Diagnostics = Invoke-Compose -Arguments @("logs", "--no-color") -CaptureOutput
            if ($Diagnostics) {
                Write-Host "--- Compose logs ---"
                Write-Host $Diagnostics
            }
        }
        catch {
            # Diagnostics are best-effort.
        }
    }
    throw
}
finally {
    if ($KeepDeployment) {
        Write-Host "Deployment preserved for debugging."
        Write-Host "EnvironmentFile=$EnvironmentFile"
        Write-Host "ProjectName=$ProjectName"
    }
    else {
        if (Test-Path -LiteralPath $EnvironmentFile) {
            Invoke-ComposeCleanup -Arguments @("down", "--volumes", "--remove-orphans", "--timeout", "15")
        }
        $PreviousPreference = $ErrorActionPreference
        try {
            $ErrorActionPreference = "SilentlyContinue"
            & docker image rm --force $ImageTag *> $null
            Remove-Item -LiteralPath $TempRoot -Recurse -Force -ErrorAction SilentlyContinue
        }
        finally {
            $ErrorActionPreference = $PreviousPreference
        }
    }
}
