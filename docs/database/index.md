# PostgreSQL

La persistencia separa auditoría, idempotencia y modelos calientes de lectura.

## Guías

- [Auditoría del esquema](./audit.md)
- [Retención segura](./retention.md)
- [Backups y restauración](./backup-restore.md)
- [Revisión de índices](./index-review.md)
- [Catálogo de migraciones](../reference/migrations.md)

## Política vigente

| Datos | Retención |
|---|---:|
| Raw de precios | 30 días |
| Raw de historial | 30 días |
| Requests de idempotencia completadas y huérfanas | 90 días |
| Historial consolidado | 400 días |
| Precios actuales | sin limpieza |

El particionamiento fue evaluado y pospuesto: el volumen medido no justificaba todavía la complejidad adicional.
