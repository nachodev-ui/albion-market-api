# Retención segura de PostgreSQL

Esta etapa limita el crecimiento de las tablas de auditoría y del historial sin
modificar la tabla caliente `current_market_prices` ni introducir todavía
particionamiento.

## Política inicial

| Tabla | Columna temporal | Retención |
|---|---|---:|
| `market_history_ingest_raw` | `received_at` | 30 días |
| `market_ingest_raw` | `received_at` | 30 días |
| `market_history_ingest_requests` | `created_at` | 90 días |
| `market_ingest_requests` | `created_at` | 90 días |
| `market_history_buckets` | `bucket_at` | 400 días |
| `current_market_prices` | — | sin limpieza |

La limpieza usa una hora de referencia única al comienzo de cada ejecución. Una
fila se considera vencida solo cuando su fecha es estrictamente anterior al
corte. Por ello, un bucket que tenga exactamente 400 días se conserva.

## Orden y protecciones

El script procesa siempre las tablas en este orden:

1. `market_history_ingest_raw`.
2. `market_ingest_raw`.
3. `market_history_ingest_requests` huérfanas.
4. `market_ingest_requests` huérfanas.
5. `market_history_buckets`.

Las solicitudes se eliminan únicamente cuando cumplen simultáneamente estas
condiciones:

- son anteriores al límite configurado;
- tienen estado `completed`;
- ya no poseen filas raw asociadas.

Las solicitudes `processing` nunca se eliminan. Las filas raw asociadas a una
solicitud `processing` también se conservan para facilitar la investigación de
un procesamiento atascado. Deben revisarse manualmente en vez de forzar su
borrado.

Cada lote se ejecuta en su propia transacción y utiliza `FOR UPDATE SKIP LOCKED`.
Así se evitan transacciones largas y se reduce la interferencia con la ingesta.
El corte queda fijo durante toda la ejecución, por lo que datos que todavía no
habían vencido al comenzar no entran en la misma limpieza.

## Índices requeridos

Antes de usar el modo de aplicación, ejecuta la migración idempotente. El
wrapper carga la conexión con las mismas reglas que los demás scripts y evita
copiar credenciales a la consola:

```powershell
.\scripts\apply-retention-indexes.ps1
```

También acepta una conexión explícita mediante `-DatabaseUrl`.

La migración añade:

- `(received_at, id)` en ambas tablas raw;
- `(bucket_at)` en `market_history_buckets`.

Los índices `request_id` y `created_at` existentes se mantienen porque son
necesarios para comprobar orfandad y antigüedad.

El script ejecuta un preflight antes de contar o borrar y se detiene si falta
alguna tabla o cualquiera de estos tres índices.

## Primera ejecución: dry-run

El modo predeterminado no elimina nada:

```powershell
.\scripts\postgres-retention.ps1
```

También puede indicarse explícitamente:

```powershell
.\scripts\postgres-retention.ps1 -Mode DryRun
```

En dry-run, el cálculo de solicitudes simula primero la futura eliminación raw.
Así, el resumen muestra cuántas solicitudes quedarían huérfanas al respetar el
orden real, aunque la base no sea modificada.

El script busca la conexión en este orden:

1. parámetro `-DatabaseUrl`;
2. variable de entorno `DATABASE_URL`;
3. `DATABASE_URL` dentro de `.env.local`.

La URL no se imprime ni se almacena en los reportes.

## Aplicar la limpieza

Después de revisar el dry-run:

```powershell
.\scripts\postgres-retention.ps1 -Mode Apply
```

Configuración habitual:

```powershell
.\scripts\postgres-retention.ps1 `
  -Mode Apply `
  -BatchSize 5000 `
  -PauseMilliseconds 100 `
  -LockTimeoutSeconds 5 `
  -StatementTimeoutSeconds 120
```

Parámetros de retención configurables:

```powershell
.\scripts\postgres-retention.ps1 `
  -Mode Apply `
  -MarketHistoryRawRetentionDays 30 `
  -MarketRawRetentionDays 30 `
  -MarketHistoryRequestsRetentionDays 90 `
  -MarketRequestsRetentionDays 90 `
  -MarketHistoryBucketsRetentionDays 400
```

Protecciones de configuración:

- la retención de requests no puede ser menor que la retención raw
  correspondiente;
- `market_history_buckets` no acepta menos de 366 días;
- el tamaño máximo admitido por lote es 100000;
- `-MaxBatchesPerTable` puede limitar una ejecución de prueba; `0` significa
  sin límite.

Ejemplo conservador para la primera aplicación real:

```powershell
.\scripts\postgres-retention.ps1 `
  -Mode Apply `
  -BatchSize 1000 `
  -PauseMilliseconds 250 `
  -MaxBatchesPerTable 5
