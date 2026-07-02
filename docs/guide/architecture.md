# Arquitectura

## Contexto

```text
┌────────────────────┐
│ Albion Data Client │
└─────────┬──────────┘
          │ mensajes locales
          ▼
┌────────────────────┐
│ receiver/forwarder │
└─────────┬──────────┘
          │ HTTP + Bearer + request_id
          ▼
┌───────────────────────────────┐
│       albion-market-api       │
│                               │
│ handlers → services → repos   │
│        │              │       │
│ métricas/logs      pgxpool    │
└─────────┬──────────────┬──────┘
          │              │
          ▼              ▼
  consumidores      PostgreSQL
```

## Capas

### HTTP y seguridad

`internal/server` registra rutas y aplica CORS, cabeceras de seguridad y limitación por IP. Los handlers controlan método, contenido, tamaño, JSON estricto y traducción de errores.

### Servicios

`internal/service` normaliza contratos, valida límites, resuelve mercados públicos y mantiene las reglas de actualización temporal.

### Persistencia

`internal/repository` utiliza `pgx` y `CopyFrom` para la auditoría raw. Los upserts por conjuntos actualizan las tablas calientes solo cuando corresponde.

### Observabilidad

`internal/observability` concentra logs, métricas en memoria y estado del pool PostgreSQL. `/api/v1/status` expone una vista operativa sin secretos.

## Modelos PostgreSQL

| Tipo | Tablas | Función |
|---|---|---|
| Auditoría raw | `market_ingest_raw`, `market_history_ingest_raw` | Evidencia de lo recibido |
| Idempotencia | `market_ingest_requests`, `market_history_ingest_requests` | Control por `request_id` |
| Lectura caliente | `current_market_prices`, `market_history_buckets` | Consultas públicas eficientes |

Consulta la [sección PostgreSQL](/database/) para retención, índices y recuperación.
