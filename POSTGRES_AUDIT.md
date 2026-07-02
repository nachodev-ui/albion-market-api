# Auditoría PostgreSQL previa a retención, particionamiento y backups

Fecha de auditoría del código: 2026-07-02
Rama: `feat/postgres-retention-backups`

## 1. Alcance y regla de esta etapa

La auditoría se realizó antes de implementar la retención para evitar diseñarla
a ciegas. Primero dejó inventariados:

- el esquema definido por las migraciones;
- las consultas reales del repositorio Go;
- los índices que esas consultas pueden utilizar;
- los puntos de crecimiento no acotado;
- el volumen esperado mediante fórmulas y escenarios;
- la información que debe capturarse desde una base real antes de diseñar la migración.

El inventario reproducible se ejecuta con:

```powershell
.\scripts\audit-postgres.ps1
```

La conexión se resuelve, en orden, desde `-DatabaseUrl`, la variable de entorno
`DATABASE_URL` o el archivo local `.env.local`. El valor no se imprime en el
reporte.

El reporte se guarda fuera del control de versiones en
`artifacts/postgres-audit/`.

La retención por lotes ya se implementó después de esta auditoría y está
documentada en `POSTGRES_RETENTION.md`. El particionamiento y la eliminación del
índice secundario de precios siguen aplazados hasta contar con evidencia que los
justifique.

## 2. Migraciones y administración del esquema

Migraciones actuales, en orden:

1. `000001_init.sql`
   - `market_ingest_raw`;
   - `current_market_prices`;
   - índices iniciales.
2. `000002_ingest_request_idempotency.sql`
   - `market_ingest_requests`.
3. `000003_raw_audit_write_path.sql`
   - elimina el índice ancho de lectura de `market_ingest_raw`.
4. `000004_market_history.sql`
   - `market_history_ingest_requests`;
   - `market_history_ingest_raw`;
   - `market_history_buckets`.
5. `000005_retention_indexes.sql`
   - índices `(received_at, id)` para las tablas raw;
   - índice `(bucket_at)` para la limpieza del historial consolidado.

Hallazgos:

- La API no aplica migraciones al arrancar. La aplicación del esquema depende de `psql` o del orquestador E2E externo.
- No existe una tabla de versión de esquema dentro de PostgreSQL. Por lo tanto, una base puede quedar parcialmente migrada sin una señal interna única y verificable.
- Las migraciones usan ampliamente `if not exists`. Esto facilita reejecuciones, pero también puede ocultar deriva: una tabla o índice con el nombre correcto y una definición antigua no se corrige automáticamente.
- Las migraciones ya publicadas no deben reescribirse para esta etapa. Toda corrección futura debe añadirse como una migración nueva y reversible cuando sea posible.

Antes de automatizar backups/restauraciones se debe decidir si se incorpora un
runner de migraciones con historial o si se mantiene `psql` con una tabla propia
de control.

## 3. Inventario de tablas

### `market_ingest_raw`

Propósito: auditoría append-only de precios recibidos.

Escrituras y lecturas reales:

- `COPY FROM` por cada batch nuevo;
- lectura por `request_id` dentro de la misma transacción para reducir el batch y actualizar `current_market_prices`;
- no participa en lecturas públicas.

Crecimiento: no acotado.

Índices actuales:

- PK sobre `id`;
- `market_ingest_raw_request_id_idx (request_id)`;
- `market_ingest_raw_received_at_id_idx (received_at, id)`, añadido por
  `000005_retention_indexes.sql`.

Observaciones:

- el índice temporal permite seleccionar lotes vencidos sin escanear repetidamente toda la tabla;
- no tiene FK hacia `market_ingest_requests`, a diferencia del pipeline histórico;
- tampoco replica las restricciones de dominio presentes en las tablas de historial;
- el servicio de precios valida el request general, pero no establece un máximo
  explícito de entradas ni valida cada `location_id`, `item_key` y `quality`
  antes del `COPY`; hoy la confianza recae en el receiver autenticado;
