# Activación de facturación en sandbox

## Estado seguro inicial

La API se entrega con:

```text
BILLING_ENABLED=false
BILLING_PROVIDER=lemonsqueezy
BILLING_TEST_MODE=true
```

Mientras `BILLING_ENABLED` permanezca en `false`, las rutas de checkout, portal y webhook no se registran y el mercado continúa operando sin depender del proveedor de pago.

## Orden de activación

### 1. Promover el esquema PostgreSQL

Aplicar `migrations/0009_billing_provider_metadata.sql` en la rama de producción de Neon.

Comprobar:

```sql
select version
from public.app_schema_state
where singleton = true;
```

El resultado debe ser `9`.

Verificar las columnas:

```sql
select table_name, column_name
from information_schema.columns
where table_schema = 'public'
  and table_name in ('subscriptions', 'billing_webhook_events')
  and column_name in (
    'provider_variant_id',
    'provider_status',
    'provider_updated_at',
    'attempt_count',
    'last_attempt_at'
  )
order by table_name, column_name;
```

### 2. Crear el producto en Lemon Squeezy Test mode

Crear:

- producto: `Albion Production Calculator Pro`;
- variante: suscripción mensual;
- precio inicial: `USD 4.99`;
- período de prueba: deshabilitado inicialmente;
- modo de prueba: activo.

Registrar el `Store ID` y el `Variant ID` de la variante mensual.

### 3. Crear credenciales de prueba

Generar:

- API key de Lemon Squeezy Test mode;
- signing secret para el webhook.

No guardar estos valores en GitHub, archivos `.env` versionados ni variables `VITE_*`.

### 4. Configurar el webhook

Endpoint:

```text
https://<host-render>/api/v1/webhooks/lemonsqueezy
```

Eventos de suscripción admitidos por la API:

```text
subscription_created
subscription_updated
subscription_cancelled
subscription_resumed
subscription_expired
subscription_paused
subscription_unpaused
```

El webhook valida `X-Signature` con HMAC-SHA256 sobre el cuerpo original, limita el tamaño del payload y deduplica por hash SHA-256.

### 5. Configurar Render

Agregar como secretos o variables externas:

```text
LEMONSQUEEZY_STORE_ID=<store-id>
LEMONSQUEEZY_PRO_VARIANT_ID=<variant-id>
LEMONSQUEEZY_API_KEY=<test-api-key>
LEMONSQUEEZY_WEBHOOK_SECRET=<signing-secret>
```

Mantener:

```text
BILLING_TEST_MODE=true
BILLING_CHECKOUT_REDIRECT_URL=https://albion-production-calculator.pages.dev/account?checkout=success
BILLING_GRACE_PERIOD=168h
```

Solo después de validar todos los valores, cambiar:

```text
BILLING_ENABLED=true
```

### 6. Desplegar la API

La sonda `/readyz` exige esquema 9. Si Neon todavía está en esquema 8, el despliegue no debe recibir tráfico.

Validar después del despliegue:

```text
GET /healthz -> 200
GET /readyz -> 200
GET /api/v1/status -> 200
```

### 7. Habilitar el frontend

En Cloudflare Pages, configurar:

```text
VITE_BILLING_ENABLED=true
```

La variable es una bandera pública; no es una credencial. Ningún secreto de Lemon Squeezy debe llegar al bundle del navegador.

### 8. Prueba end-to-end

1. iniciar sesión con una cuenta Free;
2. abrir `/plans`;
3. crear un checkout Pro de prueba;
4. completar el pago con datos de sandbox;
5. confirmar respuesta `200` del webhook;
6. comprobar una fila `processed` en `billing_webhook_events`;
7. comprobar una suscripción `trialing` o `active` en `subscriptions`;
8. actualizar permisos desde `/account`;
9. confirmar plan `pro` y entitlements ampliados;
10. abrir el Customer Portal;
11. cancelar al final del período;
12. confirmar que el acceso se mantiene hasta `access_until`;
13. reenviar el mismo webhook y confirmar `duplicate=true` sin una segunda operación.

## Consultas operativas

Eventos recientes:

```sql
select
  provider,
  provider_event_id,
  event_type,
  status,
  attempt_count,
  last_attempt_at,
  processed_at,
  error_message
from billing_webhook_events
order by coalesce(last_attempt_at, processed_at) desc
limit 50;
```

Suscripciones recientes:

```sql
select
  user_id,
  provider,
  provider_customer_id,
  provider_subscription_id,
  provider_variant_id,
  provider_status,
  provider_updated_at,
  plan_code,
  status,
  cancel_at_period_end,
  current_period_end,
  access_until,
  updated_at
from subscriptions
order by updated_at desc
limit 50;
```

## Rollback

Para detener nuevos checkouts y webhooks sin retirar permisos existentes:

```text
BILLING_ENABLED=false
```

Después, redeplegar la API y establecer en Cloudflare:

```text
VITE_BILLING_ENABLED=false
```

No eliminar inmediatamente las filas de `subscriptions` ni `billing_webhook_events`: constituyen el registro operativo y permiten reconstruir el estado durante una recuperación.
