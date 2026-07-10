# Despliegue reproducible y seguro

El repositorio mantiene dos rutas de despliegue con el mismo contrato de
seguridad:

- **Docker Compose local**, para pruebas integrales y operación autocontenida;
- **Fly.io + Neon**, para la API HTTPS y PostgreSQL administrado de producción.

La guía del entorno público está en [Producción en Fly.io y Neon](./fly-neon-production.md).

## Archivos mantenidos

| Archivo | Responsabilidad |
|---|---|
| `Dockerfile` | Compila API, healthcheck y runner de migraciones en un runtime `scratch` |
| `fly.toml` | Región, recursos, HTTPS, readiness y release command de Fly.io |
| `.github/workflows/deploy-production.yml` | Despliegue manual controlado mediante GitHub Environment |
| `deploy/compose.yaml` | PostgreSQL, migraciones, API y observabilidad para el entorno local |
| `scripts/bootstrap-fly-production.ps1` | Importa secretos y realiza el primer despliegue público |
| `scripts/new-production-ingest-token.ps1` | Genera la credencial de ingesta sin imprimirla |
| `scripts/initialize-deployment.ps1` | Inicializa secretos y configuración de Compose local |
| `scripts/test-container.ps1` | Smoke test directo de la imagen |
| `scripts/test-deployment-compose.ps1` | Prueba integral del stack Compose |
| `scripts/test-observability-compose.ps1` | Valida Prometheus, Alertmanager y Grafana |

## Propiedades de la imagen

- build multi-stage;
- dependencias verificadas con `go mod verify`;
- binarios estáticos con `CGO_ENABLED=0`, `-trimpath` y sin VCS implícito;
- runtime `scratch`, sin shell ni gestor de paquetes;
- UID/GID `65532:65532`;
- certificados CA y zona horaria UTC;
- healthcheck mediante un binario Go dedicado;
- runner de migraciones compilado en Go;
- SQL de `migrations/` incorporados como archivos de solo lectura;
- metadatos OCI de versión, revisión y fecha;
- ningún secreto, documento, paquete Node ni archivo `.env` dentro de la imagen.

La imagen contiene únicamente los artefactos necesarios para ejecutar la API y
preparar su esquema:

```text
/usr/local/bin/albion-market-api
/usr/local/bin/healthcheck
/usr/local/bin/migrate
/migrations/*.sql
```

## Producción administrada

La arquitectura pública utiliza:

```text
Cloudflare Pages
  → Fly.io São Paulo
  → Neon PostgreSQL São Paulo
```

Fly.io ejecuta `/usr/local/bin/migrate` antes de cada release. Una migración
fallida bloquea la sustitución de la instancia activa. El balanceador utiliza
`/readyz`, por lo que no envía tráfico a una Machine cuyo pool, PostgreSQL o
esquema requerido no estén disponibles.

Consulta [Producción en Fly.io y Neon](./fly-neon-production.md) para provisionar
los dominios, secretos y GitHub Environment definitivos.

## Docker Compose local

### Inicialización

```powershell
Set-Location C:\Users\mitsf\Desktop\albion-market-api

.\scripts\initialize-deployment.ps1 `
  -AllowedOrigins "https://frontend.example.com" `
  -IngestTokenId "receiver-202607"
```

El script crea archivos ignorados por Git:

```text
secrets/deployment/postgres-password
secrets/deployment/database-url
secrets/deployment/ingest-current.token
deploy/compose.env.local
```

No muestres ni confirmes el contenido de `secrets/deployment/`.

### Validar y arrancar

```powershell
docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  config --quiet

docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  up --build --detach
```

Compose aplica este orden obligatorio:

1. inicia PostgreSQL;
2. espera su healthcheck;
3. ejecuta todas las migraciones SQL en orden lexicográfico;
4. exige que `migrate` termine con código `0`;
5. solo entonces crea la API.

### Comprobaciones

```powershell
docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  ps --all

Invoke-RestMethod http://127.0.0.1:18080/healthz
Invoke-RestMethod http://127.0.0.1:18080/readyz
(Invoke-WebRequest http://127.0.0.1:18080/metrics).Content
```

El healthcheck de la imagen usa `/healthz`; los balanceadores y orquestadores
deben usar `/readyz`.

## Secretos

En Compose, los secretos se montan bajo `/run/secrets` y no aparecen en las
variables del contenedor. En Fly.io, `DATABASE_URL` e `INGEST_BEARER_TOKEN` se
almacenan en su vault cifrado. GitHub Actions recibe únicamente un deploy token
limitado a la aplicación.

Nunca incorpores secretos a:

- el `Dockerfile`;
- `fly.toml`;
- workflows;
- argumentos de build;
- variables `VITE_*`;
- issues, pull requests o documentación.

## Endurecimiento del runtime local

`deploy/compose.yaml` aplica:

- usuario `65532:65532`;
- filesystem raíz de solo lectura;
- eliminación de capacidades Linux;
- `no-new-privileges`;
- límite de procesos;
- `/tmp` temporal, no ejecutable y limitado;
- red de PostgreSQL interna;
- publicación de la API solo en `127.0.0.1`;
- apagado por `SIGTERM` con 15 segundos de gracia.

## Observabilidad local

```powershell
docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  --profile observability `
  up --build --detach
```

Sin el perfil se ejecutan PostgreSQL, migraciones y API. Con el perfil se añaden
Prometheus, Alertmanager y Grafana, publicados únicamente en loopback.

## Pruebas

```powershell
.\scripts\test-container.ps1
.\scripts\test-deployment-compose.ps1
.\scripts\test-observability-compose.ps1
```

CI valida formato, tests con race detector, contratos, documentación, construcción
de imagen, escaneo de vulnerabilidades y smoke tests. La publicación de imágenes
y el despliegue público permanecen separados de los checks de pull request.

## Destrucción local

Detener sin borrar datos:

```powershell
docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  down
```

Eliminar también el volumen PostgreSQL es destructivo:

```powershell
docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  down --volumes
```

No uses `--volumes` en un entorno con datos que deban conservarse.
