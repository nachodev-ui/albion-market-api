# Producción en Fly.io y Neon

## Arquitectura elegida

```text
Cloudflare Pages
  https://albion-production-calculator.pages.dev
             │
             ▼
Fly.io São Paulo (gru)
  https://albion-market-api-nachodev.fly.dev
             │
             ▼
Neon PostgreSQL AWS São Paulo (aws-sa-east-1)
```

El frontend se sirve desde la red global de Cloudflare. La API y PostgreSQL se
mantienen en São Paulo para reducir la latencia desde Chile y evitar tráfico de
base de datos entre regiones.

## Dominios canónicos

| Componente | Dominio |
|---|---|
| Frontend | `https://albion-production-calculator.pages.dev` |
| API | `https://albion-market-api-nachodev.fly.dev` |
| API base v1 | `https://albion-market-api-nachodev.fly.dev/api/v1` |

Si alguno de los nombres no está disponible al crear la cuenta, cambia el nombre
en `fly.toml`, el workflow de despliegue, `CORS_ALLOWED_ORIGINS` y la variable
`VITE_CENTRAL_MARKET_API_URL` antes del primer release. No despliegues con nombres
divergentes entre repositorios.

## Secretos de producción

Solo existen dos secretos de runtime para la API:

| Secreto | Ubicación definitiva |
|---|---|
| `DATABASE_URL` | vault cifrado de Fly.io |
| `INGEST_BEARER_TOKEN` | vault cifrado de Fly.io y archivo local protegido del colaborador |
| `FLY_API_TOKEN` | GitHub Environment `production` del repositorio de API |

No copies `DATABASE_URL` ni `INGEST_BEARER_TOKEN` a GitHub Actions. El workflow de
GitHub necesita únicamente un deploy token limitado a la aplicación de Fly.io.

## 1. Crear PostgreSQL en Neon

Crea un proyecto con estas propiedades:

- nombre: `albion-market-production`;
- proveedor: AWS;
- región: South America (São Paulo), `aws-sa-east-1`;
- PostgreSQL: versión estable disponible;
- base: `albion_market`;
- rol dedicado: `albion_market_api`.

Usa la conexión **directa**, no la URL pooled, porque el release runner utiliza un
advisory lock de sesión durante las migraciones. La cadena debe exigir TLS. Guarda
la URL completa, sin imprimirla en consola, en:

```text
secrets/deployment/neon-database-url.secret
```

Ejemplo de forma, nunca de valor real:

```text
postgresql://ROLE:PASSWORD@HOST/albion_market?sslmode=require
```

## 2. Generar la credencial de ingesta

Desde PowerShell:

```powershell
.\scripts\new-production-ingest-token.ps1
```

El token queda en:

```text
secrets/deployment/ingest-current.token
```

El archivo está ignorado por Git y el script restringe sus permisos. Conserva este
mismo valor para configurar `albion-market-data-platform`; no generes credenciales
diferentes para API y receiver.

## 3. Crear y desplegar la aplicación Fly.io

Instala `flyctl`, inicia sesión y ejecuta:

```powershell
.\scripts\bootstrap-fly-production.ps1
```

El script:

1. crea `albion-market-api-nachodev` si todavía no existe;
2. importa `DATABASE_URL` e `INGEST_BEARER_TOKEN` al vault de Fly.io;
3. construye la imagen mediante el `Dockerfile` auditado;
4. ejecuta `/usr/local/bin/migrate` como release command;
5. cancela el despliegue si una migración falla;
6. arranca la API solo después de completar el esquema;
7. valida `/healthz` y `/readyz` mediante HTTPS.

## 4. Configurar despliegue continuo

Crea un deploy token limitado a la aplicación:

```powershell
flyctl tokens create deploy --app albion-market-api-nachodev
```

Guárdalo como secreto `FLY_API_TOKEN` dentro del GitHub Environment
`production` del repositorio `albion-market-api`. No uses un token personal con
acceso a todas las aplicaciones.

El workflow `.github/workflows/deploy-production.yml` queda deliberadamente bajo
`workflow_dispatch`. Esto evita un despliegue accidental antes de provisionar
Neon, Fly.io y los secretos definitivos. Después del primer bootstrap, ejecútalo
desde GitHub Actions para validar el camino de entrega continua.

## Migraciones de release

La imagen incluye:

```text
/usr/local/bin/albion-market-api
/usr/local/bin/healthcheck
/usr/local/bin/migrate
/migrations/*.sql
```

Fly.io ejecuta `/usr/local/bin/migrate` en una Machine temporal antes de reemplazar
la instancia activa. El runner:

- lee `DATABASE_URL` o `DATABASE_URL_FILE`;
- obtiene un advisory lock de PostgreSQL;
- ordena los SQL lexicográficamente;
- ejecuta cada archivo dentro de una transacción;
- detiene el release ante cualquier error.

Las migraciones actuales son idempotentes y siguen siendo la única fuente del
esquema en `migrations/`.

## Configuración de runtime

`fly.toml` fija:

- región primaria `gru`;
- una Machine `shared-cpu-1x` con 512 MB;
- máquina siempre activa;
- HTTPS obligatorio;
- `TRUST_PROXY_HEADERS=true` porque Fly controla las cabeceras reenviadas;
- CORS limitado al dominio público del frontend;
- logs JSON;
- health check de routing en `/readyz`;
- apagado por `SIGTERM` con 15 segundos de gracia.

## Verificación posterior

```powershell
Invoke-RestMethod https://albion-market-api-nachodev.fly.dev/healthz
Invoke-RestMethod https://albion-market-api-nachodev.fly.dev/readyz
Invoke-RestMethod https://albion-market-api-nachodev.fly.dev/api/v1/status
```

También comprueba:

```powershell
flyctl status --app albion-market-api-nachodev
flyctl checks list --app albion-market-api-nachodev
flyctl secrets list --app albion-market-api-nachodev
flyctl logs --app albion-market-api-nachodev
```

`flyctl secrets list` debe mostrar únicamente nombres y digests; nunca revela los
valores almacenados.

## Rotación del token de ingesta

1. Genera un token nuevo en un archivo separado.
2. Configúralo como `INGEST_BEARER_TOKEN` en Fly.io.
3. Conserva temporalmente el anterior como `INGEST_BEARER_TOKEN_PREVIOUS`.
4. Actualiza el receiver y confirma envíos exitosos.
5. Elimina el secreto anterior de Fly.io.

La rotación debe completarse sin publicar tokens en terminales compartidas, logs,
issues, pull requests o documentación.
