# Scripts operativos

Los scripts PowerShell resuelven la conexión desde un parámetro explícito,
variables de entorno o `.env.local`, según su responsabilidad. Nunca confirmes
credenciales ni archivos temporales generados.

## Auditoría e índices

| Script | Propósito |
|---|---|
| `audit-postgres.ps1` | Inventario de tablas, restricciones, índices, tamaños y uso |
| `review-postgres-indexes.ps1` | `EXPLAIN (ANALYZE, BUFFERS)` sobre consultas reales |
| `benchmark-ingest-postgres.ps1` | Benchmark controlado del camino de ingesta |

## Retención

| Script | Propósito |
|---|---|
| `apply-retention-indexes.ps1` | Aplica la migración de índices de retención |
| `postgres-retention.ps1` | Dry-run o eliminación segura por lotes |
| `test-postgres-retention.ps1` | Prueba integral con base desechable |
| `invoke-postgres-retention-task.ps1` | Wrapper de la tarea programada |

## Backup y recuperación

| Script | Propósito |
|---|---|
| `postgres-backup.ps1` | Dump custom, SHA256, manifiesto y retención |
| `postgres-restore-verify.ps1` | Restauración y validación en una base limpia |
| `verify-latest-postgres-backup.ps1` | Selecciona y verifica el backup más reciente |
| `test-postgres-backup-restore.ps1` | Prueba integral de backup y restauración |
| `postgres-client.ps1` | Utilidades seguras de conexión para herramientas PostgreSQL |

## Despliegue y contenedores

| Script | Propósito |
|---|---|
| `validate-render-config.py` | Valida `render.yaml`, el workflow y la retirada completa de Fly |
| `initialize-deployment.ps1` | Genera secretos y configuración local para Docker Compose |
| `new-production-ingest-token.ps1` | Genera el token compartido por Render y el receiver |
| `test-container.ps1` | Construye la imagen de producción y ejecuta un smoke test aislado |
| `test-deployment-compose.ps1` | Valida PostgreSQL, migraciones, secretos y runtime mediante Compose |
| `test-observability-compose.ps1` | Valida Prometheus, Alertmanager, Grafana y su dashboard |

Consulta [Despliegue reproducible y seguro](../deployment/) y
[Producción en Render, Neon y Cloudflare](../deployment/render-neon-production.md).

## Tareas de Windows

| Script | Propósito |
|---|---|
| `register-postgres-backup-tasks.ps1` | Registra retención, backup y verificación |
| `unregister-postgres-backup-tasks.ps1` | Elimina las tareas registradas |
| `get-postgres-backup-task-status.ps1` | Resume estado y códigos de salida |
| `invoke-postgres-backup-task.ps1` | Wrapper del backup diario |
| `invoke-postgres-restore-verification-task.ps1` | Wrapper de la verificación semanal |

Consulta [retención](../database/retention.md) y
[backup/restauración](../database/backup-restore.md) antes de usar modos
destructivos.
