# Despliegue reproducible y seguro

El repositorio mantiene dos rutas de despliegue:

- **Docker Compose local**, para pruebas integrales y operación autocontenida;
- **Render + Neon**, para la API HTTPS y PostgreSQL administrado de producción.

La guía pública canónica está en
[Producción en Render, Neon y Cloudflare](./render-neon-production.md).

## Archivos mantenidos

| Archivo | Responsabilidad |
|---|---|
| `Dockerfile` | Compila API, healthcheck y runner de migraciones en un runtime `scratch` |
| `render.yaml` | Contrato declarativo del servicio, runtime, health check, CORS y secretos |
| `.github/workflows/deploy-production.yml` | Migración previa, deploy manual y validación integral de producción |
| `deploy/compose.yaml` | PostgreSQL, migraciones, API y observabilidad para el entorno local |
| `scripts/validate-render-config.py` | Rechaza divergencias de Render y dependencias retiradas |
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
- metadatos de versión, revisión y creación;
- ningún secreto, documento, paquete Node ni archivo `.env` dentro de la imagen.

La imagen contiene:

```text
/usr/local/bin/albion-market-api
/usr/local/bin/healthcheck
/usr/local/bin/migrate
/usr/local/share/albion-market-api/build-metadata.json
/migrations/*.sql
```

## Producción administrada

```text
Cloudflare Pages
  → Render HTTPS
  → Neon PostgreSQL São Paulo
```

Render mantiene los despliegues automáticos desactivados. El workflow manual del
GitHub Environment `production` construye la revisión exacta, ejecuta primero el
runner de migraciones contra Neon y solo después invoca el deploy hook de Render.

El job espera que `/metrics` publique el SHA solicitado y comprueba:

- `/healthz`;
- `/readyz`;
- CORS para el dominio Cloudflare;
- rechazo `401` de una credencial inválida sin persistencia;
- precios e historial públicos;
- disponibilidad del frontend.

Consulta [la guía de producción](./render-neon-production.md) para configurar los
dos secretos de entrega y ejecutar el procedimiento.

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

Compose aplica este orden:

1. inicia PostgreSQL;
2. espera su healthcheck;
3. ejecuta migraciones SQL en orden lexicográfico;
4. exige código `0`;
5. crea la API.

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

El healthcheck de la imagen usa `/healthz`; balanceadores y orquestadores deben
usar `/readyz`.

## Secretos

En Compose, los secretos se montan bajo `/run/secrets`. En Render,
`DATABASE_URL` e `INGEST_BEARER_TOKEN` permanecen en el gestor de variables del
servicio. GitHub solo recibe la URL directa de migración y el deploy hook dentro
del Environment protegido `production`.

Nunca incorpores secretos a:

- `Dockerfile`;
- `render.yaml`;
- workflows;
- argumentos de build;
- variables `VITE_*`;
- issues, pull requests o documentación.

## Observabilidad local

```powershell
docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  --profile observability `
  up --build --detach
```

Sin el perfil se ejecutan PostgreSQL, migraciones y API. Con el perfil se añaden
Prometheus, Alertmanager y Grafana, publicados solo en loopback.

## Pruebas

```powershell
python .\scripts\validate-render-config.py
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

No uses `--volumes` si los datos deben conservarse.
