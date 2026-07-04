[CmdletBinding()]
param(
    [string]$BaseUrl = "http://127.0.0.1:8080",
    [ValidateSet("west", "east", "europe")]
    [string]$Server = "west",
    [ValidateRange(1, 32767)]
    [int]$LocationId = 4002,
    [ValidateRange(1, 5000)]
    [int]$BatchSize = 500,
    [ValidateRange(0, 20)]
    [int]$WarmupRounds = 1,
    [ValidateRange(1, 100)]
    [int]$Rounds = 25,
    [string]$Token = $env:ALBION_API_TOKEN,
    [string]$OutputDirectory = "."
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

function Get-PlainTextFromSecureString {
    param([Parameter(Mandatory = $true)][Security.SecureString]$SecureString)

    $ptr = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($SecureString)
    try {
        return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($ptr)
    }
    finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($ptr)
    }
}

function Get-Percentile {
    param(
        [Parameter(Mandatory = $true)][double[]]$Values,
        [Parameter(Mandatory = $true)][ValidateRange(0, 1)][double]$Percentile
    )

    if ($Values.Count -eq 0) { return $null }
    $sorted = @($Values | Sort-Object)
    $index = [Math]::Ceiling($Percentile * $sorted.Count) - 1
    if ($index -lt 0) { $index = 0 }
    if ($index -ge $sorted.Count) { $index = $sorted.Count - 1 }
    return [double]$sorted[$index]
}

function ConvertFrom-ServerTiming {
    param([AllowNull()][object]$HeaderValue)

    $result = @{}
    $raw = (@($HeaderValue) -join ",")
    foreach ($segment in ($raw -split ",")) {
        if ($segment -match '^\s*(?<name>[A-Za-z0-9_-]+)\s*;\s*dur=(?<duration>[0-9]+(?:\.[0-9]+)?)\s*$') {
            $result[$Matches.name] = [double]::Parse(
                $Matches.duration,
                [Globalization.CultureInfo]::InvariantCulture
            )
        }
    }
    return $result
}

function New-BenchmarkEntries {
    param(
        [Parameter(Mandatory = $true)][string]$ItemPrefix,
        [Parameter(Mandatory = $true)][datetime]$ObservedAt
    )

    $timestamp = $ObservedAt.ToUniversalTime().ToString("o")
    $entries = New-Object System.Collections.Generic.List[object]
    for ($index = 0; $index -lt $BatchSize; $index++) {
        $entries.Add([ordered]@{
            observed_at       = $timestamp
            location_id       = $LocationId
            item_key          = ("{0}{1:D4}" -f $ItemPrefix, $index)
            quality           = 1
            sell_price_min    = [long](100000 + $index)
            sell_price_min_at = $timestamp
            buy_price_max     = [long](90000 + $index)
            buy_price_max_at  = $timestamp
        })
    }
    return $entries.ToArray()
}

