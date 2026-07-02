# Albion Market API

[![Quality checks](https://github.com/nachodev-ui/albion-market-api/actions/workflows/quality.yml/badge.svg?branch=develop)](https://github.com/nachodev-ui/albion-market-api/actions/workflows/quality.yml)
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

## Inicio rápido

Requisitos: Go, PostgreSQL y PowerShell 5.1 o superior para los scripts operativos.

```powershell
Copy-Item .env.example .env.local

# Edita .env.local y configura DATABASE_URL e INGEST_BEARER_TOKEN(_FILE).
Get-ChildItem .\migrations\*.sql |
    Sort-Object Name |
    ForEach-Object {
        psql $env:DATABASE_URL -v ON_ERROR_STOP=1 -f $_.FullName
    }

go test ./...
go run ./cmd/api
```

La API escucha en `:8080` por defecto. Comprueba el servicio con:

```powershell
Invoke-RestMethod http://127.0.0.1:8080/healthz
```

## Endpoints principales

| Método | Ruta | Propósito |
|---|---|---|
| `GET` | `/healthz` | Salud de la API y PostgreSQL |
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
npm install
npm run docs:dev
```

La raíz del repositorio se mantiene deliberadamente pequeña. Toda guía extensa vive en [`docs/`](./docs/), que es la única fuente del portal VitePress.