- el índice por `request_id` no es opcional con la implementación actual: sostiene el upsert set-based inmediatamente posterior al `COPY`.

### `market_ingest_requests`

Propósito: idempotencia y estado de batches de precios.

Accesos reales:

- inserción por `request_id` con `on conflict do nothing`;
- lectura puntual por PK cuando se repite un request;
- actualización puntual al completar;
- limpieza por lotes de requests `completed`, antiguas y sin raw asociado.

Índices actuales:

- PK sobre `request_id`;
- `market_ingest_requests_created_at_idx (created_at desc)`.

El índice temporal no participa en consultas de la API, pero sostiene la
retención actual del ledger.

### `current_market_prices`

Propósito: modelo caliente y pequeño para precios vigentes.

Accesos reales:

- upsert por PK `(server, location_id, item_key, quality)`;
- consulta batch por `server`, lista de `location_id` y pares
  `(item_key, quality)`;
- orden por `location_id, item_key, quality`.

Índices actuales:

- PK `(server, location_id, item_key, quality)`;
- `current_market_prices_item_loc_idx
  (item_key, location_id, quality, server)`.

Evaluación preliminar:

- la PK coincide bien con el filtro por servidor/ubicación y con el orden de salida;
- el índice secundario parece candidato a redundante para las consultas actuales, pero **no debe eliminarse** sin revisar `pg_stat_user_indexes` y planes reales con `EXPLAIN (ANALYZE, BUFFERS)`;
- esta tabla no requiere retención por fecha: su cardinalidad está acotada por combinaciones lógicas de servidor, mercado, ítem y calidad.

### `market_history_ingest_raw`

Propósito: auditoría append-only de cada bucket histórico recibido.

Accesos reales:

- `COPY FROM` por batch;
- lectura por `request_id` dentro de la transacción para consolidar
  `market_history_buckets`;
- no participa en lecturas públicas.

Crecimiento: potencialmente el más rápido del sistema, porque una entrada puede
generar muchos buckets.

Índices actuales:

- PK sobre `id`;
- `market_history_ingest_raw_request_id_idx (request_id)`;
- `market_history_ingest_raw_received_at_id_idx (received_at, id)`, añadido por
  `000005_retention_indexes.sql`.

Restricciones relevantes:

- FK hacia `market_history_ingest_requests(request_id)` con `on delete restrict`;
- checks de servidor, ubicación, calidad, conteos y precio.

Consecuencia para retención: se deben eliminar primero filas raw y después el
ledger de requests, o utilizar una estrategia de particiones compatible con la
integridad referencial elegida.

### `market_history_ingest_requests`

Propósito: idempotencia y estado de batches históricos.

Índices actuales:

- PK sobre `request_id`;
- `market_history_ingest_requests_created_at_idx (created_at desc)`.

Crecimiento: una fila por batch aceptado, acotado por la retención de requests.

### `market_history_buckets`

Propósito: modelo de lectura histórica consolidado.

Accesos reales:

- upsert por PK;
- lectura por servidor, ubicaciones, pares ítem/calidad y rango de `bucket_at`;
- la API limita cada consulta a un máximo de 366 días.

Índices actuales:

- PK `(server, location_id, item_key, quality, bucket_at)`;
- `market_history_buckets_bucket_at_idx (bucket_at)`, usado para seleccionar
  lotes vencidos por horizonte histórico.

La PK está alineada con la consulta actual. Sin embargo, la tabla también crece
indefinidamente: el límite de 366 días restringe la consulta, no la persistencia.
La política debe distinguir entre:

- retención raw de auditoría;
- horizonte histórico útil para el producto;
- necesidad de conservar agregados de largo plazo.

## 4. Mapa entre consultas e índices

