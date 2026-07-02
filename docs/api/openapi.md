# Contrato OpenAPI y auditoría inicial

El contrato canónico inicial vive en:

```text
openapi/openapi.yaml
```

Está declarado como **OpenAPI 3.1.1** con JSON Schema 2020-12. Esta primera
versión documenta la implementación existente; no cambia handlers, servicios,
repositorios ni respuestas HTTP.

## Alcance auditado

La auditoría contrastó:

- `internal/server/router.go` para inventariar rutas;
- `internal/handlers/*` para métodos, headers, parsing y códigos HTTP;
- `internal/domain/market.go` para nombres y formas JSON;
- `internal/service/*` para validaciones y límites;
- `internal/ingestauth/authenticator.go` para Bearer y HTTPS;
- `internal/server/security.go` para CORS y rate limiting;
- pruebas de handlers, servicios y router como evidencia ejecutable.

## Inventario confirmado

| Método | Ruta | Autenticación | Éxito |
|---|---|---|---|
| `GET` | `/healthz` | pública | `200` |
| `GET` | `/api/v1/status` | pública | `200`, o `503` degradado |
| `GET` | `/api/v1/markets` | pública | `200` |
| `GET` | `/api/v1/prices` | pública | `200` |
| `POST` | `/api/v1/prices/query` | pública | `200` |
| `GET` | `/api/v1/history` | pública | `200` |
| `POST` | `/api/v1/history/query` | pública | `200` |
| `POST` | `/api/v1/ingest/prices` | Bearer | `202` nuevo, `200` duplicado |
| `POST` | `/api/v1/ingest/history` | Bearer | `202` nuevo, `200` duplicado |

El prefijo `/api/v1` es la versión mayor del contrato público. `/healthz` se
mantiene fuera del prefijo por ser una sonda operacional.

## Convenciones preservadas

El contrato no intenta uniformar nombres ya publicados:

- ingesta, estado y métricas usan principalmente `snake_case`;
- lectura pública de precios e historial usa `camelCase`;
- `location_id` existe solo en ingesta interna;
- las respuestas públicas usan `marketKey` y nunca serializan IDs internos;
- los POST rechazan campos JSON desconocidos;
- las fechas de respuesta son timestamps UTC y los rangos históricos son fechas
  inclusivas `YYYY-MM-DD`.

También quedaron modelados los comportamientos transversales:

- CORS puede responder `403` antes de llegar al handler;
- el rate limiter puede responder `429` en todas las rutas salvo `/healthz`;
- ingesta admite `identity` y `gzip`;
- ingesta puede exigir HTTPS y responder `426`;
- errores internos se sustituyen por `internal server error`.

## Hallazgos que esta etapa no corrige

### 1. `limit` de historial no limita

`GET /api/v1/history` valida que `limit` sea un entero positivo, pero no lo
propaga al servicio ni recorta la respuesta. OpenAPI lo declara exactamente así
para no prometer un efecto inexistente.

### 2. La ingesta de precios es más permisiva por entrada

`IngestPrices` valida `request_id`, `server` y que `entries` no esté vacío, pero
no valida todavía cada `observed_at`, `location_id`, `item_key`, `quality` o
precio. Por eso `PriceIngest` conserva esas propiedades sin marcarlas como
requeridas en el contrato de estado actual. La ingesta histórica sí aplica
validación detallada y su esquema es estricto.

### 3. UUID inválido en ingesta de precios

Un `request_id` no vacío pero inválido llega al repositorio y puede terminar en
`500`. El esquema exige `format: uuid` porque solo un UUID puede completar la
operación, pero no se modificó todavía el mapeo de ese error a `400`.

### 4. `Allow` no es uniforme en estado

La mayoría de handlers agregan `Allow` al responder `405`. `StatusHandler` no lo
hace actualmente, y el contrato no afirma ese header para `/api/v1/status`.

### 5. `Content-Type` vacío se tolera

Los POST aceptan un header `Content-Type` ausente y lo tratan como JSON. El
contrato publica `application/json` como media type normativo, sin cambiar esa
tolerancia existente.

## Verificación automática

El contrato ya se valida con Redocly CLI y con pruebas de divergencia escritas
en Go:

```powershell
npm run openapi:lint
npm run contracts:check
```

`openapi:lint` aplica el conjunto `recommended` definido en `redocly.yaml`.
Las operaciones públicas declaran explícitamente `security: []`; las dos rutas
de ingesta conservan `ingestBearer`.

Las pruebas de `internal/contracts/openapi_contract_test.go` comparan:

- cada combinación método/ruta de OpenAPI contra los `HandleFunc` reales;
- el método permitido por cada handler contra el método documentado;
- las propiedades de 18 esquemas compartidos contra los tags `json` de Go;
- la versión esperada del documento (`3.1.1`).

El workflow `.github/workflows/contracts.yml` ejecuta estas verificaciones en
pull requests y pushes dirigidos a `develop` o `main`. Consulta
[Pruebas de contratos](../testing/contracts.md) para el flujo de mantenimiento.

## Siguiente cobertura pendiente

Esta primera barrera automática todavía no compara tipos JSON, nulabilidad,
campos `required`, límites numéricos ni los DTO privados del endpoint de estado.
Tampoco genera o valida tipos TypeScript ni clasifica cambios incompatibles entre
versiones del contrato. Esas capas se añadirán sobre el contrato canónico actual.
