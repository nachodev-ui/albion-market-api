[CmdletBinding()]
param(
    [ValidateRange(60, 600)]
    [int]$TimeoutSeconds = 240,
    [switch]$KeepDeployment
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Add-Type -AssemblyName System.Net.Http

$ProjectRoot = Split-Path -Parent $PSScriptRoot
$ComposeFile = Join-Path $ProjectRoot "deploy\compose.yaml"
$Initializer = Join-Path $PSScriptRoot "initialize-deployment.ps1"
$PrometheusDirectory = Join-Path $ProjectRoot "observability\prometheus"
$AlertmanagerConfig = Join-Path $ProjectRoot "observability\alertmanager\alertmanager.yml"
$RunId = "{0}-{1}" -f $PID, ([DateTimeOffset]::UtcNow.ToUnixTimeSeconds())
$ProjectName = "albion-market-api-observability-test-$RunId"
$ImageTag = "albion-market-api:observability-smoke-$RunId"
$TempRoot = Join-Path ([System.IO.Path]::GetTempPath()) $ProjectName
$SecretsDirectory = Join-Path $TempRoot "secrets"
$EnvironmentFile = Join-Path $TempRoot "compose.env"
$ComposeStarted = $false

$PrometheusImage = "prom/prometheus@sha256:a75c5a35bc21d7afe69551eefa3cb1e1fb1775fe759408007a66b54ec3de1f29"
$AlertmanagerImage = "prom/alertmanager@sha256:cc54cc450174ada901b32eb2538de5fc70ee259a1ac551ed38023f2ca2ad00e3"

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

    $GlobalArguments = @(
        "compose",
        "--env-file", $EnvironmentFile,
        "--file", $ComposeFile,
        "--profile", "observability"
    )
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
    $PreviousPreference = $ErrorActionPreference
    try {
        $ErrorActionPreference = "SilentlyContinue"
        & docker compose `
            --env-file $EnvironmentFile `
            --file $ComposeFile `
            --profile observability `
            down --volumes --remove-orphans *> $null
    }
    catch {
        # Cleanup is best-effort and must not hide the original result.
    }
    finally {
        $ErrorActionPreference = $PreviousPreference
    }
}

function Get-FreeTcpPorts {
    param([ValidateRange(1, 16)][int]$Count)

    $Listeners = @()
    try {
        for ($Index = 0; $Index -lt $Count; $Index++) {
            $Listener = [System.Net.Sockets.TcpListener]::new(
                [System.Net.IPAddress]::Loopback,
                0
            )
            $Listener.Start()
            $Listeners += $Listener
        }
        return @($Listeners | ForEach-Object {
            ([System.Net.IPEndPoint]$_.LocalEndpoint).Port
        })
    }
    finally {
        foreach ($Listener in $Listeners) {
            $Listener.Stop()
        }
    }
}

function Assert-PublishedPort {
    param(
        [Parameter(Mandatory = $true)][string]$Service,
        [Parameter(Mandatory = $true)][int]$ContainerPort,
        [Parameter(Mandatory = $true)][int]$ExpectedHostPort
    )

    $ContainerId = Invoke-Compose -Arguments @("ps", "--quiet", $Service) -CaptureOutput
    if ([string]::IsNullOrWhiteSpace($ContainerId)) {
        throw "Service $Service does not have a running container."
    }

    $Inspect = (Invoke-Docker -Arguments @("inspect", $ContainerId) -CaptureOutput | ConvertFrom-Json)[0]
    $BindingKey = "{0}/tcp" -f $ContainerPort
    $BindingProperty = $Inspect.HostConfig.PortBindings.PSObject.Properties[$BindingKey]
    $Bindings = @()
    if ($null -ne $BindingProperty -and $null -ne $BindingProperty.Value) {
        $Bindings = @($BindingProperty.Value)
    }

    $ExpectedHostPortText = $ExpectedHostPort.ToString([System.Globalization.CultureInfo]::InvariantCulture)
    $MatchingBindings = @($Bindings | Where-Object {
        $_.HostIp -eq "127.0.0.1" -and $_.HostPort -eq $ExpectedHostPortText
    })
    if ($MatchingBindings.Count -ne 1) {
        $Actual = if ($Bindings.Count -eq 0) {
            "<none>"
        }
        else {
            ($Bindings | ForEach-Object { "{0}:{1}" -f $_.HostIp, $_.HostPort }) -join ", "
        }
        $Status = Invoke-Compose -Arguments @("ps", "--all", $Service) -CaptureOutput
        throw "$Service container port $ContainerPort was not published as 127.0.0.1:$ExpectedHostPort. Actual: '$Actual'`nService status:`n$Status"
    }

    Write-Host "    [OK] $Service published 127.0.0.1:$ExpectedHostPort -> $ContainerPort/tcp."
}

