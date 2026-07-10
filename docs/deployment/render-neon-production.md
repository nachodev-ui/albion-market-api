# Producción en Render, Neon y Cloudflare

## Arquitectura canónica

```text
Albion Data Client
  → albion-market-data-platform
  → Render HTTPS
  → Neon PostgreSQL
  → albion-production-calculator en Cloudflare Pages
```

Dominios oficiales:

| Componente | URL |
|---|---|
| Frontend | `https://albion-production-calculator.pages.dev` |
| API | `https://albion-market-api.onrender.com` |
| API v1 | `https://albion-market-api.onrender.com/api/v1` |

La configuración versionada de Render está en `render.yaml`. El servicio usa el
`Dockerfile`, ejecuta `/usr/local/bin/albion-market-api`, enruta su health check a
`/readyz` y mantiene `autoDeployTrigger: off`.

## Principio de entrega

La producción no se despliega automáticamente al fusionar `main`. El único camino
oficial es el workflow manual `.github/workflows/deploy-production.yml`, protegido
por el GitHub Environment `production`.

El orden es obligatorio:

1. resolver la revisión exacta de `main`;
2. construir la imagen con `VERSION`, `REVISION` y `CREATED`;
3. ejecutar `/usr/local/bin/migrate` contra la URL directa de Neon;
4. detener el proceso si una migración falla;
5. disparar el deploy hook de Render para esa misma revisión;
6. esperar hasta que `/metrics` publique la revisión esperada;
7. comprobar `/healthz` y `/readyz`;
8. validar CORS desde Cloudflare;
9. comprobar la autenticación de ingesta sin insertar datos;
10. consultar precios e historial públicos;
11. comprobar que el frontend de Cloudflare siga disponible.

Esta separación evita reemplazar la instancia activa antes de validar el esquema.
Render conserva la versión anterior mientras construye y promueve la nueva; el
workflow no considera terminado el despliegue hasta observar la revisión exacta.

## Configuración única en Render

El servicio existente `albion-market-api` debe quedar conectado al repositorio
`nachodev-ui/albion-market-api`, rama `main`, usando Docker.

Ajustes obligatorios:

```text
Auto-Deploy: Off
Dockerfile: ./Dockerfile
Docker command: /usr/local/bin/albion-market-api
Health check: /readyz
```

Variables no secretas y límites se documentan en `render.yaml`. Render debe
conservar como secretos:

```text
DATABASE_URL
INGEST_BEARER_TOKEN
```

`DATABASE_URL` es la conexión de runtime de la API. No se publica en GitHub,
logs, issues ni documentación. `INGEST_BEARER_TOKEN` debe coincidir con el token
configurado en `albion-market-data-platform`.

Después de comprobar que el servicio existente coincide con `render.yaml`, puede
vincularse al Blueprint. No crees un segundo servicio con el mismo propósito.

## GitHub Environment `production`

El Environment requiere aprobación manual y contiene solamente estos secretos de
despliegue:

| Secreto | Uso |
|---|---|
| `NEON_MIGRATION_DATABASE_URL` | URL **directa** de Neon para el advisory lock y las migraciones |
| `RENDER_DEPLOY_HOOK_URL` | Deploy hook secreto del servicio Render |

La URL de migración debe apuntar a la base `albion_market`, exigir TLS y no usar el
endpoint pooled. El workflow la entrega únicamente al contenedor efímero de
migración y no la imprime.

El deploy hook se crea en Render, dentro de la configuración del servicio. Trátalo
como una credencial: quien conoce la URL puede iniciar despliegues.

Cloudflare no necesita secretos en este repositorio. Su dominio público se usa
solo como origen CORS y como smoke test final.

## Metadatos de build

El workflow explícito inyecta:

```text
VERSION  = SHA corto de 12 caracteres
REVISION = SHA completo
CREATED  = fecha ISO 8601 del commit
```

Cuando Render construye directamente el Dockerfile, `RENDER_GIT_COMMIT` se usa
como fallback para versión y revisión, y `CREATED` se genera durante el build. La
API registra los tres valores al arrancar y `/metrics` expone versión y revisión
en `albion_market_api_build_info`.

