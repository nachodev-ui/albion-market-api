# Migraciones

Las migraciones se encuentran exclusivamente en `migrations/` y se ejecutan en
orden lexicográfico.

| Archivo | Propósito |
|---|---|
| `000001_init.sql` | Raw de precios, precios actuales e índices iniciales |
| `000002_ingest_request_idempotency.sql` | Control de requests de precios |
| `000003_raw_audit_write_path.sql` | Optimización del camino de auditoría raw |
| `000004_market_history.sql` | Requests, raw y buckets de historial |
| `000005_retention_indexes.sql` | Índices para limpieza por lotes |
| `000006_observability_readiness.sql` | Marcador de versión requerido por readiness |

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

## Runner compilado

La imagen contiene `/usr/local/bin/migrate`. El runner usa una conexión directa,
adquiere un advisory lock de sesión, aplica cada archivo dentro de una transacción
y termina con error ante la primera migración fallida.

```powershell
docker run --rm `
  --env "DATABASE_URL=$env:NEON_MIGRATION_DATABASE_URL" `
  albion-market-api:local `
  /usr/local/bin/migrate
```

## Producción en Render

El workflow `Deploy production to Render` ejecuta el runner **antes** de invocar
el deploy hook. La URL directa de Neon vive en el GitHub Environment protegido
`production` bajo `NEON_MIGRATION_DATABASE_URL`.

El orden no puede invertirse:

1. construir la revisión exacta;
2. aplicar y validar migraciones;
3. detener el job ante cualquier error;
4. desplegar esa misma revisión en Render;
5. esperar el SHA en `albion_market_api_build_info`;
6. comprobar `/readyz`.

Render mantiene `autoDeployTrigger: off` para impedir que un push reemplace la
instancia antes de ejecutar las migraciones.

## Aplicación mediante Docker Compose

`deploy/compose.yaml` ejecuta un servicio de una sola ejecución llamado `migrate`.
Espera PostgreSQL, aplica los archivos en orden y debe terminar con código `0`
antes de que la API pueda arrancar.

```powershell
docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  up --build --detach
```

## Regla de mantenimiento

- no reescribir una migración publicada;
- agregar una migración para cada cambio de esquema;
- mantener `migrations/` libre de código y documentación;
- diseñar cambios compatibles con la versión activa;
- separar cualquier eliminación destructiva en una fase posterior;
- validar restauraciones aplicando también las migraciones actuales.

## Readiness del esquema

`000006_observability_readiness.sql` crea `public.app_schema_state` y registra la
versión `6`. `/readyz` exige ese marcador y las relaciones críticas de la API; si
la migración no fue aplicada, la instancia permanece viva pero no lista.