function Invoke-PhaseMeasurement {
    param(
        [Parameter(Mandatory = $true)][int]$Round,
        [Parameter(Mandatory = $true)][string]$RunPrefix
    )

    $requestId = [guid]::NewGuid().ToString()
    $observedAt = (Get-Date).ToUniversalTime().AddSeconds($Round)
    $entries = New-BenchmarkEntries `
        -ItemPrefix ("{0}R{1:D3}-" -f $RunPrefix, $Round) `
        -ObservedAt $observedAt
    $body = [ordered]@{
        request_id = $requestId
        server     = $Server
        entries    = $entries
    } | ConvertTo-Json -Depth 10 -Compress

    $stopwatch = [Diagnostics.Stopwatch]::StartNew()
    try {
        $response = Invoke-WebRequest `
            -UseBasicParsing `
            -Method Post `
            -Uri "$BaseUrl/api/v1/ingest/prices" `
            -Headers $headers `
            -ContentType "application/json" `
            -Body $body
    }
    catch {
        $stopwatch.Stop()
        throw "Fallo en ronda ${Round}: $($_.Exception.Message)"
    }
    $stopwatch.Stop()

    $json = $response.Content | ConvertFrom-Json
    $timing = ConvertFrom-ServerTiming -HeaderValue $response.Headers["Server-Timing"]
    $apiMs = if ($timing.ContainsKey("api")) { [double]$timing["api"] } else { $null }
    $transactionMs = if ($timing.ContainsKey("postgres-tx")) { [double]$timing["postgres-tx"] } else { $null }
    $commitMs = if ($timing.ContainsKey("postgres-commit")) { [double]$timing["postgres-commit"] } else { $null }
    $roundTripMs = $stopwatch.Elapsed.TotalMilliseconds
    $overheadMs = if ($null -ne $apiMs) {
        [Math]::Max(0.0, $roundTripMs - $apiMs)
    }
    else {
        $null
    }

    $valid = (
        [int]$response.StatusCode -eq 202 -and
        [int]$json.accepted -eq $BatchSize -and
        [bool]$json.duplicate -eq $false -and
        [int64]$json.current_rows_touched -eq $BatchSize -and
        $null -ne $apiMs -and
        $null -ne $transactionMs -and
        $null -ne $commitMs
    )

    $script:requestIds.Add($requestId)
    return [pscustomobject]@{
        round                              = $Round
        http                               = [int]$response.StatusCode
        request_id                         = [string]$json.request_id
        accepted                           = [int]$json.accepted
        current_rows_touched               = [int64]$json.current_rows_touched
        round_trip_total_ms                 = [Math]::Round($roundTripMs, 3)
        api_processing_ms                   = if ($null -ne $apiMs) { [Math]::Round($apiMs, 3) } else { $null }
        postgres_transaction_ms             = if ($null -ne $transactionMs) { [Math]::Round($transactionMs, 3) } else { $null }
        postgres_commit_ms                  = if ($null -ne $commitMs) { [Math]::Round($commitMs, 3) } else { $null }
        transport_protocol_overhead_ms      = if ($null -ne $overheadMs) { [Math]::Round($overheadMs, 3) } else { $null }
        rows_per_second                     = if ($roundTripMs -gt 0) {
            [Math]::Round($BatchSize / ($roundTripMs / 1000.0), 1)
        }
        else {
            0.0
        }
        valid                               = $valid
    }
}

function New-MetricSummary {
    param(
        [Parameter(Mandatory = $true)][string]$Metric,
        [Parameter(Mandatory = $true)][string]$Property,
        [Parameter(Mandatory = $true)][object[]]$Rows
    )

    $values = [double[]]@(
        $Rows |
            ForEach-Object { $_.PSObject.Properties[$Property].Value } |
            Where-Object { $null -ne $_ } |
            ForEach-Object { [double]$_ }
    )
    if ($values.Count -eq 0) { return }

    return [pscustomobject]@{
        metric    = $Metric
        samples   = $values.Count
        mean_ms   = [Math]::Round(($values | Measure-Object -Average).Average, 3)
        p50_ms    = [Math]::Round((Get-Percentile -Values $values -Percentile 0.50), 3)
        p95_ms    = [Math]::Round((Get-Percentile -Values $values -Percentile 0.95), 3)
        min_ms    = [Math]::Round(($values | Measure-Object -Minimum).Minimum, 3)
        max_ms    = [Math]::Round(($values | Measure-Object -Maximum).Maximum, 3)
    }
}

if ([string]::IsNullOrWhiteSpace($Token)) {
    $secureToken = Read-Host "Token Bearer de albion-market-api" -AsSecureString
    $Token = Get-PlainTextFromSecureString -SecureString $secureToken
}
if ([string]::IsNullOrWhiteSpace($Token)) {
    throw "El token Bearer es obligatorio."
}

