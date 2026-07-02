# Albion Market API

[![Quality checks](https://github.com/nachodev-ui/albion-market-api/actions/workflows/quality.yml/badge.svg?branch=develop)](https://github.com/nachodev-ui/albion-market-api/actions/workflows/quality.yml)
[![API contracts](https://github.com/nachodev-ui/albion-market-api/actions/workflows/contracts.yml/badge.svg?branch=develop)](https://github.com/nachodev-ui/albion-market-api/actions/workflows/contracts.yml)
[![Container checks](https://github.com/nachodev-ui/albion-market-api/actions/workflows/container.yml/badge.svg?branch=develop)](https://github.com/nachodev-ui/albion-market-api/actions/workflows/container.yml)
[![Documentation](https://github.com/nachodev-ui/albion-market-api/actions/workflows/documentation.yml/badge.svg?branch=main)](https://github.com/nachodev-ui/albion-market-api/actions/workflows/documentation.yml)

API centralizada en Go para recibir, consolidar y servir precios e historial de mercado de Albion Online mediante PostgreSQL.

```text
Albion Data Client
        │
        ▼
receiver local / forwarder
        │
        ▼
albion-market-api ─────► PostgreSQL
        │
        ▼
albion-craft-calculator
```

## Documentación

La documentación completa es navegable, buscable y se publica con GitHub Pages:

**[Abrir portal de documentación](https://nachodev-ui.github.io/albion-market-api/)**

Accesos directos:

- [Inicio rápido](https://nachodev-ui.github.io/albion-market-api/guide/getting-started)
- [Referencia HTTP](https://nachodev-ui.github.io/albion-market-api/api/endpoints)
- [Configuración](https://nachodev-ui.github.io/albion-market-api/guide/configuration)
- [PostgreSQL y mantenimiento](https://nachodev-ui.github.io/albion-market-api/database/)
- [Seguridad](https://nachodev-ui.github.io/albion-market-api/security/)
- [Operación y observabilidad](https://nachodev-ui.github.io/albion-market-api/operations/)
- [Despliegue con contenedores](https://nachodev-ui.github.io/albion-market-api/deployment/)

## Inicio rápido

Requisitos: Go, PostgreSQL y PowerShell 5.1 o superior para los scripts operativos. Docker Desktop es necesario para validar la imagen de producción.

```powershell
Copy-Item .env.example .env.local

# Para este flujo manual, configura DATABASE_URL e INGEST_BEARER_TOKEN(_FILE).
Get-ChildItem .\migrations\*.sql |
    Sort-Object Name |
    ForEach-Object {
        psql $env:DATABASE_URL -v ON_ERROR_STOP=1 -f $_.FullName
    }

go test ./...
go run ./cmd/api
```

La API escucha en `:8080` por defecto. Comprueba liveness, readiness y métricas con:

```powershell
Invoke-RestMethod http://127.0.0.1:8080/healthz
Invoke-RestMethod http://127.0.0.1:8080/readyz
(Invoke-WebRequest http://127.0.0.1:8080/metrics).Content
```

## Despliegue local reproducible

```powershell
.\scripts\initialize-deployment.ps1 `
  -AllowedOrigins "https://frontend.example.com"

docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  up --build --detach
```

La API solo arranca después de que PostgreSQL esté saludable y todas las migraciones terminen correctamente. Los secretos se montan como archivos y no se incorporan a la imagen.

## Endpoints principales

| Método | Ruta | Propósito |
|---|---|---|
| `GET` | `/healthz` | Liveness del proceso |
| `GET` | `/readyz` | Readiness con PostgreSQL |
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
npm ci
npm run contracts:check
npm run docs:dev
.\scripts\test-container.ps1
.\scripts\test-deployment-compose.ps1
```

La raíz del repositorio se mantiene deliberadamente pequeña. Toda guía extensa vive en [`docs/`](./docs/), que es la única fuente del portal VitePress.
