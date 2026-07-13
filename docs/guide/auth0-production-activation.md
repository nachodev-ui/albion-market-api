# Activación de Auth0 en producción

## Contrato de identidad

La API utiliza Auth0 únicamente para identidad. La autorización efectiva permanece en PostgreSQL mediante planes, suscripciones, entitlements y overrides.

Valores acordados:

```text
Audience / API Identifier: https://albion-market-api
Frontend origin: https://albion-production-calculator.pages.dev
API origin: https://albion-market-api.onrender.com
```

## Auth0

Crear una API llamada `Albion Market API` con:

```text
Identifier: https://albion-market-api
Signing Algorithm: RS256
```

La SPA `Albion Production Calculator` debe solicitar access tokens para esa audience.

## Render

Configurar en el servicio `albion-market-api`:

```text
AUTH_ENABLED=false
AUTH_ISSUER=https://<tenant-domain>/
AUTH_AUDIENCE=https://albion-market-api
```

Primero guardar issuer y audience con autenticación deshabilitada. Después de comprobar el discovery document y JWKS, cambiar `AUTH_ENABLED=true` y ejecutar el workflow manual `Deploy production to Render`.

`render.yaml` declara las tres variables con `sync: false` para que sus valores reales permanezcan gestionados por Render y no se almacenen en Git.

## GitHub Environment `production`

Configurar las variables públicas de control usadas por el workflow:

```text
AUTH0_ENABLED=false
AUTH0_ISSUER=https://<tenant-domain>/
AUTH0_AUDIENCE=https://albion-market-api
```

El workflow ejecuta `scripts/validate-auth0-production.py` antes de migrar o desplegar. Cuando `AUTH0_ENABLED=true`, valida el discovery document, endpoints HTTPS y al menos una clave RSA compatible con RS256.

Después del despliegue, `/api/v1/me` sin bearer token debe responder:

- `401 unauthorized` cuando Auth0 está habilitado;
- `503 authentication unavailable` cuando continúa deshabilitado.

Esta comprobación evita publicar un frontend con login activo contra una API todavía apagada.
