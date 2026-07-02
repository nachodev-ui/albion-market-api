# Paso 4: observabilidad de `albion-market-api`

Esta versión agrega observabilidad local al proceso de ingesta sin cambiar el contrato exitoso de `POST /api/v1/ingest/prices`.

## Logs de ingesta

Cada solicitud al handler genera exactamente una línea al terminar. Los campos aparecen siempre en el mismo orden:

```text
request_id server entries accepted current_rows_touched duplicate status duration_ms
```

Los errores agregan al final:

```text
error_kind error
```

Ejemplos:

```text
2026-06-25T23:00:00Z [OK   ] ingest.completed request_id="..." server="west" entries=500 accepted=500 current_rows_touched=500 duplicate=false status=202 duration_ms=77.73
2026-06-25T23:00:01Z [DUP  ] ingest.duplicate request_id="..." server="west" entries=500 accepted=500 current_rows_touched=0 duplicate=true status=200 duration_ms=48.529
2026-06-25T23:00:02Z [WARN ] ingest.rejected request_id="..." server="west" entries=500 accepted=0 current_rows_touched=0 duplicate=false status=409 duration_ms=2.14 error_kind="request_payload_conflict" error="request_id was already used with a different payload"
2026-06-25T23:00:03Z [ERROR] ingest.failed request_id="..." server="west" entries=500 accepted=0 current_rows_touched=0 duplicate=false status=500 duration_ms=15.62 error_kind="internal_error" error="..."
```

Etiquetas visuales:

- `OK`: solicitud nueva completada, verde.
- `DUP`: solicitud idempotente ya procesada, cian.
- `WARN`: rechazo esperado del cliente, amarillo.
- `ERROR`: fallo interno, rojo.
- `INFO`: inicio, apagado y eventos generales, cian.

Los colores se activan automáticamente solo cuando la salida es una terminal interactiva. En Windows, el proceso habilita el procesamiento de secuencias ANSI en la consola antes de escribirlas; si la consola no lo admite, `auto` desactiva el color para evitar mostrar códigos como `←[32m`. Se controlan con:

```env
LOG_COLOR=auto
```

Valores permitidos:

- `auto`: comportamiento recomendado.
- `always`: fuerza secuencias ANSI.
- `never`: salida sin color, útil para archivos o recolectores de logs.

La variable estándar `NO_COLOR` también desactiva colores cuando `LOG_COLOR=auto`.

## Respuestas de error

Los errores internos se registran completos en la consola, pero la respuesta HTTP solo devuelve `internal server error`. Esto evita exponer detalles de PostgreSQL o de infraestructura al cliente.

Errores distinguidos:

- `method_not_allowed`
- `unauthorized`
- `invalid_content_encoding`
- `unsupported_content_encoding`
- `payload_too_large`
- `invalid_json`
- `validation_error`
- `request_already_processing`
- `request_payload_conflict`
- `internal_error`

## Endpoint de estado

```http
GET /api/v1/status
```

Ejemplo en PowerShell:

```powershell
Invoke-RestMethod http://127.0.0.1:8080/api/v1/status |
    ConvertTo-Json -Depth 10
```

Respuesta resumida:

```json
{
  "status": "ok",
  "service": "albion-market-api",
  "environment": "development",
  "started_at": "2026-06-25T23:00:00Z",
  "now": "2026-06-25T23:10:00Z",
  "uptime_seconds": 600,
  "database": {
    "status": "ok",
    "ping_latency_ms": 0.85,
    "pool": {
      "max_connections": 10,
      "total_connections": 2,
      "acquired_connections": 1,
      "idle_connections": 1,
      "constructing_connections": 0,
      "acquire_count": 150,
      "empty_acquire_count": 1,
      "canceled_acquire_count": 0,
      "new_connections_count": 2,
      "acquire_duration_ms": 18.42
    }
  },
  "ingest": {
    "requests_total": 30,
    "in_flight": 0,
    "succeeded_total": 28,
    "duplicates_total": 2,
    "errors_total": 2,
    "accepted_entries_total": 13000,
    "current_rows_touched_total": 4200,
    "average_duration_ms": 74.2,
    "last_duration_ms": 48.5,
    "max_duration_ms": 280.3,
    "last_request_at": "2026-06-25T23:09:59Z",
    "last_success_at": "2026-06-25T23:09:59Z",
    "last_error_at": "2026-06-25T23:08:15Z",
    "last_error_kind": "request_payload_conflict"
  }
}
```

El endpoint responde:

- HTTP `200` y `status: ok` cuando PostgreSQL responde.
- HTTP `503` y `status: degraded` cuando PostgreSQL no está disponible.

No expone la cadena de conexión, tokens, contraseñas ni el texto interno del error de base de datos. Los eventos de ingesta incluyen únicamente `auth_key_id`, que identifica la credencial activa sin revelar su valor.

## Significado de los contadores

- `requests_total`: solicitudes que entraron al handler, incluso rechazos.
- `in_flight`: solicitudes actualmente en procesamiento.
- `succeeded_total`: respuestas HTTP 2xx, incluidos duplicados.
- `duplicates_total`: solicitudes idempotentes ya completadas.
- `errors_total`: respuestas no 2xx.
- `accepted_entries_total`: entradas de solicitudes nuevas procesadas; no suma duplicados.
- `current_rows_touched_total`: filas realmente insertadas o actualizadas en la tabla caliente.

Los contadores viven en memoria y vuelven a cero cuando se reinicia el proceso. No sustituyen una plataforma persistente de métricas; entregan visibilidad inmediata y barata para esta etapa.

## Validación

```powershell
gofmt -w .
go test ./...
go vet ./...
go build ./cmd/api
```

El CI ejecuta además los tests con el detector de carreras para validar los contadores concurrentes.

## Observabilidad del historial central

La ingesta histórica usa contadores separados para evitar que un flujo sano de
precios actuales oculte fallos del historial.

Eventos:

```text
ingest.history_completed
ingest.history_duplicate
ingest.history_rejected
ingest.history_failed
```

Orden de campos:

```text
request_id server entries buckets accepted_entries accepted_buckets history_rows_touched duplicate status duration_ms
```

`GET /api/v1/status` agrega:

```json
{
  "history_ingest": {
    "requests_total": 12,
    "in_flight": 0,
    "succeeded_total": 11,
    "duplicates_total": 1,
    "errors_total": 1,
    "accepted_entries_total": 120,
    "accepted_buckets_total": 8160,
    "history_rows_touched_total": 7900,
    "average_duration_ms": 82.5,
    "last_duration_ms": 71.3,
    "max_duration_ms": 240.1,
    "last_request_at": "2026-06-26T21:00:00Z",
    "last_success_at": "2026-06-26T21:00:00Z",
    "last_error_at": "2026-06-26T20:30:00Z",
    "last_error_kind": "request_payload_conflict"
  }
}
```

Los duplicados incrementan `duplicates_total`, pero no vuelven a sumar
entradas, buckets ni filas tocadas.
