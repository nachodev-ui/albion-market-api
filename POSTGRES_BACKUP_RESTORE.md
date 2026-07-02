# Backups y restauración comprobada de PostgreSQL

Esta etapa añade backups lógicos automáticos de `albion_market`, checksum SHA256,
retención de archivos y una restauración real en una base desechable. No sustituye
un sistema de recuperación punto en el tiempo con WAL: el alcance es recuperar el
esquema y los datos contenidos en cada archivo generado por `pg_dump`.

## Política inicial

- Formato: custom de PostgreSQL (`pg_dump --format=custom`).
- Frecuencia recomendada: una vez al día, después de la tarea de retención.
- Retención de archivos: 30 días.
- Mínimo protegido: los 7 backups completos más recientes, aunque excedan los
  30 días.
- Integridad: SHA256 por archivo.
- Verificación recomendada: restauración desechable semanal.
- Credenciales: se leen desde `DATABASE_URL`/`.env.local`; no se escriben en
  nombres, manifiestos, reportes ni argumentos visibles del proceso.

La retención de archivos solo se ejecuta después de crear y validar un backup
nuevo. Un conjunto se considera completo únicamente cuando existe su manifiesto
con estado `completed`.

## Archivos generados

Cada ejecución correcta crea un conjunto con el mismo prefijo:

```text
albion_market-20260702T120000000Z-1234.dump
albion_market-20260702T120000000Z-1234.dump.sha256
albion_market-20260702T120000000Z-1234.toc.txt
albion_market-20260702T120000000Z-1234.manifest.json
```

- `.dump`: archivo custom restaurable con `pg_restore`.
- `.dump.sha256`: checksum estándar del archivo.
- `.toc.txt`: listado legible del contenido del archive.
- `.manifest.json`: versiones, tamaño, hash, conteos y comprobaciones tomadas
  antes y después del dump.

Los archivos se escriben primero con sufijo `.partial` y solo se renombran al
final. Los parciales con más de 24 horas se eliminan durante la siguiente
limpieza.

## Crear un backup manual

Desde PowerShell, en la raíz del repositorio:

```powershell
.\scripts\postgres-backup.ps1
```

La salida predeterminada es:

```text
artifacts/postgres-backups/
```

Para una ubicación externa y una política explícita:

```powershell
.\scripts\postgres-backup.ps1 `
  -BackupDirectory "D:\AlbionBackups\PostgreSQL" `
  -RetentionDays 30 `
  -MinimumBackups 7
```

Para crear el backup sin borrar conjuntos antiguos:

```powershell
.\scripts\postgres-backup.ps1 -SkipRetentionCleanup
```

La conexión se resuelve en este orden:

1. `-DatabaseUrl`;
2. variable de entorno `DATABASE_URL`;
3. `DATABASE_URL` dentro de `.env.local`.

El script exige `psql`, `pg_dump` y `pg_restore` en `PATH`. También comprueba que
la versión principal de `pg_dump` no sea más antigua que la del servidor.

## Consistencia y conteos

`pg_dump` crea un snapshot consistente del contenido que respalda. Para poder
comparar la restauración con el origen, el script toma un resumen justo antes y
justo después del dump:

- conteo exacto de las seis tablas;
- cantidad de tablas, PK, FK e índices esperados;
- requests `completed` y `processing`;
- consulta representativa de `current_market_prices`;
- consulta representativa de `market_history_buckets`;
- validez de las secuencias de ambas tablas raw.

Si ambos resúmenes coinciden, el manifiesto marca
`source_stable_during_backup=true` y la restauración exige igualdad exacta de
conteos y consultas. Si hubo escrituras concurrentes, el backup sigue siendo
válido, pero la verificación omite la igualdad exacta contra el origen y mantiene
las validaciones de archive, esquema, secuencias y consultas.

## Restaurar y comprobar en una base desechable

Usa un backup concreto:

```powershell
.\scripts\postgres-restore-verify.ps1 `
  -BackupPath ".\artifacts\postgres-backups\albion_market-AAAAMMDDTHHMMSSfffZ-PID.dump"
```

La cuenta debe poder ejecutar `CREATE DATABASE` y `DROP DATABASE`. El script:

1. verifica que existan el dump, checksum y manifiesto;
2. recalcula SHA256;
3. comprueba que `pg_restore --list` pueda leer el archive;
4. crea una base `albion_market_restore_verify_*` desde `template0`;
5. restaura en una sola transacción y detiene el proceso ante el primer error;
6. aplica las migraciones del repositorio, salvo que se use `-SkipMigrations`;
7. valida tablas, PK/FK, índices, secuencias, conteos y consultas principales;
8. guarda un reporte JSON;
9. elimina la base desechable.

Para conservarla después de un fallo:

```powershell
.\scripts\postgres-restore-verify.ps1 `
  -BackupPath $backup `
  -KeepDatabaseOnFailure
```

