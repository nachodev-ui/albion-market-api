# Operación

## Herramientas operativas

- [Observabilidad](./observability.md)
- [Alertas y respuesta](./alerts.md)
- [Auditoría final de observabilidad](./observability-audit.md)
- [Rendimiento de ingesta](./performance.md)
- [Retención PostgreSQL](../database/retention.md)
- [Backups y restauración](../database/backup-restore.md)
- [Referencia de scripts](../reference/scripts.md)

## Programación recomendada

```text
03:00  Retención PostgreSQL
03:30  Backup PostgreSQL
Domingo 04:30  Verificación de restauración
```

Las tareas locales pueden registrarse en Windows con `scripts/register-postgres-backup-tasks.ps1`.