| Operación | Tabla | Predicado/conflicto | Índice esperado |
|---|---|---|---|
| Reducir batch de precios | `market_ingest_raw` | `request_id = $1` | `market_ingest_raw_request_id_idx` |
| Idempotencia de precios | `market_ingest_requests` | `request_id = $1` | PK |
| Upsert de precio actual | `current_market_prices` | conflicto por clave lógica | PK |
| Consulta pública de precios | `current_market_prices` | servidor + ubicaciones + pares ítem/calidad | PK; validar índice secundario |
| Reducir batch histórico | `market_history_ingest_raw` | `request_id = $1` | `market_history_ingest_raw_request_id_idx` |
| Idempotencia histórica | `market_history_ingest_requests` | `request_id = $1` | PK |
| Upsert de bucket | `market_history_buckets` | conflicto por clave lógica + bucket | PK |
| Consulta pública histórica | `market_history_buckets` | servidor + ubicaciones + pares + rango temporal | PK |
| Limpieza de ledgers | tablas `*_requests` | `created_at < límite` + `completed` + sin raw | índices `created_at desc` y `request_id` raw |
| Limpieza raw por lotes | tablas `*_raw` | `received_at < límite` | índices `(received_at, id)` de `000005` |

## 5. Volumen esperado

No existe telemetría persistente de tasa diaria en el repositorio. Por ello, el
diseño debe basarse en dos fuentes:

1. métricas reales capturadas con `scripts/audit-postgres.ps1`;
2. escenarios explícitos, sin asumir que el benchmark equivale a producción.

### Precios raw

El benchmark usa batches de 500 filas. La fórmula es:

```text
filas_raw_día = batches_día × filas_por_batch
filas_raw_retención = filas_raw_día × días_de_retención
```

Escenarios con 500 filas por batch:

| Frecuencia | Batches/día | Filas/día | 30 días | 90 días |
|---|---:|---:|---:|---:|
| cada 1 minuto | 1.440 | 720.000 | 21.600.000 | 64.800.000 |
| cada 5 minutos | 288 | 144.000 | 4.320.000 | 12.960.000 |
| cada 15 minutos | 96 | 48.000 | 1.440.000 | 4.320.000 |
| cada 1 hora | 24 | 12.000 | 360.000 | 1.080.000 |

El ingest de precios no tiene hoy un límite explícito de cantidad de entradas en
el servicio; solo lo acota el cuerpo HTTP de 5 MiB. Antes de fijar retención se
debe medir el percentil de filas por request y considerar un límite de dominio
coherente con el forwarder.

### Historial raw

Límites de servicio actuales:

- hasta 2.000 entradas por request;
- hasta 100.000 buckets totales por request;
- cuerpo HTTP máximo de 5 MiB, que puede imponer un límite efectivo menor.

Fórmula:

```text
filas_history_raw_día = requests_históricos_día × buckets_por_request
```

Escenarios ilustrativos:

| Escenario | Frecuencia | Buckets/request | Filas/día | 30 días |
|---|---:|---:|---:|---:|
| una captura pequeña | cada 5 min | 68 | 19.584 | 587.520 |
| 100 capturas de 68 buckets | cada 5 min | 6.800 | 1.958.400 | 58.752.000 |
| límite lógico máximo | cada 1 h | 100.000 | 2.400.000 | 72.000.000 |

Estos escenarios muestran que `market_history_ingest_raw` debe evaluarse antes
que la tabla raw de precios para particionamiento y almacenamiento.

### Tablas calientes

`current_market_prices` está acotada aproximadamente por:

```text
servidores × ubicaciones habilitadas × pares únicos (item_key, quality)
```

`market_history_buckets` no está acotada de la misma forma porque suma buckets
temporales. Su cardinalidad depende de:

```text
servidores × ubicaciones × pares item/calidad × buckets conservados
```

## 6. Restricciones técnicas para particionar los raw

Particionar por `received_at` sigue siendo la opción natural para expirar datos,
pero el esquema actual exige resolver antes estos puntos:

- PostgreSQL exige que una PK o restricción `UNIQUE` declarada en una tabla
  particionada incluya todas las columnas de la clave de partición. Las PK
  actuales usan solo `id`; una migración por `received_at` tendría que cambiar
  la clave a algo como `(received_at, id)`, o retirar la unicidad global de
  `id` manteniendo la secuencia como identificador técnico.
