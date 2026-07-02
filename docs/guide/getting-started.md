# Inicio rápido

## Requisitos

- Go según la versión declarada en `go.mod`;
- PostgreSQL con herramientas cliente (`psql`, `pg_dump`, `pg_restore`);
- PowerShell 5.1 o superior para los scripts operativos;
- Node.js 20 o superior únicamente para el portal de documentación.

## 1. Preparar configuración

```powershell
Copy-Item .env.example .env.local
```

Edita `.env.local` y configura al menos:

```dotenv
APP_ENV=development
HTTP_ADDR=:8080
DATABASE_URL=postgres://postgres:TU_CLAVE@localhost:5432/albion_market?sslmode=disable
INGEST_BEARER_TOKEN=un-token-local-de-al-menos-32-caracteres
INGEST_BEARER_TOKEN_FILE=
```

No confirmes `.env.local` ni archivos de tokens en Git.

## 2. Crear y migrar la base

```powershell
psql -U postgres -v ON_ERROR_STOP=1 -c "CREATE DATABASE albion_market;"

Get-ChildItem .\migrations\*.sql |
    Sort-Object Name |
    ForEach-Object {
        psql $env:DATABASE_URL -v ON_ERROR_STOP=1 -f $_.FullName
        if ($LASTEXITCODE -ne 0) {
            throw "Falló la migración $($_.Name)"
        }
    }
```

Si `DATABASE_URL` solo está en `.env.local`, carga su valor en la sesión o utiliza los wrappers documentados en [scripts](../reference/scripts.md).

## 3. Validar y ejecutar

```powershell
go test ./...
go vet ./...
go run ./cmd/api
```

## 4. Comprobar salud

```powershell
Invoke-RestMethod http://127.0.0.1:8080/healthz
Invoke-RestMethod http://127.0.0.1:8080/readyz
(Invoke-WebRequest http://127.0.0.1:8080/metrics).Content
Invoke-RestMethod http://127.0.0.1:8080/api/v1/status |
    ConvertTo-Json -Depth 10
```

## 5. Consultar mercados

```powershell
Invoke-RestMethod http://127.0.0.1:8080/api/v1/markets
```

Continúa con la [referencia HTTP](../api/endpoints.md) y la [configuración completa](./configuration.md).
