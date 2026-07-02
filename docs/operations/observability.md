# Observabilidad

La API expone logs estructurados, correlación HTTP, liveness, readiness y métricas
Prometheus. Prometheus, Grafana y Alertmanager se incorporan como servicios
externos en el siguiente bloque de la etapa 6.

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
contenedor. El `HEALTHCHECK` de la imagen utiliza esta sonda.

### Readiness

```http
GET /readyz
```

La comprobación usa un timeout acotado y exige, en este orden:

1. que el pool PostgreSQL entregue una conexión;
2. que esa conexión responda a `Ping`;
3. que existan las relaciones críticas de lectura, ingesta y auditoría;
4. que `public.app_schema_state` indique como mínimo la versión `6`.

Resultados:

- `200 {"status":"ok"}` cuando la API puede recibir tráfico;
- `503` con un mensaje genérico cuando falla el pool, PostgreSQL o el esquema;
- nunca expone host, DSN, relaciones faltantes ni texto interno del error.

La migración `000006_observability_readiness.sql` mantiene el marcador de versión.
Una instancia con migraciones incompletas continúa viva, pero no lista.

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
albion_market_api_http_errors_total
albion_market_api_http_requests_in_flight
albion_market_api_http_request_duration_seconds
```

`http_requests_total` usa `method`, `route` y `status`;
`http_errors_total` usa `method`, `route` y la clase acotada `4xx` o `5xx`. Las
rutas desconocidas se agrupan como `unmatched` y los métodos no estándar como
`OTHER`; nunca se usa la URL ni el método arbitrario como etiqueta.

### Readiness

```text
albion_market_api_readiness_ready
albion_market_api_readiness_checks_total
albion_market_api_readiness_failures_total
albion_market_api_readiness_check_duration_seconds
albion_market_api_readiness_last_success_timestamp_seconds
albion_market_api_readiness_last_failure_timestamp_seconds
```

`result` solo admite `success` o `error`. `component` solo admite
`database_pool`, `database`, `schema` o `unknown`. Esto permite alertar por causa
sin convertir mensajes de error en labels. Cada scrape de `/metrics` ejecuta una
comprobación acotada, por lo que el gauge no depende de una consulta previa a
`/readyz`.

### Ingesta

```text
albion_market_api_ingest_batches_total
albion_market_api_ingest_errors_total
albion_market_api_ingest_requests_in_flight
albion_market_api_ingest_entries_received_total
albion_market_api_ingest_accepted_entries_total
albion_market_api_ingest_entries_stored_total
albion_market_api_ingest_entries_rejected_total
albion_market_api_ingest_entries_duplicate_total
albion_market_api_ingest_buckets_received_total
albion_market_api_ingest_accepted_buckets_total
albion_market_api_ingest_buckets_stored_total
albion_market_api_ingest_buckets_rejected_total
albion_market_api_ingest_buckets_duplicate_total
albion_market_api_ingest_rows_touched_total
albion_market_api_ingest_request_duration_seconds
albion_market_api_ingest_last_request_timestamp_seconds
albion_market_api_ingest_last_success_timestamp_seconds
albion_market_api_ingest_last_error_timestamp_seconds
```

La etiqueta `stream` distingue `prices` e `history`; `result` se limita a
`success`, `duplicate` y `error`. Los contadores de recibidas, almacenadas,
rechazadas y duplicadas permiten distinguir falta de tráfico, idempotencia y
fallos reales. En historial también se contabilizan buckets.

`albion_market_api_ingest_requests_total` y
`albion_market_api_ingest_observed_requests_total` se conservan por compatibilidad
con la primera versión del endpoint.

### PostgreSQL

```text
albion_market_api_database_ready
albion_market_api_database_pool_acquisition_duration_seconds
albion_market_api_database_ping_duration_seconds
albion_market_api_database_pool_utilization_ratio
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
albion_market_api_database_pool_acquire_duration_seconds_average
albion_market_api_database_operations_total
albion_market_api_database_errors_total
albion_market_api_database_operation_duration_seconds
albion_market_api_ingest_copy_duration_seconds
albion_market_api_ingest_upsert_duration_seconds
albion_market_api_database_transaction_duration_seconds
```

Las operaciones usan un conjunto fijo. Incluye consultas, ping, adquisición y
validación de readiness, CopyFrom, upserts y transacciones de ingesta. Las tres
últimas familias presentan las duraciones críticas con la etiqueta `stream`
limitada a `prices` o `history`.

`database_pool_acquisition_duration_seconds` mide la adquisición más reciente
realizada durante el scrape. El acumulado y el promedio del pool permiten detectar
contención sostenida; `database_pool_utilization_ratio` facilita alertar cerca del
límite configurado.

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

Los nombres de ruta, resultado, componente y operación pasan por conjuntos
acotados. Los smoke tests comprueban que `/metrics` no contiene las credenciales
generadas para PostgreSQL o ingesta.

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
separación liveness/readiness, migraciones incompletas, caída temporal de
PostgreSQL, recuperación y ausencia de secretos en la exposición.