`-KeepDatabaseOnSuccess` existe solo para inspección manual. No debe usarse en
la tarea programada porque acumularía bases temporales.

Los reportes quedan en:

```text
artifacts/postgres-restore-verification/
```

## Prueba integral sin producción

La prueba crea una base fuente desechable, aplica migraciones, carga fixtures,
genera tres backups, comprueba la retención 30 días/mínimo 2, restaura el más
nuevo en otra base temporal, valida los conteos y confirma que un checksum
alterado sea rechazado.

```powershell
.\scripts\test-postgres-backup-restore.ps1
```

La cuenta necesita `CREATE DATABASE` y `DROP DATABASE`. Para conservar la base
fuente cuando falle:

```powershell
.\scripts\test-postgres-backup-restore.ps1 -KeepDatabaseOnFailure
```

Nunca apuntes manualmente la restauración a una base que quieras conservar. El
script de verificación siempre genera su propio nombre aleatorio.

## Programador de tareas de Windows

### Preparación

1. Ejecuta `test-postgres-backup-restore.ps1`.
2. Crea una carpeta fuera del repositorio, por ejemplo
   `D:\AlbionBackups\PostgreSQL`.
3. Restringe esa carpeta y `.env.local` al usuario de la tarea.
4. No escribas `DATABASE_URL` en los argumentos del Programador de tareas.
5. Usa una cuenta PostgreSQL de backup con permisos de lectura sobre todos los
   objetos. La verificación semanal requiere además una cuenta administrativa
   capaz de crear y eliminar bases desechables.
6. Desbloquea solo los scripts descargados:

```powershell
Get-ChildItem .\scripts -Filter *.ps1 | Unblock-File
```

### Tarea diaria de backup

Configuración sugerida:

- Nombre: `Albion PostgreSQL Backup`.
- Frecuencia: diaria, después de la retención PostgreSQL.
- Ejecutar aunque el usuario no haya iniciado sesión.
- No iniciar otra instancia si ya existe una ejecución.
- Detener la tarea si supera 2 horas.
- Reintentar cada 15 minutos, hasta 3 veces.

Programa:

```text
powershell.exe
```

Argumentos:

```text
-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "C:\Users\mitsf\Desktop\albion-market-api\scripts\postgres-backup.ps1" -BackupDirectory "D:\AlbionBackups\PostgreSQL" -RetentionDays 30 -MinimumBackups 7
```

Iniciar en:

```text
C:\Users\mitsf\Desktop\albion-market-api
```

No añadas `-DatabaseUrl` a la tarea. El script cargará `.env.local`.

### Tarea semanal de restauración comprobada

Crea una segunda tarea después del backup semanal. El script incluido selecciona
el conjunto completo más reciente a partir de su manifiesto y ejecuta toda la
restauración desechable:

```powershell
.\scripts\verify-latest-postgres-backup.ps1 `
  -BackupDirectory "D:\AlbionBackups\PostgreSQL"
```

Programa:

```text
powershell.exe
```

Argumentos:

```text
-NoProfile -NonInteractive -ExecutionPolicy Bypass -File "C:\Users\mitsf\Desktop\albion-market-api\scripts\verify-latest-postgres-backup.ps1" -BackupDirectory "D:\AlbionBackups\PostgreSQL"
```

La tarea debe fallar si no existe un conjunto completo, si no coincide el SHA256,
si el archive no puede leerse, si la restauración falla o si alguna validación no
coincide. No añadas la URL administrativa a los argumentos; usa
`POSTGRES_ADMIN_URL` o `.env.local` bajo la cuenta de la tarea.

## Recuperación real

Antes de una recuperación real:

1. conserva una copia adicional del conjunto `.dump`, `.sha256` y
   `.manifest.json`;
2. ejecuta `postgres-restore-verify.ps1` sobre ese mismo archivo;
3. detén temporalmente la escritura del receiver/API;
4. restaura en una base nueva, nunca encima de la dañada;
5. cambia la aplicación a la base recuperada solo después de validar conteos y
   consultas;
6. conserva la base anterior hasta terminar la revisión.

Este backup es por base de datos. No incluye roles globales, contraseñas de roles,
tablespaces del clúster ni recuperación punto en el tiempo. Esos elementos se
cubrirán en el despliegue reproducible y la estrategia operativa del servidor.

## Transporte seguro de la conexión a las herramientas cliente

Los scripts no colocan la contraseña en los argumentos visibles de `psql`,
`pg_dump` ni `pg_restore`. La URL se entrega mediante `--dbname` después de
retirar su contraseña, y la credencial se proporciona a libpq mediante un
archivo `PGPASSFILE` temporal que se elimina al terminar cada proceso.

No se debe asignar una URL completa a `PGDATABASE`: esa variable representa el
nombre de la base y no sustituye de forma fiable a `--dbname` para una URI de
conexión completa.
