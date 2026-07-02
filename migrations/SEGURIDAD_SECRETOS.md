# Secretos y autenticación de ingesta

La ingesta de precios e historial usa credenciales Bearer dedicadas. Las rutas
públicas de lectura no requieren esta credencial.

## Controles implementados

- comparación de credenciales mediante digest SHA-256 y comparación constante;
- cabecera `Authorization` validada estrictamente como `Bearer <token>`;
- credenciales identificadas por un `key id` no secreto;
- token actual y token anterior simultáneos durante una rotación;
- lectura desde variables de entorno o archivos montados, nunca ambos para la
  misma credencial;
- mínimo configurable de 32 caracteres por defecto;
- rechazo de placeholders como `CHANGE_ME`;
- HTTPS obligatorio por defecto cuando `APP_ENV=production`;
- `X-Forwarded-Proto` solo se confía cuando `TRUST_PROXY_HEADERS=true`;
- respuestas `401` con `WWW-Authenticate` y sin detalles del token;
- valores asociados a campos sensibles se redactan en logs;
- los logs exitosos muestran únicamente `auth_key_id`, útil para comprobar que
  la credencial anterior dejó de utilizarse.

## Configuración recomendada por archivos

En `.env.local`:

```env
INGEST_BEARER_TOKEN=
INGEST_BEARER_TOKEN_FILE=./secrets/ingest-current.token
INGEST_BEARER_TOKEN_ID=receiver-202607

INGEST_BEARER_TOKEN_PREVIOUS=
INGEST_BEARER_TOKEN_PREVIOUS_FILE=
INGEST_BEARER_TOKEN_PREVIOUS_ID=receiver-previous

INGEST_MIN_TOKEN_LENGTH=32
```

Los archivos deben contener solamente el token, sin comillas. En Linux, los
archivos usados en producción deben tener permisos `0600` o más restrictivos.

También se mantiene compatibilidad con secretos inyectados directamente por el
proveedor:

```env
INGEST_BEARER_TOKEN=<secreto del proveedor>
INGEST_BEARER_TOKEN_FILE=
```

`INGEST_BEARER_TOKEN` y `INGEST_BEARER_TOKEN_FILE` son mutuamente excluyentes.
Lo mismo aplica a la credencial anterior.

## Rotación sin interrupciones

1. Conserva el token actual como `INGEST_BEARER_TOKEN_PREVIOUS` o en
   `INGEST_BEARER_TOKEN_PREVIOUS_FILE`.
2. Configura un token nuevo como credencial actual y cambia su `key id`.
3. Reinicia primero `albion-market-api`.
4. Actualiza el token utilizado por `albion-market-data-platform` y reinicia el
   receiver.
5. Revisa los eventos `ingest.completed` e `ingest.history_completed`. Cuando
   todos muestren el `auth_key_id` nuevo, elimina la credencial anterior.
6. Reinicia nuevamente la API.

El script `scripts/rotate-ingest-token.ps1` del repositorio
`albion-market-data-platform` automatiza la generación y sincronización local de
los archivos sin imprimir el token.

## HTTPS y proxy

En producción, `INGEST_REQUIRE_HTTPS` es `true` por defecto. Una petición HTTP
recibe `426 Upgrade Required` antes de validar la credencial.

Detrás de un proxy TLS confiable:

```env
INGEST_REQUIRE_HTTPS=true
TRUST_PROXY_HEADERS=true
```

No actives `TRUST_PROXY_HEADERS` cuando los clientes puedan conectarse
 directamente a la API, porque podrían falsificar `X-Forwarded-Proto` y las
cabeceras de IP.

## Archivos que nunca deben publicarse

```text
.env
.env.local
.env.*.local
secrets/
*.token
*.secret
```

`.env.example` contiene únicamente nombres de variables y rutas de ejemplo. La
API ya no carga `.env.example` en tiempo de ejecución.
