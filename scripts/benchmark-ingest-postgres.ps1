[CmdletBinding()]
param(
    [string]$BaseUrl = "http://127.0.0.1:8080",
    [ValidateSet("west", "east", "europe")]
    [string]$Server = "west",
    [ValidateRange(1, 32767)]
    [int]$LocationId = 4002,
    [ValidateRange(1, 5000)]
    [int]$BatchSize = 500,
    [ValidateRange(1, 50)]
    [int]$Rounds = 5,
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

    if ($Values.Count -eq 0) { return 0.0 }
    $sorted = @($Values | Sort-Object)
    $index = [Math]::Ceiling($Percentile * $sorted.Count) - 1
    if ($index -lt 0) { $index = 0 }
    if ($index -ge $sorted.Count) { $index = $sorted.Count - 1 }
    return [double]$sorted[$index]
}

function New-BenchmarkEntries {
    param(
        [Parameter(Mandatory = $true)][string]$ItemPrefix,
        [Parameter(Mandatory = $true)][datetime]$ObservedAt,
        [Parameter(Mandatory = $true)][int]$Count,
        [Parameter(Mandatory = $true)][long]$PriceOffset
    )

    $timestamp = $ObservedAt.ToUniversalTime().ToString("o")
    $entries = New-Object System.Collections.Generic.List[object]

    for ($i = 0; $i -lt $Count; $i++) {
        $sell = [long](100000 + $PriceOffset + $i)
        $buy = [long](90000 + $PriceOffset + $i)

        $entries.Add([ordered]@{
            observed_at       = $timestamp
            location_id       = $LocationId
            item_key          = ("{0}{1:D4}" -f $ItemPrefix, $i)
            quality           = 1
            sell_price_min    = $sell
            sell_price_min_at = $timestamp
            buy_price_max     = $buy
            buy_price_max_at  = $timestamp
        })
    }

    return $entries.ToArray()
}

function New-IngestBody {
    param(
        [Parameter(Mandatory = $true)][string]$RequestId,
        [Parameter(Mandatory = $true)][object[]]$Entries
    )

    return ([ordered]@{
        request_id = $RequestId
        server     = $Server
        entries    = $Entries
    } | ConvertTo-Json -Depth 10 -Compress)
}

function Invoke-IngestMeasurement {
    param(
        [Parameter(Mandatory = $true)][string]$Phase,
        [Parameter(Mandatory = $true)][int]$Round,
        [Parameter(Mandatory = $true)][string]$RequestId,
        [Parameter(Mandatory = $true)][string]$Body,
        [Parameter(Mandatory = $true)][bool]$ExpectedDuplicate,
        [Parameter(Mandatory = $true)][int64]$ExpectedTouched
    )

    $bodyBytes = [Text.Encoding]::UTF8.GetByteCount($Body)
    $stopwatch = [Diagnostics.Stopwatch]::StartNew()

    try {
        $response = Invoke-WebRequest `
            -UseBasicParsing `
            -Method Post `
            -Uri "$BaseUrl/api/v1/ingest/prices" `
            -Headers $headers `
            -ContentType "application/json" `
            -Body $Body
    }
    catch {
        $stopwatch.Stop()
        $details = $_.Exception.Message
        if ($_.Exception.Response) {
            try {
                $stream = $_.Exception.Response.GetResponseStream()
                if ($stream) {
                    $reader = New-Object IO.StreamReader($stream)
                    $details = $reader.ReadToEnd()
                    $reader.Dispose()
                }
            }
            catch {
                # Se conserva el mensaje original.
            }
        }
        throw "Fallo en fase '${Phase}', ronda ${Round}: ${details}"
    }

    $stopwatch.Stop()
    $json = $response.Content | ConvertFrom-Json
    $elapsedMs = $stopwatch.Elapsed.TotalMilliseconds
    $rowsPerSecond = if ($elapsedMs -gt 0) { $BatchSize / ($elapsedMs / 1000.0) } else { 0.0 }

    $valid = (
        [int]$response.StatusCode -in @(200, 202) -and
        [int]$json.accepted -eq $BatchSize -and
        [bool]$json.duplicate -eq $ExpectedDuplicate -and
        [int64]$json.current_rows_touched -eq $ExpectedTouched
    )

    $script:requestIds.Add($RequestId)

    return [pscustomobject]@{
        phase                = $Phase
        round                = $Round
        http                 = [int]$response.StatusCode
        request_id           = [string]$json.request_id
        accepted             = [int]$json.accepted
        current_rows_touched = [int64]$json.current_rows_touched
        duplicate            = [bool]$json.duplicate
        elapsed_ms           = [Math]::Round($elapsedMs, 3)
        rows_per_second      = [Math]::Round($rowsPerSecond, 1)
        body_bytes           = $bodyBytes
        valid                = $valid
    }
}