La imagen también conserva `/usr/local/share/albion-market-api/build-metadata.json`
como evidencia interna del build.

## Migraciones

`/usr/local/bin/migrate`:

- lee `DATABASE_URL` o `DATABASE_URL_FILE`;
- adquiere un advisory lock de sesión;
- ordena `migrations/*.sql` lexicográficamente;
- ejecuta cada archivo dentro de una transacción;
- termina con código distinto de cero ante cualquier error.

Las migraciones publicadas no se reescriben. Cada cambio de esquema agrega un
archivo nuevo y debe ser compatible con la versión activa durante el despliegue.
Las operaciones destructivas requieren una fase separada después de retirar todo
código que dependa del esquema anterior.

Antes de cada release, Neon debe seguir mostrando el marcador requerido en
`public.app_schema_state`. Actualmente `/readyz` exige como mínimo la versión `6`.

## Ejecutar el despliegue

1. Fusiona el cambio aprobado en `main` sin activar el auto-deploy de Render.
2. Abre **Actions → Deploy production to Render → Run workflow**.
3. Selecciona la rama `main`.
4. Escribe `DEPLOY` en el campo de confirmación.
5. Aprueba el GitHub Environment `production`.
6. Revisa el resumen del job y la revisión publicada.

El workflow rechaza ejecuciones desde una rama distinta de `main`.

## Verificaciones reproducibles

PowerShell:

```powershell
$Api = "https://albion-market-api.onrender.com"
$Frontend = "https://albion-production-calculator.pages.dev"

Invoke-RestMethod "$Api/healthz"
Invoke-RestMethod "$Api/readyz"
Invoke-RestMethod "$Api/api/v1/status"

Invoke-RestMethod `
  "$Api/api/v1/prices?server=west&marketKey=martlock&itemIds=T4_BAG&quality=1"

Invoke-RestMethod `
  "$Api/api/v1/history?server=west&marketKey=martlock&itemId=T4_BAG&quality=1&period=4-weeks"

Invoke-WebRequest $Frontend -UseBasicParsing
```

Validación CORS:

```powershell
$Headers = @{
  Origin = $Frontend
  "Access-Control-Request-Method" = "POST"
  "Access-Control-Request-Headers" = "content-type"
}

$response = Invoke-WebRequest `
  -Method Options `
  -Uri "$Api/api/v1/prices/query" `
  -Headers $Headers `
  -UseBasicParsing

$response.Headers["Access-Control-Allow-Origin"]
```

Prueba de autenticación sin escritura:

```powershell
try {
  Invoke-WebRequest `
    -Method Post `
    -Uri "$Api/api/v1/ingest/prices" `
    -Headers @{ Authorization = "Bearer deployment-probe-invalid-token" } `
    -ContentType "application/json" `
    -Body "{}" `
    -UseBasicParsing

  throw "La API aceptó una credencial inválida."
}
catch {
  if ($_.Exception.Response.StatusCode.value__ -ne 401) {
    throw
  }
}
```

La autenticación se evalúa antes de procesar el payload, por lo que esta sonda no
alcanza el servicio de persistencia.

## Rollback

Si falla antes del deploy hook, Render no recibe una revisión nueva. Si falla el
build o la promoción en Render, la versión activa anterior permanece sirviendo.

Para volver a código anterior:

1. identifica un commit de `main` compatible con el esquema ya aplicado;
2. usa **Rollback** en Render o dispara manualmente ese commit;
3. comprueba `build_info`, `/healthz`, `/readyz`, CORS, precios e historial;
4. abre un PR correctivo hacia `develop` y luego `main`.

Las migraciones no se revierten automáticamente. Un rollback de aplicación solo
es seguro cuando el esquema nuevo mantiene compatibilidad hacia atrás.

## Configuración retirada

Fly.io ya no forma parte de producción. Se eliminaron:

```text
fly.toml
scripts/bootstrap-fly-production.ps1
scripts/validate-fly-config.py
docs/deployment/fly-neon-production.md
```

No vuelvas a agregar `FLY_API_TOKEN`, `flyctl` ni dominios `fly.dev` al camino de
producción.
