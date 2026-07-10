# Albion Market API

[![Quality checks](https://github.com/nachodev-ui/albion-market-api/actions/workflows/quality.yml/badge.svg?branch=develop)](https://github.com/nachodev-ui/albion-market-api/actions/workflows/quality.yml)
[![API contracts](https://github.com/nachodev-ui/albion-market-api/actions/workflows/contracts.yml/badge.svg?branch=develop)](https://github.com/nachodev-ui/albion-market-api/actions/workflows/contracts.yml)
[![Container checks](https://github.com/nachodev-ui/albion-market-api/actions/workflows/container.yml/badge.svg?branch=develop)](https://github.com/nachodev-ui/albion-market-api/actions/workflows/container.yml)
[![Documentation](https://github.com/nachodev-ui/albion-market-api/actions/workflows/documentation.yml/badge.svg?branch=main)](https://github.com/nachodev-ui/albion-market-api/actions/workflows/documentation.yml)
[![Release](https://github.com/nachodev-ui/albion-market-api/actions/workflows/release.yml/badge.svg)](https://github.com/nachodev-ui/albion-market-api/actions/workflows/release.yml)

API centralizada en Go para recibir, consolidar y servir precios e historial de
mercado de Albion Online mediante PostgreSQL.

```text
Albion Data Client
        │
        ▼
receiver local / forwarder
        │ HTTPS + Bearer
        ▼
albion-market-api en Render ─────► Neon PostgreSQL
        │
        ▼
albion-production-calculator en Cloudflare Pages
```

## Producción hosted-first

```text
Cloudflare Pages global
  → Render HTTPS
  → Neon PostgreSQL AWS São Paulo (aws-sa-east-1)
```

Dominios canónicos:

```text
Frontend: https://albion-production-calculator.pages.dev
API:      https://albion-market-api.onrender.com
API v1:   https://albion-market-api.onrender.com/api/v1
```

El frontend público no necesita receiver local. El receiver es una herramienta
separada para colaboradores y entrega datos a la API autenticada.

La configuración declarativa vive en [`render.yaml`](render.yaml). Los despliegues
automáticos están desactivados: el workflow manual de producción ejecuta primero
las migraciones de Neon, despliega una revisión exacta en Render y después valida
health, readiness, CORS, autenticación sin escritura, precios, historial y el
frontend de Cloudflare.

Consulta la guía completa en
[`docs/deployment/render-neon-production.md`](docs/deployment/render-neon-production.md).

## Documentación

La documentación completa se publica mediante GitHub Pages:

**[Abrir portal de documentación](https://nachodev-ui.github.io/albion-market-api/)**

Accesos directos:

- [Inicio rápido](https://nachodev-ui.github.io/albion-market-api/guide/getting-started)
- [Referencia HTTP](https://nachodev-ui.github.io/albion-market-api/api/endpoints)
- [Configuración](https://nachodev-ui.github.io/albion-market-api/guide/configuration)
- [PostgreSQL y mantenimiento](https://nachodev-ui.github.io/albion-market-api/database/)
- [Seguridad](https://nachodev-ui.github.io/albion-market-api/security/)
- [Operación y observabilidad](https://nachodev-ui.github.io/albion-market-api/operations/)
- [Despliegue](https://nachodev-ui.github.io/albion-market-api/deployment/)
- [Releases, firma y rollback](https://nachodev-ui.github.io/albion-market-api/release/)

## Inicio rápido local

Requisitos: Go, PostgreSQL y PowerShell 5.1 o superior. Docker Desktop es
necesario para validar la imagen de producción.

```powershell
Copy-Item .env.example .env.local

Get-ChildItem .\migrations\*.sql |
    Sort-Object Name |
    ForEach-Object {
        psql $env:DATABASE_URL -v ON_ERROR_STOP=1 -f $_.FullName
    }

go test ./...
go run ./cmd/api
```

La API escucha en `:8080` por defecto:

```powershell
Invoke-RestMethod http://127.0.0.1:8080/healthz
Invoke-RestMethod http://127.0.0.1:8080/readyz
(Invoke-WebRequest http://127.0.0.1:8080/metrics).Content
```

## Producción en Render

El servicio existente debe coincidir con `render.yaml` y mantener
`autoDeployTrigger: off`. El GitHub Environment `production` requiere aprobación
y contiene:

```text
NEON_MIGRATION_DATABASE_URL
RENDER_DEPLOY_HOOK_URL
```

La primera es la conexión **directa** a la base `albion_market`; la segunda es el
deploy hook secreto del servicio Render. Los secretos de runtime `DATABASE_URL` e
`INGEST_BEARER_TOKEN` permanecen en Render.

Para desplegar, abre **Actions → Deploy production to Render**, selecciona `main`
y escribe `DEPLOY`. El workflow:

1. construye la revisión con `VERSION`, `REVISION` y `CREATED`;
2. aplica migraciones con advisory lock;
3. detiene el proceso si Neon falla;
4. dispara Render para el SHA exacto;
5. espera ese SHA en `albion_market_api_build_info`;
6. ejecuta todas las verificaciones públicas y de seguridad.

## Despliegue local reproducible

```powershell
.\scripts\initialize-deployment.ps1 `
  -AllowedOrigins "https://frontend.example.com"

docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  up --build --detach
```

La API solo arranca después de que PostgreSQL esté saludable y todas las
migraciones terminen correctamente. Los secretos se montan como archivos y no se
incorporan a la imagen.

## Distribución verificada

Los tags estables `vMAJOR.MINOR.PATCH` creados desde el `main` vigente publican
una imagen OCI en GitHub Container Registry y un GitHub Release con SBOM SPDX,
checksums, firma Cosign keyless y attestations de provenance/SBOM.

```text
ghcr.io/nachodev-ui/albion-market-api@sha256:<digest>
```

## Endpoints principales

| Método | Ruta | Propósito |
|---|---|---|
| `GET` | `/healthz` | Liveness del proceso |
| `GET` | `/readyz` | Readiness del pool, PostgreSQL y esquema |
| `GET` | `/metrics` | Métricas Prometheus y build info |
| `GET` | `/api/v1/status` | Estado operativo y métricas |
| `GET` | `/api/v1/markets` | Catálogo público de mercados |
| `GET` | `/api/v1/prices` | Consulta simple de precios |
| `POST` | `/api/v1/prices/query` | Consulta batch de precios |
| `GET` | `/api/v1/history` | Consulta simple de historial |
| `POST` | `/api/v1/history/query` | Consulta batch de historial |
| `POST` | `/api/v1/ingest/prices` | Ingesta autenticada de precios |
| `POST` | `/api/v1/ingest/history` | Ingesta autenticada de historial |

## Desarrollo

```powershell
go test ./...
go vet ./...
go build ./cmd/api
go build ./cmd/migrate
npm ci
npm run contracts:check
npm run docs:dev
python .\scripts\validate-render-config.py
.\scripts\test-container.ps1
.\scripts\test-deployment-compose.ps1
.\scripts\test-observability-compose.ps1
```

Toda guía extensa vive en [`docs/`](./docs/), que es la única fuente del portal
VitePress.
