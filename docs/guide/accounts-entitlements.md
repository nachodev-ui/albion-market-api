# Cuentas y permisos

La identidad, la suscripción y los permisos se mantienen separados del proveedor de pago. Auth0 emitirá el access token; la API valida el JWT y PostgreSQL conserva el usuario local, el acceso efectivo y los overrides administrativos.

## Configuración

```text
AUTH_ENABLED=false
AUTH_ISSUER=https://<tenant>.auth0.com/
AUTH_AUDIENCE=https://albion-production-calculator/api
AUTH_JWKS_CACHE_TTL=15m
AUTH_HTTP_TIMEOUT=5s
AUTH_CLOCK_SKEW=30s
```

`AUTH_ENABLED=false` conserva el despliegue actual mientras se crea el tenant. Al habilitarlo en producción, el issuer debe usar HTTPS y el audience debe coincidir exactamente con el identificador de API configurado en Auth0.

## Recorrido autenticado

1. El frontend obtiene un access token RS256 para el audience de la API.
2. Envía `Authorization: Bearer <token>` a `GET /api/v1/me`.
3. La API valida firma, issuer, audience y vigencia.
4. `app_users` se crea o actualiza usando el claim estable `sub`.
5. Se resuelve la suscripción vigente, se cargan permisos del plan y se aplican overrides activos.
6. La respuesta usa `Cache-Control: no-store`.

## Asignación manual de Pro

Después del primer acceso:

```sql
insert into subscriptions (
    user_id, provider, provider_subscription_id, plan_code, status,
    current_period_start, current_period_end, access_until
)
select
    id, 'manual', 'manual:' || id::text, 'pro', 'active',
    now(), now() + interval '30 days', now() + interval '30 days'
from app_users
where auth_subject = 'auth0|REPLACE_WITH_SUBJECT'
on conflict (provider, provider_subscription_id)
do update set
    plan_code = excluded.plan_code,
    status = excluded.status,
    current_period_start = excluded.current_period_start,
    current_period_end = excluded.current_period_end,
    access_until = excluded.access_until,
    updated_at = now();
```

Para retirar el acceso:

```sql
update subscriptions
set status = 'expired', access_until = now(), updated_at = now()
where provider = 'manual'
  and user_id = (
      select id from app_users
      where auth_subject = 'auth0|REPLACE_WITH_SUBJECT'
  );
```

## Overrides administrativos

```sql
insert into user_entitlement_overrides (
    user_id, entitlement_key, entitlement_value, reason, expires_at
)
select
    id, 'exports.csv', 'true'::jsonb, 'Acceso de prueba',
    now() + interval '14 days'
from app_users
where auth_subject = 'auth0|REPLACE_WITH_SUBJECT'
on conflict (user_id, entitlement_key)
do update set
    entitlement_value = excluded.entitlement_value,
    reason = excluded.reason,
    expires_at = excluded.expires_at,
    updated_at = now();
```

La interfaz puede ocultar funciones según `GET /api/v1/me/entitlements`, pero la autorización definitiva debe permanecer en la API mediante `accounts.RequireEntitlement`.