$BaseUrl = $BaseUrl.TrimEnd("/")
$headers = @{ Authorization = "Bearer $Token" }
$requestIds = New-Object System.Collections.Generic.List[string]
$results = New-Object System.Collections.Generic.List[object]
$runStamp = (Get-Date).ToUniversalTime().ToString("yyyyMMddTHHmmssfffZ")
$runPrefix = "BENCHPHASE-$runStamp-"
New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null

$health = Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/healthz" -Method Get
if ([int]$health.StatusCode -ne 200) {
    throw "El health check respondio HTTP $($health.StatusCode)."
}

Write-Host "Calentamiento: $WarmupRounds ronda(s), excluidas de estadisticas..."
for ($round = 1; $round -le $WarmupRounds; $round++) {
    $null = Invoke-PhaseMeasurement -Round (-$round) -RunPrefix $runPrefix
}

Write-Host "Midiendo $Rounds ronda(s) de $BatchSize entradas..."
for ($round = 1; $round -le $Rounds; $round++) {
    $row = Invoke-PhaseMeasurement -Round $round -RunPrefix $runPrefix
    $results.Add($row)
    Write-Host (
        "  ronda {0}: total={1:N3} ms | api={2:N3} | tx={3:N3} | commit={4:N3} | overhead={5:N3}" -f
        $round,
        $row.round_trip_total_ms,
        $row.api_processing_ms,
        $row.postgres_transaction_ms,
        $row.postgres_commit_ms,
        $row.transport_protocol_overhead_ms
    )
}

$rows = @($results.ToArray())
$summary = @(
    New-MetricSummary -Metric "round_trip_total" -Property "round_trip_total_ms" -Rows $rows
    New-MetricSummary -Metric "api_processing" -Property "api_processing_ms" -Rows $rows
    New-MetricSummary -Metric "postgres_transaction" -Property "postgres_transaction_ms" -Rows $rows
    New-MetricSummary -Metric "postgres_commit" -Property "postgres_commit_ms" -Rows $rows
    New-MetricSummary -Metric "transport_protocol_overhead" -Property "transport_protocol_overhead_ms" -Rows $rows
)

Write-Host ""
$rows | Format-Table round, http, round_trip_total_ms, api_processing_ms, postgres_transaction_ms, postgres_commit_ms, transport_protocol_overhead_ms, valid -AutoSize
Write-Host ""
$summary | Format-Table metric, samples, mean_ms, p50_ms, p95_ms, min_ms, max_ms -AutoSize

$detailPath = Join-Path $OutputDirectory "benchmark-ingest-phases-$runStamp.csv"
$summaryPath = Join-Path $OutputDirectory "benchmark-ingest-phases-$runStamp-summary.csv"
$cleanupPath = Join-Path $OutputDirectory "benchmark-ingest-phases-$runStamp-cleanup.sql"
$rows | Export-Csv -Path $detailPath -NoTypeInformation -Encoding UTF8
$summary | Export-Csv -Path $summaryPath -NoTypeInformation -Encoding UTF8

$uniqueIds = @($requestIds | Sort-Object -Unique)
$sqlIds = ($uniqueIds | ForEach-Object { "    '$($_)'" }) -join ",`r`n"
$cleanupSql = @"
BEGIN;

DELETE FROM market_ingest_raw
WHERE request_id IN (
$sqlIds
);

DELETE FROM current_market_prices
WHERE item_key LIKE '$runPrefix%';

DELETE FROM market_ingest_requests
WHERE request_id IN (
$sqlIds
);

COMMIT;
"@
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)
[System.IO.File]::WriteAllText(
    [System.IO.Path]::GetFullPath($cleanupPath),
    $cleanupSql,
    $utf8NoBom
)

Write-Host ""
Write-Host "Detalle:  $detailPath"
Write-Host "Resumen:  $summaryPath"
Write-Host "Limpieza: $cleanupPath"

if ($rows.valid -contains $false) {
    Write-Warning "Una o mas rondas no incluyeron timings validos o no coincidieron con la respuesta esperada."
    exit 2
}

Write-Host "Benchmark segmentado completado correctamente." -ForegroundColor Green