function Wait-ForHttp {
    param(
        [Parameter(Mandatory = $true)][string]$Name,
        [Parameter(Mandatory = $true)][string]$Service,
        [Parameter(Mandatory = $true)][string]$Uri,
        [Parameter(Mandatory = $true)][int]$Timeout
    )

    Write-Host "  - Waiting for $Name at $Uri"

    $Handler = [System.Net.Http.HttpClientHandler]::new()
    $Handler.UseProxy = $false
    $Client = [System.Net.Http.HttpClient]::new($Handler)
    $Client.Timeout = [TimeSpan]::FromSeconds(5)
    $Deadline = [DateTime]::UtcNow.AddSeconds($Timeout)
    $LastFailure = "no response received"

    try {
        do {
            try {
                $Response = $Client.GetAsync($Uri).GetAwaiter().GetResult()
                try {
                    if ($Response.IsSuccessStatusCode) {
                        Write-Host "    [OK] $Name is ready."
                        return
                    }
                    $LastFailure = "HTTP $([int]$Response.StatusCode) $($Response.ReasonPhrase)"
                }
                finally {
                    $Response.Dispose()
                }
            }
            catch {
                $LastFailure = $_.Exception.Message
            }

            Start-Sleep -Seconds 1
        } while ([DateTime]::UtcNow -lt $Deadline)
    }
    finally {
        $Client.Dispose()
        $Handler.Dispose()
    }

    $Status = Invoke-Compose -Arguments @("ps", "--all", $Service) -CaptureOutput
    $Logs = Invoke-Compose -Arguments @("logs", "--no-color", "--tail", "120", $Service) -CaptureOutput
    throw "$Name at $Uri did not become ready within $Timeout seconds. Last failure: $LastFailure`nService status:`n$Status`nRecent logs:`n$Logs"
}

function Wait-ForPrometheusTarget {
    param(
        [Parameter(Mandatory = $true)][string]$Uri,
        [Parameter(Mandatory = $true)][string]$JobName,
        [Parameter(Mandatory = $true)][int]$Timeout
    )

    Write-Host "  - Waiting for Prometheus target '$JobName' to become up..."

    $Handler = [System.Net.Http.HttpClientHandler]::new()
    $Handler.UseProxy = $false
    $Client = [System.Net.Http.HttpClient]::new($Handler)
    $Client.Timeout = [TimeSpan]::FromSeconds(5)
    $Deadline = [DateTime]::UtcNow.AddSeconds($Timeout)
    $LastFailure = "target was not reported yet"

    try {
        do {
            try {
                $Response = $Client.GetAsync($Uri).GetAwaiter().GetResult()
                try {
                    if (-not $Response.IsSuccessStatusCode) {
                        $LastFailure = "HTTP $([int]$Response.StatusCode) $($Response.ReasonPhrase)"
                    }
                    else {
                        $Content = $Response.Content.ReadAsStringAsync().GetAwaiter().GetResult()
                        $Payload = $Content | ConvertFrom-Json
                        $Targets = @($Payload.data.activeTargets | Where-Object {
                            $_.labels.job -eq $JobName
                        })

                        $UpTargets = @($Targets | Where-Object { $_.health -eq "up" })
                        if ($Targets.Count -eq 1 -and $UpTargets.Count -eq 1) {
                            Write-Host "    [OK] Prometheus target '$JobName' is up."
                            return
                        }

                        if ($Targets.Count -eq 0) {
                            $LastFailure = "target was not reported yet"
                        }
                        else {
                            $TargetSummaries = @($Targets | ForEach-Object {
                                $ErrorText = if ([string]::IsNullOrWhiteSpace([string]$_.lastError)) {
                                    "<none>"
                                }
                                else {
                                    [string]$_.lastError
                                }
                                "scrapeUrl=$($_.scrapeUrl) health=$($_.health) lastError=$ErrorText"
                            })
                            $LastFailure = $TargetSummaries -join "; "
                        }
                    }
                }
                finally {
                    $Response.Dispose()
                }
            }
            catch {
                $LastFailure = $_.Exception.Message
            }

            Start-Sleep -Seconds 1
        } while ([DateTime]::UtcNow -lt $Deadline)
    }
    finally {
        $Client.Dispose()
        $Handler.Dispose()
    }

    $Status = Invoke-Compose -Arguments @("ps", "--all", "prometheus", "api") -CaptureOutput
    $Logs = Invoke-Compose -Arguments @("logs", "--no-color", "--tail", "120", "prometheus", "api") -CaptureOutput
    throw "Prometheus target '$JobName' did not become up within $Timeout seconds. Last state: $LastFailure`nService status:`n$Status`nRecent logs:`n$Logs"
}