function Show-PhaseSummary {
    param(
        [Parameter(Mandatory = $true)][string]$Phase,
        [Parameter(Mandatory = $true)][object[]]$Rows
    )

    $phaseRows = @($Rows | Where-Object { $_.phase -eq $Phase })
    if ($phaseRows.Count -eq 0) { return }

    $values = [double[]]@($phaseRows | ForEach-Object { [double]$_.elapsed_ms })
    $mean = ($values | Measure-Object -Average).Average
    $min = ($values | Measure-Object -Minimum).Minimum
    $max = ($values | Measure-Object -Maximum).Maximum
    $median = Get-Percentile -Values $values -Percentile 0.50
    $p95 = Get-Percentile -Values $values -Percentile 0.95
    $throughput = if ($mean -gt 0) { $BatchSize / ($mean / 1000.0) } else { 0.0 }

    [pscustomobject]@{
        phase           = $Phase
        samples         = $phaseRows.Count
        mean_ms         = [Math]::Round($mean, 3)
        median_ms       = [Math]::Round($median, 3)
        p95_ms          = [Math]::Round($p95, 3)
        min_ms          = [Math]::Round($min, 3)
        max_ms          = [Math]::Round($max, 3)
        mean_rows_sec   = [Math]::Round($throughput, 1)
        all_valid       = -not ($phaseRows.valid -contains $false)
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
$runPrefix = "BENCHPG-$runStamp-"
$baseTime = (Get-Date).ToUniversalTime().AddMinutes(-10)

New-Item -ItemType Directory -Path $OutputDirectory -Force | Out-Null

Write-Host ""
Write-Host "Benchmark real de ingest contra PostgreSQL" -ForegroundColor Cyan
Write-Host "API:        $BaseUrl"
Write-Host "Servidor:   $Server"
Write-Host "Ubicacion:  $LocationId"
Write-Host "Batch:      $BatchSize entradas"
Write-Host "Rondas:     $Rounds por fase"
Write-Host "Prefijo:    $runPrefix"
Write-Host ""

Write-Host "Comprobando /healthz..."
$health = Invoke-WebRequest -UseBasicParsing -Uri "$BaseUrl/healthz" -Method Get
if ([int]$health.StatusCode -ne 200) {
    throw "El health check respondio HTTP $($health.StatusCode)."
}

# Calentamiento completo, excluido de las estadisticas.
Write-Host "Calentamiento: 1 batch real de $BatchSize..."
$warmEntries = New-BenchmarkEntries `
    -ItemPrefix "${runPrefix}W-" `
    -ObservedAt $baseTime `
    -Count $BatchSize `
    -PriceOffset 1
$warmRequestId = [guid]::NewGuid().ToString()
$warmBody = New-IngestBody -RequestId $warmRequestId -Entries $warmEntries
$warm = Invoke-IngestMeasurement `
    -Phase "warmup" `
    -Round 1 `
    -RequestId $warmRequestId `
    -Body $warmBody `
    -ExpectedDuplicate $false `
    -ExpectedTouched $BatchSize
Write-Host ("Calentamiento: {0:N3} ms, {1:N1} filas/s" -f $warm.elapsed_ms, $warm.rows_per_second)

Write-Host ""
Write-Host "Fase 1/4: INSERT de 500 claves nuevas por ronda..." -ForegroundColor Yellow
$targetEntries = $null
for ($round = 1; $round -le $Rounds; $round++) {
    $entries = New-BenchmarkEntries `
        -ItemPrefix ("{0}I{1:D2}-" -f $runPrefix, $round) `
        -ObservedAt $baseTime.AddSeconds($round) `
        -Count $BatchSize `
        -PriceOffset ($round * 1000)
    $requestId = [guid]::NewGuid().ToString()
    $body = New-IngestBody -RequestId $requestId -Entries $entries
    $row = Invoke-IngestMeasurement `
        -Phase "insert-new" `
        -Round $round `
        -RequestId $requestId `
        -Body $body `
        -ExpectedDuplicate $false `
        -ExpectedTouched $BatchSize
    $results.Add($row)
    Write-Host ("  ronda {0}: {1:N3} ms | touched={2} | {3:N1} filas/s" -f $round, $row.elapsed_ms, $row.current_rows_touched, $row.rows_per_second)

    if ($round -eq 1) {
        $targetEntries = $entries
    }
}

Write-Host ""
Write-Host "Fase 2/4: UPDATE de las mismas 500 claves con datos mas nuevos..." -ForegroundColor Yellow
$lastUpdatedEntries = $null
for ($round = 1; $round -le $Rounds; $round++) {
    $timestamp = $baseTime.AddMinutes(1).AddSeconds($round)
    $entries = New-BenchmarkEntries `
        -ItemPrefix ("{0}I01-" -f $runPrefix) `
        -ObservedAt $timestamp `
        -Count $BatchSize `
        -PriceOffset (100000 + ($round * 1000))
    $requestId = [guid]::NewGuid().ToString()
    $body = New-IngestBody -RequestId $requestId -Entries $entries
    $row = Invoke-IngestMeasurement `
        -Phase "update-newer" `
        -Round $round `
        -RequestId $requestId `
        -Body $body `
        -ExpectedDuplicate $false `
        -ExpectedTouched $BatchSize
    $results.Add($row)
    Write-Host ("  ronda {0}: {1:N3} ms | touched={2} | {3:N1} filas/s" -f $round, $row.elapsed_ms, $row.current_rows_touched, $row.rows_per_second)
    $lastUpdatedEntries = $entries
}

Write-Host ""
Write-Host "Fase 3/4: request nuevo con las mismas 500 observaciones..." -ForegroundColor Yellow
$lastNoChangeRequestId = $null
$lastNoChangeBody = $null
for ($round = 1; $round -le $Rounds; $round++) {
    $requestId = [guid]::NewGuid().ToString()
    $body = New-IngestBody -RequestId $requestId -Entries $lastUpdatedEntries
    $row = Invoke-IngestMeasurement `
        -Phase "no-change" `
        -Round $round `
        -RequestId $requestId `
        -Body $body `
        -ExpectedDuplicate $false `
        -ExpectedTouched 0
    $results.Add($row)
    Write-Host ("  ronda {0}: {1:N3} ms | touched={2} | {3:N1} filas/s" -f $round, $row.elapsed_ms, $row.current_rows_touched, $row.rows_per_second)
    $lastNoChangeRequestId = $requestId
    $lastNoChangeBody = $body
}

Write-Host ""
Write-Host "Fase 4/4: mismo request_id y mismo payload, ruta idempotente..." -ForegroundColor Yellow
for ($round = 1; $round -le $Rounds; $round++) {
    $row = Invoke-IngestMeasurement `
        -Phase "duplicate" `
        -Round $round `
        -RequestId $lastNoChangeRequestId `
        -Body $lastNoChangeBody `
        -ExpectedDuplicate $true `
        -ExpectedTouched 0
    $results.Add($row)
    Write-Host ("  ronda {0}: {1:N3} ms | duplicate={2} | {3:N1} filas/s" -f $round, $row.elapsed_ms, $row.duplicate, $row.rows_per_second)
}

$allRows = @($results.ToArray())
$summary = @(
    Show-PhaseSummary -Phase "insert-new" -Rows $allRows
    Show-PhaseSummary -Phase "update-newer" -Rows $allRows
    Show-PhaseSummary -Phase "no-change" -Rows $allRows
    Show-PhaseSummary -Phase "duplicate" -Rows $allRows
)

Write-Host ""
Write-Host "Resultados por solicitud" -ForegroundColor Cyan
$allRows | Format-Table phase, round, http, accepted, current_rows_touched, duplicate, elapsed_ms, rows_per_second, valid -AutoSize

Write-Host ""
Write-Host "Resumen" -ForegroundColor Cyan
$summary | Format-Table phase, samples, mean_ms, median_ms, p95_ms, min_ms, max_ms, mean_rows_sec, all_valid -AutoSize

$csvPath = Join-Path $OutputDirectory "benchmark-ingest-postgres-$runStamp.csv"
$summaryPath = Join-Path $OutputDirectory "benchmark-ingest-postgres-$runStamp-summary.csv"
$cleanupPath = Join-Path $OutputDirectory "benchmark-ingest-postgres-$runStamp-cleanup.sql"

$allRows | Export-Csv -Path $csvPath -NoTypeInformation -Encoding UTF8
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
$cleanupFullPath = [System.IO.Path]::GetFullPath($cleanupPath)
[System.IO.File]::WriteAllText($cleanupFullPath, $cleanupSql, $utf8NoBom)

Write-Host ""
Write-Host "Archivos generados:" -ForegroundColor Green
Write-Host "  Detalle:  $csvPath"
Write-Host "  Resumen:  $summaryPath"
Write-Host "  Limpieza: $cleanupPath"

if ($allRows.valid -contains $false) {
    Write-Warning "Una o mas respuestas no coincidieron con el comportamiento esperado. Revisa la columna valid."
    exit 2
}

Write-Host ""
Write-Host "Benchmark completado: todas las respuestas fueron coherentes." -ForegroundColor Green
