# Historial de mercado centralizado

Esta versión agrega almacenamiento y lectura de historial a
`albion-market-api`. El receiver local podrá enviar sus capturas normalizadas a
la API central y el frontend podrá consultarlas mediante claves públicas
`marketKey`, sin conocer los `location_id` internos de Albion.

## 1. Aplicar la migración

Antes de iniciar la nueva versión de la API, ejecutar:

```powershell
psql $env:DATABASE_URL `
  -v ON_ERROR_STOP=1 `
  -f .\migrations\000004_market_history.sql
```

La migración es aditiva y crea tres tablas:

- `market_history_ingest_requests`: registro de idempotencia por `request_id`;
- `market_history_ingest_raw`: auditoría durable y append-only de cada bucket;
- `market_history_buckets`: modelo caliente de lectura, con un único registro
  por servidor, mercado, objeto, calidad y timestamp del bucket.

La clave primaria de la tabla caliente es:

```text
server + location_id + item_key + quality + bucket_at
```

Una captura más nueva puede corregir un bucket existente. Una captura más
antigua nunca reemplaza una observación más reciente.

## 2. Ingesta autenticada

```http
POST /api/v1/ingest/history
Authorization: Bearer <INGEST_BEARER_TOKEN>
Content-Type: application/json
```

```json
{
  "request_id": "00112233-4455-6677-8899-aabbccddeeff",
  "server": "west",
  "entries": [
    {
      "observed_at": "2026-06-26T20:00:00Z",
      "location_id": 4002,
      "item_key": "T5_LEATHER_LEVEL4@4",
      "quality": 1,
      "history": [
        {
          "timestamp": "2026-06-25T00:00:00Z",
          "item_count": 42,
          "average_unit_price": 18750
        }
      ]
    }
  ]
}
```

Respuesta para un batch nuevo:

```json
{
  "request_id": "00112233-4455-6677-8899-aabbccddeeff",
  "accepted_entries": 1,
  "accepted_buckets": 1,
  "history_rows_touched": 1,
  "duplicate": false
}
```

Códigos relevantes:

- `202`: batch nuevo procesado;
- `200`: el mismo `request_id` y payload ya había sido procesado;
- `400`: contrato inválido;
- `401`: token ausente o incorrecto;
- `409`: request todavía en procesamiento o `request_id` reutilizado con otro
  payload;
- `413`: cuerpo sobre `MAX_INGEST_BODY_BYTES`;
- `500`: fallo interno, sin filtrar detalles de PostgreSQL al cliente.

Reglas actuales:

- `request_id` debe ser un UUID;
- hasta 2000 capturas por request;
- hasta 100000 buckets normalizados por request, sujeto también al límite de
  bytes configurado;
- calidad entre 1 y 5;
- `item_count` no puede ser negativo;
- `average_unit_price` negativo es inválido;
- `average_unit_price` igual a cero se normaliza a `null`, porque representa
  ausencia de un precio promedio significativo.

La ingesta conserva `location_id` porque es un contrato interno entre receiver
y API. Las rutas públicas de lectura nunca lo exponen.

## 3. Lectura batch pública

```http
POST /api/v1/history/query
Content-Type: application/json
```

```json
{
  "server": "west",
  "marketKeys": ["martlock", "fort_sterling"],
  "entries": [
    {
      "itemIdentifier": "T5_LEATHER_LEVEL4@4",
      "quality": 1
    }
  ],
  "rangeStart": "2026-06-01",
  "rangeEnd": "2026-06-25"
}
```

`rangeStart` y `rangeEnd` son fechas UTC inclusivas con formato `YYYY-MM-DD`.
La consulta admite hasta 366 días.

Respuesta:

```json
{
  "requestedAt": "2026-06-26T21:00:00Z",
  "rangeStart": "2026-06-01",
  "rangeEnd": "2026-06-25",
  "count": 1,
  "bucketCount": 1,
  "data": [
    {
      "server": "west",
      "marketKey": "fort_sterling",
      "itemIdentifier": "T5_LEATHER_LEVEL4@4",
      "quality": 1,
      "history": [
        {
          "timestamp": "2026-06-25T00:00:00Z",
          "itemCount": 42,
          "averageUnitPrice": 18750
        }
      ]
    }
  ]
}
```

Las combinaciones sin datos se omiten de `data`. El arreglo nunca es `null`.
Los mercados e ítems repetidos se deduplican antes de consultar PostgreSQL.

Límites:

- hasta 32 mercados;
- hasta 2000 pares objeto/calidad;
- hasta 366 días por consulta;
- servidores `west`, `east` y `europe`;
- solamente mercados habilitados en `GET /api/v1/markets`.

## 4. GET compatible con el receiver local

El frontend actual puede usar una consulta simple con la misma forma del
receiver:

```http
GET /api/v1/history?server=west&marketKey=fort_sterling&itemId=T5_LEATHER_LEVEL4@4&quality=1&period=4-weeks&limit=1
```

Periodos admitidos:

- `7-days`;
- `4-weeks`.

El periodo se calcula sobre días UTC completados y termina en el día anterior.
También puede enviarse un rango explícito:

```http
GET /api/v1/history?server=west&marketKey=fort_sterling&itemId=T5_LEATHER_LEVEL4@4&quality=1&rangeStart=2026-06-01&rangeEnd=2026-06-25
```

Para múltiples mercados, objetos o calidades debe preferirse
`POST /api/v1/history/query`.

## 5. Pruebas rápidas en PowerShell

### Ingesta

```powershell
$historyBody = @{
  request_id = [guid]::NewGuid().ToString()
  server = "west"
  entries = @(
    @{
      observed_at = (Get-Date).ToUniversalTime().ToString("o")
      location_id = 4002
      item_key = "T5_LEATHER_LEVEL4@4"
      quality = 1
      history = @(
        @{
          timestamp = "2026-06-25T00:00:00Z"
          item_count = 42
          average_unit_price = 18750
        }
      )
    }
  )
} | ConvertTo-Json -Depth 8

Invoke-RestMethod `
  -Method Post `
  -Uri "http://127.0.0.1:8080/api/v1/ingest/history" `
  -Headers @{ Authorization = "Bearer $env:INGEST_BEARER_TOKEN" } `
  -ContentType "application/json" `
  -Body $historyBody
```

### Lectura

```powershell
$queryBody = @{
  server = "west"
  marketKeys = @("fort_sterling")
  entries = @(
    @{ itemIdentifier = "T5_LEATHER_LEVEL4@4"; quality = 1 }
  )
  rangeStart = "2026-06-01"
  rangeEnd = "2026-06-25"
} | ConvertTo-Json -Depth 6

Invoke-RestMethod `
  -Method Post `
  -Uri "http://127.0.0.1:8080/api/v1/history/query" `
  -ContentType "application/json" `
  -Body $queryBody |
    ConvertTo-Json -Depth 10
```

## 6. Estado y logs

`GET /api/v1/status` agrega `history_ingest`, separado de `ingest`, con:

- requests, éxitos, duplicados y errores;
- entradas y buckets aceptados;
- filas históricas insertadas o corregidas;
- latencia media, última y máxima;
- fecha y tipo del último error.

Los eventos de log son:

```text
ingest.history_completed
ingest.history_duplicate
ingest.history_rejected
ingest.history_failed
```
