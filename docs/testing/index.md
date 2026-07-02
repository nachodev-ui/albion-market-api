# Pruebas

## API

```powershell
go test ./...
go vet ./...
go test -race ./...
go build ./cmd/api
```

## PostgreSQL

```powershell
.\scripts\test-postgres-retention.ps1
.\scripts\test-postgres-backup-restore.ps1
```

Ambas pruebas usan bases desechables y no deben apuntar a producción.

## Flujo de tres proyectos

Consulta la [prueba end-to-end](./end-to-end.md).

## Contratos HTTP

```powershell
npm ci
npm run contracts:check
```

Consulta [Pruebas de contratos](./contracts.md) para el lint OpenAPI y las
comparaciones automáticas con Go.

## Despliegue con contenedores

```powershell
.\scripts\test-container.ps1
.\scripts\test-deployment-compose.ps1
```

La primera prueba valida directamente la imagen. La segunda valida el modelo Compose completo: imágenes fijadas por digest, secretos montados, migraciones obligatorias, runtime endurecido, `/healthz`, `/readyz`, `/metrics`, ausencia de secretos en métricas y apagado por `SIGTERM`. También detiene PostgreSQL temporalmente para comprobar que liveness continúa saludable, readiness responde `503`, el contenedor no se reinicia y readiness se recupera al restaurar la base. Consulta [Despliegue reproducible y seguro](../deployment/).

## Documentación

```powershell
npm ci
npm run docs:build
npm run docs:preview
```

El workflow de documentación ejecuta el build en pull requests y publica GitHub Pages desde `main`.
