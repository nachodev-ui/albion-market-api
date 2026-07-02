# Migraciones

Las migraciones se encuentran exclusivamente en `migrations/` y se ejecutan en orden lexicográfico.

| Archivo | Propósito |
|---|---|
| `000001_init.sql` | Raw de precios, precios actuales e índices iniciales |
| `000002_ingest_request_idempotency.sql` | Control de requests de precios |
| `000003_raw_audit_write_path.sql` | Optimización del camino de auditoría raw |
| `000004_market_history.sql` | Requests, raw y buckets de historial |
| `000005_retention_indexes.sql` | Índices para limpieza por lotes |

## Aplicación manual

```powershell
Get-ChildItem .\migrations\*.sql |
    Sort-Object Name |
    ForEach-Object {
        psql $env:DATABASE_URL -v ON_ERROR_STOP=1 -f $_.FullName
        if ($LASTEXITCODE -ne 0) {
            throw "Falló la migración $($_.Name)"
        }
    }
```

## Regla de mantenimiento

- no reescribir una migración publicada;
- agregar una nueva migración para cada cambio de esquema;
- mantener `migrations/` libre de código, documentación y artefactos;
- validar restauraciones aplicando también las migraciones actuales.
