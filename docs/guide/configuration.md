# Configuración

La API carga primero las variables del proceso. En desarrollo puede completar valores desde `.env.local`, `.env.<entorno>.local`, `.env.<entorno>` y `.env`, sin sobrescribir variables ya definidas.

## Variables principales

| Variable | Predeterminado | Propósito |
|---|---:|---|
| `APP_ENV` | `development` | Entorno de ejecución |
| `HTTP_ADDR` | `:8080` | Dirección HTTP |
| `DATABASE_URL` | obligatorio* | URI PostgreSQL como variable |
| `DATABASE_URL_FILE` | obligatorio* | Archivo que contiene la URI PostgreSQL |
| `LOAD_DOTENV` | `true` fuera de producción | Carga de archivos dotenv |
| `READ_HEADER_TIMEOUT` | `2s` | Límite para cabeceras |
| `READ_TIMEOUT` | `5s` | Límite de lectura |
| `WRITE_TIMEOUT` | `10s` | Límite de escritura |
| `IDLE_TIMEOUT` | `60s` | Keep-alive inactivo |
| `MAX_HEADER_BYTES` | `1048576` | Tamaño máximo de cabeceras |

## Ingesta y secretos

| Variable | Uso |
|---|---|
| `INGEST_BEARER_TOKEN` | Token actual como variable |
| `INGEST_BEARER_TOKEN_FILE` | Archivo del token actual |
| `INGEST_BEARER_TOKEN_ID` | Identificador no secreto |
| `INGEST_BEARER_TOKEN_PREVIOUS` | Token anterior durante rotación |
| `INGEST_BEARER_TOKEN_PREVIOUS_FILE` | Archivo del token anterior |
| `INGEST_BEARER_TOKEN_PREVIOUS_ID` | Identificador anterior |
| `INGEST_MIN_TOKEN_LENGTH` | Longitud mínima; 32 en producción |
| `INGEST_REQUIRE_HTTPS` | Exige HTTPS para ingesta |
| `MAX_INGEST_BODY_BYTES` | Límite comprimido y descomprimido |
| `MAX_PUBLIC_BODY_BYTES` | Límite de consultas públicas POST |

`DATABASE_URL` y `DATABASE_URL_FILE` son mutuamente excluyentes; uno de los dos es obligatorio. La misma regla se aplica a cada par de token de ingesta. Consulta [secretos y autenticación](../security/secrets.md).

Los archivos gestionados por Docker Compose bajo `/run/secrets` se aceptan como secretos montados de solo lectura. Los archivos de producción ubicados fuera de ese directorio deben usar permisos `0600` o más restrictivos en Linux.

## CORS, proxy y rate limiting

| Variable | Predeterminado |
|---|---:|
| `CORS_ALLOWED_ORIGINS` | orígenes locales de Vite |
| `RATE_LIMIT_ENABLED` | `true` |
| `RATE_LIMIT_REQUESTS_PER_SECOND` | `20` |
| `RATE_LIMIT_BURST` | `40` |
| `RATE_LIMIT_CLIENT_TTL` | `10m` |
| `TRUST_PROXY_HEADERS` | `false` |

En producción no se admite `*` como origen CORS. Activa `TRUST_PROXY_HEADERS` solo detrás de un proxy confiable que reescriba las cabeceras reenviadas.

## Logs

`LOG_COLOR` admite `auto`, `always` o `never`. La variable estándar `NO_COLOR` también desactiva colores en modo automático.

La plantilla completa y mantenida junto al código es `.env.example`.
