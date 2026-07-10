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
albion-market-api ─────► PostgreSQL
        │
        ▼
albion-production-calculator
```

## Producción hosted-first

La topología pública recomendada es:

```text
Cloudflare Pages global
  → Fly.io São Paulo (gru)
  → Neon PostgreSQL AWS São Paulo (aws-sa-east-1)
```

Dominios configurados:

```text
Frontend: https://albion-production-calculator.pages.dev
API:      https://albion-market-api-nachodev.fly.dev
API v1:   https://albion-market-api-nachodev.fly.dev/api/v1
```

El frontend público no necesita receiver local. El receiver es una herramienta
separada para colaboradores y entrega datos directamente a la API autenticada.

La guía completa está en
[`docs/deployment/fly-neon-production.md`](docs/deployment/fly-neon-production.md).

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

## Primer despliegue público

Después de crear PostgreSQL en Neon y guardar su URL directa en un archivo local
ignorado por Git:

```powershell
.\scripts\new-production-ingest-token.ps1
.\scripts\bootstrap-fly-production.ps1
```

El bootstrap crea la aplicación Fly.io, importa `DATABASE_URL` e
`INGEST_BEARER_TOKEN` al vault cifrado, ejecuta las migraciones de release y
comprueba `/healthz` y `/readyz`.

GitHub Actions usa únicamente `FLY_API_TOKEN` dentro del Environment
`production`. Los secretos de base de datos e ingesta no se duplican en GitHub.

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

En producción debe fijarse el digest inmutable cuando se despliega una imagen
publicada externamente:

```text
ghcr.io/nachodev-ui/albion-market-api@sha256:<digest>
```

## Endpoints principales

| Método | Ruta | Propósito |
|---|---|---|
| `GET` | `/healthz` | Liveness del proceso |
| `GET` | `/readyz` | Readiness del pool, PostgreSQL y esquema |
| `GET` | `/metrics` | Métricas Prometheus |
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
.\scripts\test-container.ps1
.\scripts\test-deployment-compose.ps1
.\scripts\test-observability-compose.ps1
```

Toda guía extensa vive en [`docs/`](./docs/), que es la única fuente del portal
VitePress.