```

Si el resumen termina con `batch-limit-reached`, vuelve a ejecutar el script
hasta que `EligibleAfter` sea cero. `eligible-rows-remain` normalmente indica
filas bloqueadas por otra transacción o actividad concurrente; no debe
solucionarse aumentando agresivamente los timeouts.

## Reportes

Cada ejecución crea archivos sin credenciales en:

```text
artifacts/postgres-retention/
```

Se generan:

- `.log`: progreso por lote y resumen legible;
- `.csv`: resumen tabular;
- `.json`: configuración efectiva y resumen estructurado.

Los campos principales son:

- `EligibleBefore`: filas vencidas encontradas;
- `Deleted`: filas eliminadas;
- `EligibleAfter`: filas que aún cumplen el criterio;
- `Batches`: transacciones de borrado ejecutadas;
- `Status`: resultado de la tabla.

## Prueba contra una base desechable

La prueba crea una base temporal con nombre aleatorio, aplica todas las
migraciones, carga fixtures deterministas, ejecuta dry-run y aplicación con
lotes de una fila, valida las protecciones y elimina la base al terminar.

La cuenta de PostgreSQL debe tener permisos `CREATE DATABASE` y `DROP DATABASE`:

```powershell
.\scripts\test-postgres-retention.ps1
```

Puede usarse una URL administrativa explícita:

```powershell
.\scripts\test-postgres-retention.ps1 `
  -AdminDatabaseUrl $env:POSTGRES_ADMIN_URL
```

Para conservar la base temporal después de un fallo:

```powershell
.\scripts\test-postgres-retention.ps1 -KeepDatabaseOnFailure
```

La prueba confirma, entre otros puntos, que:

- dry-run no modifica ninguna tabla;
- se usan varios lotes pequeños;
- las requests recientes permanecen;
- las requests `processing` y sus raw permanecen;
- solo se eliminan requests viejas y huérfanas;
- un bucket de 401 días se elimina y uno de exactamente 400 días permanece;
- `current_market_prices` no cambia.

Nunca apuntes esta prueba directamente a una base que quieras conservar. El
script siempre crea otra base y la elimina, pero la URL administrativa debe ser
revisada antes de ejecutarlo.

## Programador de tareas de Windows

### Preparación

1. Ejecuta `apply-retention-indexes.ps1` y la prueba desechable.
2. Ejecuta un dry-run contra la base real y revisa los reportes.
3. Restringe el acceso de `.env.local` al usuario que ejecutará la tarea.
4. Usa una cuenta de PostgreSQL con los permisos mínimos necesarios sobre estas
   cinco tablas; no uses la contraseña en los argumentos de la tarea.
5. Desbloquea únicamente el script cuando Windows lo marque como descargado:

```powershell
Unblock-File .\scripts\postgres-retention.ps1
```

### Crear la tarea desde la interfaz

En **Programador de tareas → Crear tarea**:

- **General**
  - nombre: `Albion Market PostgreSQL Retention`;
  - ejecutar aunque el usuario no haya iniciado sesión;
  - ejecutar con la cuenta que puede leer `.env.local`;
  - no habilitar privilegios elevados salvo que sean realmente necesarios.
- **Desencadenadores**
  - diariamente, por ejemplo a las `03:30`;
  - habilitar reintento si el equipo estaba apagado.
- **Acciones**
  - programa:

    ```text
    powershell.exe
    ```

  - argumentos:

    ```text
    -NoProfile -NonInteractive -ExecutionPolicy Bypass -File "C:\ruta\albion-market-api\scripts\postgres-retention.ps1" -Mode Apply -BatchSize 5000 -PauseMilliseconds 100
    ```

  - iniciar en:

    ```text
    C:\ruta\albion-market-api
    ```

- **Condiciones y configuración**
  - no iniciar otra instancia si la tarea ya está ejecutándose;
  - detenerla si supera una hora;
  - reintentar cada 15 minutos hasta 3 veces;
  - conservar el historial de ejecución.

`ExecutionPolicy Bypass` se limita al proceso creado por la tarea. No cambia la
política global del sistema.

### Validación de la tarea

Ejecuta la tarea manualmente una vez y confirma:

1. código de resultado `0x0`;
2. aparición de nuevos archivos en `artifacts/postgres-retention`;
3. `EligibleAfter = 0` o una explicación conocida para las filas restantes;
4. funcionamiento normal de la ingesta y de las consultas públicas.

Durante las primeras semanas conviene revisar diariamente el `.csv` y mantener
un `BatchSize` conservador. El particionamiento se reevaluará solo si el volumen,
los tiempos de borrado o el vacuum empiezan a justificar su complejidad.
