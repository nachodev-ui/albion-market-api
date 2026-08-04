# Lemon Squeezy: operación de producción

## Objetivo

La integración usa un flujo de confianza cero:

1. el endpoint acepta exclusivamente `POST application/json`;
2. verifica `X-Signature` mediante HMAC-SHA256 sobre el cuerpo original;
3. exige que `X-Event-Name` coincida con `meta.event_name`;
4. valida store, modo, tipo de recurso y variante esperada;
5. elimina PII antes de persistir el payload;
6. registra el evento de forma idempotente en PostgreSQL;
7. responde `200 OK` después del commit de la cola;
8. un worker separado procesa el evento con bloqueo `FOR UPDATE SKIP LOCKED`, lease, backoff y dead-letter.

No se almacena el cuerpo crudo. `provider_event_id` es el SHA-256 determinístico del cuerpo firmado, porque Lemon Squeezy no entrega un identificador único de entrega en los headers documentados.

## Eventos habilitados

- `order_created`
- `subscription_created`
- `subscription_updated`
- `subscription_cancelled`
- `subscription_resumed`
- `subscription_expired`
- `subscription_paused`
- `subscription_unpaused`
- `subscription_payment_failed`
- `subscription_payment_recovered`
- `subscription_payment_success`

`order_created` crea un acceso provisional máximo de 15 minutos. `subscription_created` reemplaza ese registro por la suscripción real. La medida evita latencia visible y no permite acceso indefinido si el segundo webhook no llega.

Los eventos de pago crean entradas idempotentes en `billing_notification_outbox`. El transporte de correo o notificaciones debe consumir esa outbox; el webhook nunca llama directamente a un proveedor de correo.

## Variables obligatorias

```text
APP_ENV=production
BILLING_ENABLED=true
BILLING_PROVIDER=lemonsqueezy
BILLING_TEST_MODE=false
BILLING_CHECKOUT_REDIRECT_URL=https://albioncalculator.app/account?checkout=success
BILLING_GRACE_PERIOD=168h
BILLING_HTTP_TIMEOUT=10s
BILLING_MAX_WEBHOOK_BODY_BYTES=1048576
BILLING_WEBHOOK_INGEST_TIMEOUT=1500ms
BILLING_WEBHOOK_WORKER_POLL_INTERVAL=250ms
BILLING_WEBHOOK_JOB_TIMEOUT=15s
BILLING_WEBHOOK_LEASE_DURATION=60s
BILLING_WEBHOOK_BASE_RETRY_DELAY=5s
BILLING_WEBHOOK_MAX_RETRY_DELAY=15m
BILLING_WEBHOOK_BATCH_SIZE=10
BILLING_WEBHOOK_MAX_ATTEMPTS=8
LEMONSQUEEZY_API_BASE_URL=https://api.lemonsqueezy.com
LEMONSQUEEZY_STORE_ID=<live-store-id>
LEMONSQUEEZY_PRO_VARIANT_ID=<live-variant-id>
LEMONSQUEEZY_API_KEY=<live-api-key>
LEMONSQUEEZY_WEBHOOK_SECRET=<32+ character live signing secret>
```

En contenedores propios se recomienda usar `LEMONSQUEEZY_API_KEY_FILE` y `LEMONSQUEEZY_WEBHOOK_SECRET_FILE`. Nunca definir secretos como variables `VITE_*`, archivos versionados o valores incluidos en Cloudflare Pages.

## Migración

Aplicar `migrations/0018_billing_webhook_queue.sql` antes del despliegue. La API exige esquema 18 y las relaciones:

- `billing_webhook_events`
- `billing_orders`
- `billing_notification_outbox`

Comprobación:

```sql
select version from app_schema_state where singleton = true;

select to_regclass('public.billing_orders'),
       to_regclass('public.billing_notification_outbox');
```

## Configuración del webhook live

Endpoint:

```text
https://<api-production>/api/v1/webhooks/lemonsqueezy
```

Seleccionar todos los eventos listados en este documento. Usar un signing secret exclusivo de producción y mantener separado el store de prueba. No activar `BILLING_ENABLED` hasta que la migración, los secretos y el webhook live estén configurados.

## Validación E2E

1. usuario Free autenticado;
2. creación de checkout live con una compra controlada;
3. `order_created` en estado `processed`;
4. `subscription_created` en estado `processed`;
5. plan efectivo `pro` en `/api/v1/me/entitlements`;
6. acceso al Customer Portal;
7. reenvío del mismo webhook con `duplicate=true` y sin una segunda mutación;
8. cambio de variante y revocación del plan Pro;
9. cancelación con acceso hasta `ends_at`;
10. pago fallido con estado `past_due` y período de gracia;
11. recuperación de pago con estado `active`;
12. eventos de notificación presentes una sola vez en la outbox.

## Consultas operativas

```sql
select provider_event_id, event_type, status, attempt_count,
       delivery_count, next_attempt_at, last_attempt_at,
       processed_at, dead_letter_at, error_message
from billing_webhook_events
order by last_received_at desc
limit 100;
```

```sql
select provider_subscription_id, provider_variant_id,
       provider_status, plan_code, status,
       current_period_end, access_until, updated_at
from subscriptions
where provider = 'lemonsqueezy'
order by updated_at desc
limit 100;
```

```sql
select notification_type, status, attempt_count,
       next_attempt_at, sent_at, error_code
from billing_notification_outbox
order by created_at desc
limit 100;
```

## Alertas mínimas

- cualquier evento en `dead_letter`;
- más de cinco eventos `failed` durante cinco minutos;
- evento `processing` con `locked_at` anterior al lease;
- p95 de recepción del webhook superior a 1 segundo;
- crecimiento sostenido de `billing_notification_outbox` pendiente;
- discrepancia entre suscripciones activas del proveedor y registros locales.

## Rollback

1. definir `BILLING_ENABLED=false` y desplegar;
2. definir `VITE_BILLING_ENABLED=false` en Cloudflare Pages;
3. no borrar suscripciones, órdenes, eventos ni outbox;
4. resolver o exportar dead letters;
5. restaurar el servicio con la misma clave de idempotencia.

Desactivar billing impide nuevos checkouts y webhooks, pero conserva el estado necesario para reconciliación y auditoría.
