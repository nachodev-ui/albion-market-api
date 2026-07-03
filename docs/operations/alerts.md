# Alertas y respuesta

Las reglas viven en `observability/prometheus/rules/albion-market-api.rules.yml`. Los umbrales son iniciales para el entorno local y deben ajustarse con datos reales antes de usarlos como política de guardia.

| Alerta | Severidad | Condición inicial | Primera respuesta |
|---|---|---|---|
| `AlbionMarketAPIUnavailable` | crítica | target sin scrape durante 1 min | revisar contenedor, puerto, logs y red |
| `AlbionMarketAPINotReady` | crítica | readiness en 0 durante 2 min | consultar `/readyz`, PostgreSQL, pool y migraciones |
| `AlbionMarketAPIRepeatedRestarts` | advertencia | al menos 2 reinicios en 30 min | revisar OOM, señales, healthcheck y logs previos |
| `AlbionMarketAPIHighHTTP5xxRate` | crítica | 5xx > 5% con tráfico durante 5 min | identificar ruta y correlacionar con errores de DB |
| `AlbionMarketAPIHighHTTPLatency` | advertencia | p95 > 1 s durante 10 min | revisar rutas lentas, pool y operaciones PostgreSQL |
| `AlbionMarketAPIAuthenticationFailuresHigh` | advertencia | 10 respuestas 401/403 en 10 min | revisar `auth_key_id`, reloj y configuración del forwarder |
| `AlbionMarketAPIIngestTrafficStopped` | advertencia | sin solicitudes de ingesta por 15 min | revisar Data Client, receptor y forwarder |
| `AlbionMarketAPINoSuccessfulIngest` | crítica | sin ingesta exitosa por 30 min | comparar último request, errores y disponibilidad de DB |
| `AlbionMarketAPIIngestErrorsRepeated` | advertencia | 3 batches fallidos en 10 min | revisar validación, payload y logs correlacionados |
| `AlbionMarketAPIIngestPersistenceErrorsRepeated` | crítica | 3 errores CopyFrom/upsert en 10 min | revisar locks, espacio, conexiones y errores SQL |
| `AlbionMarketAPIDatabasePoolSaturated` | crítica | utilización > 90% durante 5 min | revisar consultas lentas y conexiones retenidas |
| `AlbionMarketAPIDatabaseAcquireSlow` | advertencia | adquisición > 250 ms durante 5 min | revisar saturación, latencia y límites del pool |

## Flujo común

1. Confirma la alerta en Prometheus y Alertmanager.
2. Revisa el dashboard `Albion Market API · Overview` en Grafana.
3. Consulta `/healthz`, `/readyz` y `/metrics` directamente.
4. Correlaciona la ventana temporal con logs JSON por `correlation_id` y `auth_key_id`.
5. No reinicies la API automáticamente cuando solo falle readiness por una interrupción breve de PostgreSQL.
6. Registra causa, acción aplicada y momento de recuperación antes de cerrar la alerta.

## Silencios

Los silencios se crean en la UI local de Alertmanager. Deben tener duración acotada y un comentario que indique mantenimiento, responsable y hora esperada de finalización. No silencies `AlbionMarketAPIUnavailable` o `AlbionMarketAPINotReady` de forma indefinida.

## AlbionMarketAPIUnavailable

1. Comprueba `docker compose ps api` y los logs del servicio.
2. Prueba `/healthz` desde el host.
3. Revisa que Prometheus resuelva `api:8080` en la red `observability`.
4. Escala como incidente crítico si la API no vuelve después de corregir red o proceso.

## AlbionMarketAPINotReady

1. Consulta `/readyz` y `albion_market_api_readiness_failures_total` por componente.
2. Comprueba salud de PostgreSQL, disponibilidad del pool y migración `000006`.
3. Mantén el proceso vivo mientras se resuelve una interrupción breve de PostgreSQL.

## AlbionMarketAPIRepeatedRestarts

1. Revisa `docker inspect` para `OOMKilled`, código de salida y contador de reinicios.
2. Correlaciona los reinicios con despliegues, señales o fallos de healthcheck.
3. Conserva los logs del proceso anterior antes de recrear el contenedor.

## AlbionMarketAPIHighHTTP5xxRate

1. Separa la tasa por `route` y revisa logs `http.request_completed` del mismo intervalo.
2. Compara con `database_errors_total` y readiness.
3. Si una ruta concreta concentra los fallos, reduce tráfico o deshabilita el consumidor afectado.

## AlbionMarketAPIHighHTTPLatency

1. Compara p95 HTTP con duración de consultas, CopyFrom y upsert.
2. Revisa utilización del pool y adquisición de conexiones.
3. Identifica si la latencia pertenece a lectura, ingesta o a una dependencia degradada.

## AlbionMarketAPIAuthenticationFailuresHigh

1. Comprueba el `auth_key_id` registrado, nunca el token.
2. Verifica que receiver y API usan la misma credencial activa.
3. Revisa HTTPS, cabeceras reenviadas y desfase de despliegue antes de rotar credenciales.

## AlbionMarketAPIIngestTrafficStopped

1. Revisa Albion Data Client, receiver local y forwarder en ese orden.
2. Confirma cola, reintentos y último envío del forwarder.
3. Comprueba conectividad hacia la API antes de reiniciar componentes.

## AlbionMarketAPINoSuccessfulIngest

1. Compara el último request con el último success.
2. Si existen requests recientes, revisa errores de validación y persistencia.
3. Si tampoco existen requests, sigue el runbook de tráfico detenido.

## AlbionMarketAPIIngestErrorsRepeated

1. Revisa los eventos de ingesta por `correlation_id`.
2. Distingue rechazo de payload, conflicto idempotente y error interno.
3. Conserva una muestra redactada del payload problemático para reproducir el fallo.

## AlbionMarketAPIIngestPersistenceErrorsRepeated

1. Separa `copy_raw_prices`, `copy_raw_history`, `upsert_current_prices` y `upsert_market_history`.
2. Comprueba espacio, locks, transacciones abiertas y disponibilidad de conexiones.
3. No omitas la tabla cruda de auditoría para recuperar rendimiento.

## AlbionMarketAPIDatabasePoolSaturated

1. Revisa conexiones adquiridas, inactivas, máximo configurado y adquisición promedio.
2. Identifica operaciones lentas o conexiones retenidas.
3. Aumenta el pool solo después de verificar la capacidad real de PostgreSQL.

## AlbionMarketAPIDatabaseAcquireSlow

1. Compara latencia de adquisición con utilización del pool.
2. Revisa espera por conexiones, red y creación de conexiones nuevas.
3. Si el pool no está saturado, investiga latencia o degradación de PostgreSQL.
