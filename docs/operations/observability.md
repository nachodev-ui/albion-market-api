# Observabilidad

La API expone una base de observabilidad interna antes de incorporar Prometheus,
Grafana y Alertmanager como servicios externos. Este bloque incluye logs
estructurados, correlación HTTP, liveness, readiness y métricas Prometheus.

## Logs estructurados

`LOG_FORMAT` controla el formato:

```dotenv
LOG_FORMAT=text
LOG_COLOR=auto
```

Valores admitidos:

- `text`: salida legible `key=value`, predeterminada fuera de producción;
- `json`: un objeto JSON por línea, predeterminado en `production`;
- `LOG_COLOR` solo aplica a `text`; JSON nunca contiene secuencias ANSI.

Cada petición HTTP genera un evento `http.request_completed` con:

```text
correlation_id method route status duration_ms response_bytes
```

El middleware acepta un `X-Request-ID` válido o genera uno criptográficamente
aleatorio y lo devuelve en la respuesta. Los IDs se registran en logs, pero nunca
se usan como etiquetas Prometheus.

Ejemplo JSON:

```json
{
  "timestamp": "2026-07-02T18:00:00Z",
  "level": "INFO",
  "event": "http.request_completed",
  "correlation_id": "8a83d4a0d4e247f3a838fbdffb8192e1",
  "method": "GET",
  "route": "/readyz",
  "status": 200,
  "duration_ms": 1.24,
  "response_bytes": 16
}
```

Los campos sensibles se redactan. Claves como `authorization`, `database_url`,
`password`, `secret`, `token` y sus sufijos equivalentes nunca imprimen el valor.
`auth_key_id` sigue siendo seguro porque identifica la credencial sin exponerla.

## Liveness y readiness

### Liveness

```http
GET /healthz
```

Solo confirma que el proceso HTTP está vivo. No consulta PostgreSQL, por lo que
una interrupción breve de la base de datos no provoca reinicios innecesarios del
contenedor.

### Readiness

```http
GET /readyz
```

Ejecuta un ping PostgreSQL con timeout acotado:

- `200 {"status":"ok"}` cuando la API está lista;
- `503` con un mensaje genérico cuando PostgreSQL no responde;
- nunca expone host, DSN ni texto interno del error.

`/healthz`, `/readyz`, `/metrics` y `OPTIONS` quedan fuera del rate limiter.

## Métricas Prometheus

```http
GET /metrics
```

PowerShell:

```powershell
(Invoke-WebRequest http://127.0.0.1:8080/metrics).Content
```

La exposición usa el formato de texto Prometheus `0.0.4` y `Cache-Control:
no-store`.

### HTTP

```text
albion_market_api_http_requests_total
albion_market_api_http_requests_in_flight
albion_market_api_http_request_duration_seconds
```

Etiquetas permitidas: `method`, `route` y `status`. Las rutas desconocidas se
agrupan como `unmatched` y los métodos no estándar como `OTHER`; nunca se usa
la URL ni el método arbitrario como etiqueta.

### Ingesta

```text
albion_market_api_ingest_requests_total
albion_market_api_ingest_requests_in_flight
albion_market_api_ingest_accepted_entries_total
albion_market_api_ingest_accepted_buckets_total
albion_market_api_ingest_rows_touched_total
albion_market_api_ingest_request_duration_seconds
albion_market_api_ingest_last_request_timestamp_seconds
albion_market_api_ingest_last_success_timestamp_seconds
albion_market_api_ingest_last_error_timestamp_seconds
```

La etiqueta `stream` distingue `prices` e `history`; `result` se limita a
`success`, `duplicate` y `error`.

### PostgreSQL

```text
albion_market_api_database_ready
albion_market_api_database_ping_duration_seconds
albion_market_api_database_pool_max_connections
albion_market_api_database_pool_total_connections
albion_market_api_database_pool_acquired_connections
albion_market_api_database_pool_idle_connections
albion_market_api_database_pool_constructing_connections
albion_market_api_database_pool_acquire_total
albion_market_api_database_pool_empty_acquire_total
albion_market_api_database_pool_canceled_acquire_total
albion_market_api_database_pool_new_connections_total
albion_market_api_database_pool_acquire_duration_seconds_total
albion_market_api_database_operations_total
albion_market_api_database_operation_duration_seconds
```

Las operaciones instrumentadas usan un conjunto fijo, por ejemplo
`ingest_prices`, `ingest_history`, `copy_raw_prices`, `copy_raw_history`,
`upsert_current_prices`, `upsert_market_history`, `query_current_prices`,
`query_market_history` y `ping`.

### Proceso y build

```text
albion_market_api_build_info
albion_market_api_process_start_time_seconds
albion_market_api_process_uptime_seconds
albion_market_api_go_goroutines
albion_market_api_go_memory_alloc_bytes
albion_market_api_go_memory_heap_inuse_bytes
albion_market_api_go_gc_cycles_total
```

La imagen inyecta `version` y `revision` durante el build mediante `-ldflags`.

## Política de cardinalidad y secretos

No se publican como labels:

```text
request_id
correlation_id
item_id
location_id
market_key
auth_key_id
token
mensaje de error
```

Los smoke tests comprueban que `/metrics` no contiene las credenciales generadas
para PostgreSQL o ingesta.

## Endpoint detallado de estado

```http
GET /api/v1/status
```

Conserva la vista JSON para diagnóstico humano: uptime, ping, estadísticas del
pool y contadores en memoria de ingesta. Responde `503` cuando PostgreSQL está
indisponible. No sustituye `/metrics`, que está diseñado para scraping.

## Validación

```powershell
gofmt -w .

go test ./...
go vet ./...

npm run contracts:check
npm run docs:check

.\scripts\test-container.ps1
.\scripts\test-deployment-compose.ps1
```

Los tests cubren concurrencia, redacción, formato JSON, etiquetas acotadas,
separación liveness/readiness y ausencia de secretos en la exposición.
