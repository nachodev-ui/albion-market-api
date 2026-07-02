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

## Documentación

```powershell
npm ci
npm run docs:build
npm run docs:preview
```

El workflow de documentación ejecuta el build en pull requests y publica GitHub Pages desde `main`.