- Los índices de una tabla particionada son locales a cada partición. Una
  consulta solo por `request_id`, sin `received_at`, puede revisar todos los
  índices hijos porque no puede podar particiones por fecha.
- En el flujo actual, el ledger y las filas raw se crean dentro de la misma
  transacción y ambos usan `now()`. Esto permite estudiar una consulta futura
  que recupere también la fecha del request y use `request_id + received_at`
  para podar la partición correcta.
- Retener una cantidad acotada de particiones reduce el costo de esa búsqueda,
  pero no elimina la necesidad de medirla con el número de particiones esperado.
- La FK del raw histórico obliga a coordinar el descarte de particiones con la
  limpieza posterior del ledger de requests.

Por estas razones no se añade todavía una migración de particionamiento.

## 7. Datos que debe producir la auditoría contra PostgreSQL

El script recopila sin modificar datos:

- versión del servidor y configuración básica relevante;
- tamaño total, heap e índices por tabla;
- estimación de filas vivas y muertas;
- fechas de `ANALYZE`, `VACUUM` y autovacuum;
- columnas, defaults y nulabilidad;
- PK, FK y checks;
- definición, tamaño y uso acumulado de índices;
- fecha de reinicio de estadísticas, necesaria para interpretar contadores de
  uso iguales a cero;
- relación de particiones existente;
- posibles índices exactamente duplicados.

Con `-IncludeExactCounts` añade conteos y rangos temporales exactos. Esa opción
puede hacer escaneos completos y no debe ejecutarse en horario crítico sobre una
base grande.

## 8. Hipótesis que deben verificarse con una base real

1. La PK de `current_market_prices` cubre la consulta pública mejor o igual que
   `current_market_prices_item_loc_idx`.
2. La PK de `market_history_buckets` evita un sort costoso en rangos y lotes
   representativos.
3. Los índices por `request_id` mantienen bajo el costo de los CTE posteriores
   al `COPY` incluso cuando los raw tienen decenas de millones de filas.
4. Los índices temporales de los ledgers mantienen acotado el costo de la limpieza actual.
5. La tasa de tuplas muertas no exige ajustes de autovacuum en las tablas de
   upsert.
6. El tamaño real promedio de fila e índice permite elegir un horizonte de
   retención con margen operativo y de backup.

## 9. Resultado de la decisión de retención

La auditoría real confirmó un volumen todavía pequeño, estadísticas corregidas
con `VACUUM (ANALYZE)` y ausencia de una necesidad inmediata de particionamiento.
La política implementada queda documentada en `POSTGRES_RETENTION.md`:

- raw de precios e historial: 30 días;
- ledgers de requests: 90 días;
- buckets históricos: 400 días;
- `current_market_prices`: sin limpieza.

La implementación usa dry-run, lotes transaccionales, pausas configurables,
protección de requests `processing`, prueba contra base desechable y reportes
locales. El particionamiento permanece aplazado hasta que el volumen o el costo
de mantenimiento lo justifiquen.

## 10. Orden recomendado después de obtener el reporte

1. Aplicar `000005_retention_indexes.sql` en un horario de baja actividad.
2. Ejecutar la prueba contra una base desechable.
3. Revisar un dry-run sobre la base real.
4. Ejecutar la primera limpieza con lotes conservadores y revisar sus reportes.
5. Medir planes `EXPLAIN (ANALYZE, BUFFERS)`, duración de borrados y efecto de
   vacuum antes de reconsiderar particionamiento o índices.
6. Continuar con backup custom, checksum y restauración verificada.

## 11. Decisiones aún abiertas

- necesidad legal u operativa de auditoría de largo plazo;
- conservación o eliminación del índice secundario de precios;
- FK equivalente para el raw de precios;
- estrategia de particiones diaria, semanal o mensual si el volumen futuro la exige;
- runner formal de migraciones y tabla de versión de esquema.
