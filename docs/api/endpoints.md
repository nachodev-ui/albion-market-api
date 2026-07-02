# Referencia HTTP

Base local predeterminada: `http://127.0.0.1:8080`.

El contrato máquina legible y su auditoría inicial están en
[Contrato OpenAPI](./openapi.md). La fuente canónica es
`openapi/openapi.yaml`.

## Resumen

| Método | Ruta | Autenticación | Descripción |
|---|---|---|---|
| `GET` | `/healthz` | pública | Liveness del proceso |
| `GET` | `/readyz` | pública | Readiness del pool, PostgreSQL y esquema |
| `GET` | `/metrics` | pública | Métricas Prometheus |
| `GET` | `/api/v1/status` | pública | Estado y métricas en memoria |
| `GET` | `/api/v1/markets` | pública | Catálogo de mercados |
| `GET` | `/api/v1/prices` | pública | Consulta simple de precios |
| `POST` | `/api/v1/prices/query` | pública | Consulta batch de precios |
| `GET` | `/api/v1/history` | pública | Consulta simple de historial |
| `POST` | `/api/v1/history/query` | pública | Consulta batch de historial |
| `POST` | `/api/v1/ingest/prices` | Bearer | Ingesta de precios |
| `POST` | `/api/v1/ingest/history` | Bearer | Ingesta de historial |

## Liveness y readiness

```http
GET /healthz
GET /readyz
```

`/healthz` confirma que el proceso está vivo sin depender de PostgreSQL.
`/readyz` adquiere una conexión del pool, comprueba PostgreSQL y valida las relaciones y la versión mínima del esquema. Devuelve `503` cuando cualquiera de esas condiciones falla.
Ambas respuestas incluyen `Cache-Control: no-store`.

## Métricas

```http
GET /metrics
```

Expone métricas Prometheus de HTTP, readiness, ingesta, PostgreSQL, runtime Go y build con
etiquetas de cardinalidad acotada. Consulta [observabilidad](../operations/observability.md).

## Estado

```http
GET /api/v1/status
```

Incluye uptime, ping y pool PostgreSQL, además de métricas separadas para ingesta de precios e historial. Consulta [observabilidad](../operations/observability.md).

## Mercados

```http
GET /api/v1/markets?includeDisabled=false
```

`includeDisabled` debe ser `true` o `false`.

## Precios: consulta simple

```http
GET /api/v1/prices?server=west&marketKey=martlock&itemIds=T4_BAG,T5_MAIN_SWORD&quality=1
```

Parámetros requeridos: `server`, `marketKey`, `itemIds` y `quality` entre 1 y 5.

## Precios: consulta batch

```http
POST /api/v1/prices/query
Content-Type: application/json
```

```json
{
  "server": "west",
  "marketKeys": ["martlock", "fort_sterling"],
  "entries": [
    {"itemIdentifier": "T4_BAG", "quality": 1},
    {"itemIdentifier": "T5_MAIN_SWORD", "quality": 3}
  ]
}
```

Límites: hasta 32 mercados y 2000 entradas por consulta.

## Historial: consulta simple

```http
GET /api/v1/history?server=west&marketKey=martlock&itemId=T4_BAG&quality=1&period=4-weeks
```

`period` admite `7-days` o `4-weeks`. Como alternativa, usa juntos `rangeStart` y `rangeEnd` en formato `YYYY-MM-DD`. El intervalo máximo es de 366 días.

## Historial: consulta batch

```http
POST /api/v1/history/query
Content-Type: application/json
```

```json
{
  "server": "west",
  "marketKeys": ["martlock"],
  "entries": [
    {"itemIdentifier": "T4_BAG", "quality": 1}
  ],
  "rangeStart": "2026-06-01",
  "rangeEnd": "2026-06-30"
}
```

Límites: hasta 32 mercados, 2000 entradas y 366 días.

## Ingesta

Las rutas de ingesta requieren:

```http
Authorization: Bearer <token>
Content-Type: application/json
```

Cada payload usa un `request_id` UUID para idempotencia. Repetir el mismo ID y contenido devuelve el resultado existente; reutilizarlo con otro contenido se rechaza. Los contratos completos están en [integración del frontend](./frontend-consumption.md) e [historial centralizado](./market-history.md).

## Errores comunes

| Estado | Significado habitual |
|---:|---|
| `400` | JSON o contrato inválido |
| `401` | Credencial ausente o inválida |
| `409` | `request_id` en proceso o reutilizado con otro payload |
| `413` | Cuerpo demasiado grande |
| `415` | `Content-Type` no compatible |
| `426` | HTTPS requerido para ingesta |
| `429` | Límite por IP excedido |
| `500` | Error interno no expuesto al cliente |
| `503` | Servicio o base no disponible |
