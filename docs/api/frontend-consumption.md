# Contrato de lectura para el frontend

`albion-market-api` expone un contrato público basado en `marketKey`. El
frontend no necesita conocer los `location_id` numéricos usados internamente
por Albion ni por PostgreSQL.

## Mercados disponibles

```http
GET /api/v1/markets
```

Respuesta:

```json
{
  "schemaVersion": 1,
  "count": 7,
  "data": [
    {
      "key": "brecilien",
      "name": "Brecilien",
      "type": "regular",
      "enabled": true
    }
  ]
}
```

Para diagnóstico puede incluirse el mercado deshabilitado:

```http
GET /api/v1/markets?includeDisabled=true
```

Los mercados habilitados son:

- `bridgewatch`
- `martlock`
- `lymhurst`
- `fort_sterling`
- `thetford`
- `caerleon`
- `brecilien`

`black_market` permanece publicado únicamente cuando se solicita
`includeDisabled=true`; actualmente está deshabilitado porque el catálogo de
captura no tiene un `marketLocationId` utilizable.

## Consulta batch recomendada

```http
POST /api/v1/prices/query
Content-Type: application/json
```

```json
{
  "server": "west",
  "marketKeys": ["martlock", "fort_sterling"],
  "entries": [
    {
      "itemIdentifier": "T4_BAG",
      "quality": 1
    },
    {
      "itemIdentifier": "T5_MAIN_SWORD",
      "quality": 3
    }
  ]
}
```

Respuesta:

```json
{
  "requestedAt": "2026-06-26T04:00:00Z",
  "count": 1,
  "data": [
    {
      "server": "west",
      "marketKey": "martlock",
      "itemIdentifier": "T4_BAG",
      "quality": 1,
      "sellPriceMin": 12500,
      "sellPriceMinDate": "2026-06-26T03:58:20Z",
      "buyPriceMax": 11900,
      "buyPriceMaxDate": "2026-06-26T03:57:55Z",
      "updatedAt": "2026-06-26T03:58:22Z"
    }
  ]
}
```

La ausencia de una combinación solicitada se representa omitiendo esa fila de
`data`; el frontend puede detectarla comparando la matriz solicitada con las
filas recibidas. `data` siempre es un arreglo, incluso cuando no existen
resultados.

Límites de validación actuales:

- hasta 32 mercados por consulta;
- hasta 2000 pares objeto/calidad por consulta;
- calidad entre 1 y 5;
- servidores permitidos: `west`, `east` y `europe`.

Las claves y los pares objeto/calidad repetidos se normalizan y deduplican antes
de consultar PostgreSQL.

## Consulta GET compatible con el receiver local

Para consultas simples se mantiene una ruta con la misma forma de parámetros
que utiliza el receiver local:

```http
GET /api/v1/prices?server=west&itemIds=T4_BAG,T5_BAG&marketKey=martlock&quality=1
```

La respuesta usa la misma envoltura `requestedAt`, `count` y `data` del endpoint
batch. Esta ruta permite que el futuro cliente frontend cambie entre API central
y receiver local sin exponer IDs internos. Para varios mercados o calidades
mixtas debe preferirse `POST /api/v1/prices/query`.

## Frescura

Para determinar la frescura real del precio, el frontend debe usar:

- `sellPriceMinDate` cuando consume `sellPriceMin`;
- `buyPriceMaxDate` cuando consume `buyPriceMax`.

`updatedAt` indica cuándo cambió la fila caliente en PostgreSQL y `requestedAt`
indica cuándo respondió la API; ninguno sustituye la fecha específica del
precio.

## PowerShell

```powershell
$body = @{
  server = "west"
  marketKeys = @("martlock", "fort_sterling")
  entries = @(
    @{ itemIdentifier = "T4_BAG"; quality = 1 }
    @{ itemIdentifier = "T5_MAIN_SWORD"; quality = 3 }
  )
} | ConvertTo-Json -Depth 5

Invoke-RestMethod `
  -Method Post `
  -Uri "http://127.0.0.1:8080/api/v1/prices/query" `
  -ContentType "application/json" `
  -Body $body
```

La incorporación original de `marketKey` para precios actuales no requirió una
migración. El historial central sí requiere aplicar
`migrations/000004_market_history.sql`; el contrato completo está documentado
en [historial centralizado](./market-history.md).

## Historial por `marketKey`

La consulta batch recomendada es:

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

La respuesta devuelve series identificadas por `marketKey` y no serializa
`location_id`. `rangeStart` y `rangeEnd` son fechas UTC inclusivas. Para el
fallback simple del frontend también existe:

```http
GET /api/v1/history?server=west&marketKey=fort_sterling&itemId=T5_LEATHER_LEVEL4@4&quality=1&period=4-weeks&limit=1
```