function Assert-HardenedService {
    param([Parameter(Mandatory = $true)][string]$Service)

    $ContainerId = Invoke-Compose -Arguments @("ps", "--quiet", $Service) -CaptureOutput
    if ([string]::IsNullOrWhiteSpace($ContainerId)) {
        throw "Service $Service does not have a running container."
    }

    $Inspect = (Invoke-Docker -Arguments @("inspect", $ContainerId) -CaptureOutput | ConvertFrom-Json)[0]
    if (-not $Inspect.HostConfig.ReadonlyRootfs) {
        throw "$Service root filesystem is not read-only."
    }
    if ($Inspect.HostConfig.CapDrop -notcontains "ALL") {
        throw "$Service did not drop all Linux capabilities."
    }
    if ($Inspect.HostConfig.SecurityOpt -notcontains "no-new-privileges:true") {
        throw "$Service is missing no-new-privileges."
    }
}

$Ports = Get-FreeTcpPorts -Count 4
$ApiPort = $Ports[0]
$PrometheusPort = $Ports[1]
$AlertmanagerPort = $Ports[2]
$GrafanaPort = $Ports[3]

try {
    Write-Host "[1/12] Checking Docker and initializing isolated deployment..."
    Invoke-Docker -Arguments @("info") | Out-Null
    New-Item -ItemType Directory -Force -Path $TempRoot | Out-Null
    & $Initializer `
        -SecretsDirectory $SecretsDirectory `
        -EnvironmentFile $EnvironmentFile `
        -ComposeProjectName $ProjectName `
        -ApiImage $ImageTag `
        -ApiHostPort $ApiPort `
        -PrometheusHostPort $PrometheusPort `
        -AlertmanagerHostPort $AlertmanagerPort `
        -GrafanaHostPort $GrafanaPort `
        -PrometheusRetentionTime "1d" `
        -AllowedOrigins "http://127.0.0.1:$ApiPort" `
        -IngestTokenId "observability-smoke" `
        -IngestRequireHttps $false `
        -ImageVersion "observability-smoke"
    if ($LASTEXITCODE -ne 0) {
        throw "Deployment initializer failed with exit code $LASTEXITCODE"
    }

    Write-Host "[2/12] Validating Compose profile rendering..."
    Invoke-Compose -Arguments @("config", "--quiet")

    Write-Host "[3/12] Validating Prometheus configuration and alert rules..."
    Invoke-Docker -Arguments @(
        "run", "--rm",
        "--entrypoint", "/bin/promtool",
        "--mount", "type=bind,source=$PrometheusDirectory,target=/etc/prometheus,readonly",
        $PrometheusImage,
        "check", "config", "/etc/prometheus/prometheus.yml"
    )
    Invoke-Docker -Arguments @(
        "run", "--rm",
        "--entrypoint", "/bin/promtool",
        "--mount", "type=bind,source=$PrometheusDirectory,target=/etc/prometheus,readonly",
        $PrometheusImage,
        "test", "rules", "/etc/prometheus/tests/albion-market-api.rules.test.yml"
    )

    Write-Host "[4/12] Validating Alertmanager configuration..."
    Invoke-Docker -Arguments @(
        "run", "--rm",
        "--entrypoint", "/bin/amtool",
        "--mount", "type=bind,source=$AlertmanagerConfig,target=/etc/alertmanager/alertmanager.yml,readonly",
        $AlertmanagerImage,
        "check-config", "/etc/alertmanager/alertmanager.yml"
    )

    Write-Host "[5/12] Building and starting API plus observability profile..."
    Invoke-Compose -Arguments @("up", "--build", "--detach")
    $ComposeStarted = $true

    Write-Host "[6/12] Verifying published ports and waiting for services..."
    Assert-PublishedPort -Service "api" -ContainerPort 8080 -ExpectedHostPort $ApiPort
    Assert-PublishedPort -Service "prometheus" -ContainerPort 9090 -ExpectedHostPort $PrometheusPort
    Assert-PublishedPort -Service "alertmanager" -ContainerPort 9093 -ExpectedHostPort $AlertmanagerPort
    Assert-PublishedPort -Service "grafana" -ContainerPort 3000 -ExpectedHostPort $GrafanaPort

    Wait-ForHttp -Name "API readiness" -Service "api" -Uri "http://127.0.0.1:$ApiPort/readyz" -Timeout $TimeoutSeconds
    Wait-ForHttp -Name "Prometheus" -Service "prometheus" -Uri "http://127.0.0.1:$PrometheusPort/-/ready" -Timeout $TimeoutSeconds
    Wait-ForHttp -Name "Alertmanager" -Service "alertmanager" -Uri "http://127.0.0.1:$AlertmanagerPort/-/ready" -Timeout $TimeoutSeconds
    Wait-ForHttp -Name "Grafana" -Service "grafana" -Uri "http://127.0.0.1:$GrafanaPort/api/health" -Timeout $TimeoutSeconds

    Write-Host "[7/12] Verifying Prometheus scrape targets..."
    Wait-ForPrometheusTarget `
        -Uri "http://127.0.0.1:$PrometheusPort/api/v1/targets" `
        -JobName "albion-market-api" `
        -Timeout $TimeoutSeconds

    Write-Host "[8/12] Verifying loaded alert rules..."
    $Rules = Invoke-RestMethod `
        -Uri "http://127.0.0.1:$PrometheusPort/api/v1/rules?type=alert" `
        -Method Get `
        -TimeoutSec 10
    $LoadedRuleNames = @($Rules.data.groups.rules | ForEach-Object { $_.name })
    foreach ($ExpectedRule in @(
        "AlbionMarketAPIUnavailable",
        "AlbionMarketAPINotReady",
        "AlbionMarketAPIHighHTTP5xxRate",
        "AlbionMarketAPIHighHTTPLatency",
        "AlbionMarketAPIAuthenticationFailuresHigh",
        "AlbionMarketAPIIngestTrafficStopped",
        "AlbionMarketAPINoSuccessfulIngest",
        "AlbionMarketAPIIngestErrorsRepeated",
        "AlbionMarketAPIDatabasePoolSaturated",
        "AlbionMarketAPIDatabaseAcquireSlow",
        "AlbionMarketAPIIngestPersistenceErrorsRepeated",
        "AlbionMarketAPIRepeatedRestarts"
    )) {
        if ($LoadedRuleNames -notcontains $ExpectedRule) {
            throw "Prometheus did not load alert rule $ExpectedRule."
        }
    }

    Write-Host "[9/12] Verifying Alertmanager API..."
    $AlertmanagerStatus = Invoke-RestMethod `
        -Uri "http://127.0.0.1:$AlertmanagerPort/api/v2/status" `
        -Method Get `
        -TimeoutSec 10
    if ($null -eq $AlertmanagerStatus.cluster) {
        throw "Alertmanager status response did not contain cluster information."
    }

    Write-Host "[10/12] Verifying provisioned Grafana dashboard..."
    $Dashboard = Invoke-RestMethod `
        -Uri "http://127.0.0.1:$GrafanaPort/api/dashboards/uid/albion-market-api-overview" `
        -Method Get `
        -TimeoutSec 10
    if ($Dashboard.dashboard.uid -ne "albion-market-api-overview") {
        throw "Grafana did not provision the expected dashboard."
    }

    Write-Host "[11/12] Verifying hardened observability containers..."
    foreach ($Service in @("prometheus", "alertmanager", "grafana")) {
        Assert-HardenedService -Service $Service
    }

    Write-Host "[12/12] Observability smoke test completed."
    Write-Host "[OK] Prometheus, Alertmanager, Grafana, alert rules and dashboard validated."
    Write-Host "Prometheus=http://127.0.0.1:$PrometheusPort"
    Write-Host "Alertmanager=http://127.0.0.1:$AlertmanagerPort"
    Write-Host "Grafana=http://127.0.0.1:$GrafanaPort"
}
finally {
    if ($ComposeStarted -and -not $KeepDeployment) {
        Invoke-ComposeCleanup
    }
    if (-not $KeepDeployment -and (Test-Path -LiteralPath $TempRoot)) {
        Remove-Item -LiteralPath $TempRoot -Recurse -Force -ErrorAction SilentlyContinue
    }
    elseif ($KeepDeployment) {
        Write-Host "Deployment retained. EnvironmentFile=$EnvironmentFile"
    }
}
