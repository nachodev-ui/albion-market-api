# Auth0 en producción

## Contrato de identidad

La API usa Auth0 para identidad y autorización OAuth del endpoint de cuenta. La autorización comercial continúa en PostgreSQL mediante planes, suscripciones, entitlements y overrides.

```text
Issuer: https://albion-production-calculator.us.auth0.com/
Audience / API Identifier: https://albion-market-api
Required delegated scope: read:account
Frontend origin: https://albion-production-calculator.pages.dev
API origin: https://albion-market-api.onrender.com
Signing Algorithm: RS256
```

## Validación de access tokens

Antes de ejecutar las rutas `/api/v1/me` y `/api/v1/me/entitlements`, la API comprueba:

- firma RS256 mediante JWKS;
- issuer exacto;
- audience exacta;
- `sub`, expiración, `nbf` e `iat`;
- presencia de `read:account` en `scope` o `permissions`.

Un token válido sin el scope requerido responde `403 forbidden`. Un token ausente, inválido o expirado responde `401 unauthorized`.

## Auth0 Application Access

La API define el permiso:

```text
read:account
```

La SPA `Albion Production Calculator` tiene acceso delegado `1 / 1`. Client Access y Client Credentials permanecen deshabilitados.

El frontend solicita:

```text
openid profile email read:account
```

## Render

`render.yaml` declara los identificadores públicos y activa Auth0:

```text
AUTH_ENABLED=true
AUTH_EMERGENCY_DISABLED=false
AUTH_ISSUER=https://albion-production-calculator.us.auth0.com/
AUTH_AUDIENCE=https://albion-market-api
```

La configuración de production también usa esos valores como defaults seguros. `AUTH_EMERGENCY_DISABLED=true` es el único interruptor operacional para deshabilitar temporalmente identidad ante una incidencia.

## Deployment

El workflow `Deploy Auth0 production to Render`:

1. valida el contrato Render;
2. consulta discovery y JWKS del tenant;
3. aplica las migraciones de Neon;
4. despliega la revisión exacta mediante el hook de Render;
5. espera que `/metrics` exponga esa revisión;
6. verifica health y readiness;
7. exige `401` en `/api/v1/me` sin bearer token;
8. valida CORS desde Cloudflare Pages.

El workflow manual general `Deploy production to Render` también espera Auth0 habilitado, evitando regresiones futuras a `503 authentication unavailable`.
