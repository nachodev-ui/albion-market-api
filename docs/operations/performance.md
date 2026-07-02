# Rendimiento de ingesta

Esta versión mantiene el flujo y los contratos existentes:

`Albion Data Client -> receiver local -> forwarder -> albion-market-api -> PostgreSQL`

## Cambios aplicados

- `market_ingest_raw` sigue siendo la auditoría durable e inmutable.
- La carga cruda usa `pgx.CopyFrom` dentro de la misma transacción de idempotencia.
- El `CopyFromSource` reutiliza su buffer de valores y no construye un `[][]any` completo.
- El upsert de `current_market_prices` procesa el batch por conjuntos con `DISTINCT ON`.
- Se eliminaron las cuatro subconsultas correlacionadas por clave del upsert anterior.
- Los datos viejos o idénticos ya no generan una actualización física de la fila caliente.
- El hash del request se calcula antes de abrir la transacción para reducir el tiempo de ocupación de una conexión.
- La migración `000003_raw_audit_write_path.sql` elimina el índice de lectura amplia de la tabla cruda, que no utiliza la API.

## Aplicación

Ejecutar las migraciones en orden, incluyendo:

```text
migrations/000003_raw_audit_write_path.sql
```

Luego compilar y probar:

```powershell
go test ./...
go build ./cmd/api
```

No se requieren cambios en `albion-market-data-platform`: el payload, la autenticación, los reintentos y el tamaño de batch del forwarder siguen siendo compatibles.

## Historial central

La migración `000004_market_history.sql` aplica el mismo patrón de rendimiento
al historial:

- `market_history_ingest_raw` conserva la auditoría append-only;
- `pgx.CopyFrom` escribe los buckets sin construir un buffer plano completo;
- el `CopyFromSource` recorre directamente las entradas anidadas;
- `market_history_buckets` funciona como modelo caliente de lectura;
- el upsert reduce cada request por clave lógica antes de tocar la tabla
  caliente;
- solamente una captura más nueva, o una corrección con el mismo
  `observed_at`, genera una actualización;
- la consulta batch usa arrays y `unnest` para resolver varios pares
  objeto/calidad en una llamada.

Antes de iniciar esta versión debe aplicarse:

```text
migrations/000004_market_history.sql
```
