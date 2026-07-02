# Revisión de índices PostgreSQL con planes reales

Esta revisión cierra la decisión de índices de la etapa PostgreSQL sin modificar
el esquema automáticamente. Ejecuta `EXPLAIN (ANALYZE, BUFFERS)` sobre las
formas exactas de consulta usadas por los repositorios y sobre las selecciones de
candidatos utilizadas por la retención.

## Alcance

Se miden:

- consulta batch de `current_market_prices`;
- consulta por rango de `market_history_buckets`;
- lecturas por `request_id` de ambos raw;
- candidatos de retención para ambos raw;
- requests completadas, antiguas y huérfanas;
- buckets anteriores al horizonte de 400 días.

La revisión también ejecuta una segunda medición de precios con
`enable_seqscan = off`. Esa medición no representa el comportamiento normal del
servidor: sirve únicamente para descubrir qué índice elegiría PostgreSQL si una
lectura indexada fuese preferible.

Todas las consultas se ejecutan dentro de transacciones `READ ONLY`. La revisión
no crea, elimina ni reconstruye índices. En los candidatos de retención se omite
`FOR UPDATE SKIP LOCKED` para evitar bloqueos; los predicados, orden y límite que
determinan el acceso por índice permanecen iguales.

## Ejecución

En PowerShell, con `psql` disponible en `PATH`:

```powershell
$env:Path = "C:\Program Files\PostgreSQL\18\bin;$env:Path"

.\scripts\review-postgres-indexes.ps1
```

La muestra predeterminada usa hasta 8 ubicaciones, 100 pares de ítem/calidad y
un lote de retención de 5000 filas. Puede ampliarse sin superar los límites del
API:

```powershell
.\scripts\review-postgres-indexes.ps1 `
  -SampleLocations 32 `
  -SampleEntries 2000 `
  -RetentionBatchSize 5000
```

`EXPLAIN ANALYZE` ejecuta los `SELECT`. Actualmente son consultas pequeñas, pero
en una base grande conviene ejecutar la revisión fuera del horario crítico.

## Reportes

Cada ejecución genera:

```text
artifacts/postgres-index-review/postgres-index-review-*.txt
artifacts/postgres-index-review/postgres-index-review-*.json
```

El TXT contiene los planes completos, tiempos, buffers, estadísticas de tablas
y uso acumulado de índices. El JSON resume la decisión automatizada para:

```text
current_market_prices_item_loc_idx
```

Resultados posibles:

- `keep`: el índice secundario apareció en al menos un plan medido;
- `candidate-for-removal`: el plan indexado eligió la PK y el índice secundario
  no registraba usos antes de la medición;
- `observe-before-removal`: la PK cubre el plan, pero hay uso histórico del
  índice secundario que debe explicarse;
- `manual-review-required`: la evidencia fue insuficiente.

## Criterio para eliminar el índice secundario

No se debe crear una migración de eliminación solo porque la tabla actual sea
pequeña o el plan normal use `Seq Scan`. En tablas pequeñas, un recorrido
secuencial puede ser la opción correcta.

La eliminación queda justificada cuando se cumplen conjuntamente:

1. el plan de la consulta real no usa
   `current_market_prices_item_loc_idx`;
2. con `enable_seqscan = off`, PostgreSQL selecciona
   `current_market_prices_pkey`;
3. el contador histórico `idx_scan` del índice secundario es cero o su uso se
   explica únicamente por pruebas anteriores;
4. la consulta se prueba con una muestra representativa, idealmente hasta los
   límites de 32 mercados y 2000 entradas;
5. no existe otra consulta del repositorio que filtre comenzando por
   `item_key` sin `server` y `location_id`.

Si el resultado es `candidate-for-removal`, el paso siguiente es crear una
migración separada, volver a ejecutar las pruebas y comparar los mismos planes.
Este script deliberadamente no realiza esa eliminación.

## Índices esperados en los demás planes

- `market_ingest_raw_request_id_idx` para el raw de precios por request;
- `market_history_ingest_raw_request_id_idx` para el raw histórico por request;
- `market_ingest_raw_received_at_id_idx` para candidatos raw de precios;
- `market_history_ingest_raw_received_at_id_idx` para candidatos raw históricos;
- índices `created_at` de los ledgers más los índices raw por `request_id` para
  requests antiguas y huérfanas;
- `market_history_buckets_bucket_at_idx` para el horizonte de 400 días;
- PK de `market_history_buckets` para la consulta pública por servidor,
  ubicación, ítem, calidad y rango temporal.

Un `Seq Scan` en un ledger de pocas decenas de filas no implica un problema. La
revisión debe considerar cardinalidad, tiempo, buffers y el plan que aparece al
crecer la tabla, no solo el nombre del nodo actual.
