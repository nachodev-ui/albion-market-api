# Descripción general

`albion-market-api` es el punto central del sistema de datos de mercado. Recibe batches normalizados, aplica autenticación e idempotencia, conserva una auditoría durable y expone contratos de lectura basados en claves públicas de mercado.

## Responsabilidades

- recibir precios actuales e historial desde el receiver;
- validar y autenticar cada ingesta;
- impedir duplicados por `request_id`;
- conservar datos raw durante una ventana de auditoría;
- mantener modelos calientes para las consultas públicas;
- exponer salud, estado y métricas operativas;
- ejecutar retención, backups y verificaciones de restauración.

## Fuera de alcance

- capturar directamente el tráfico del Albion Data Client;
- calcular rentabilidad de crafteo;
- sustituir un sistema de recuperación punto en el tiempo basado en WAL;
- administrar automáticamente las migraciones durante el arranque.

Continúa con [arquitectura](./architecture.md) o [inicio rápido](./getting-started.md).
