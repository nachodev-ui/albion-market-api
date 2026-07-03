# Despliegue reproducible y seguro

El repositorio define una imagen de producción mínima y un despliegue Docker Compose verificable. El contrato incluye PostgreSQL persistente, migraciones obligatorias antes del arranque, secretos montados como archivos y un runtime de API sin privilegios.

## Archivos mantenidos

| Archivo | Responsabilidad |
|---|---|
| `Dockerfile` | Compila los binarios y produce el runtime `scratch` |
| `deploy/compose.yaml` | Declara PostgreSQL, migraciones, API y perfil opcional de observabilidad |
| `deploy/compose.env.example` | Plantilla de configuración no secreta |
| `scripts/initialize-deployment.ps1` | Genera secretos y `compose.env.local` sin imprimir valores |
| `scripts/test-container.ps1` | Smoke test directo de la imagen |
| `scripts/test-deployment-compose.ps1` | Prueba integral del despliegue Compose |
| `scripts/test-observability-compose.ps1` | Prueba Prometheus, Alertmanager, Grafana, reglas y dashboard |

## Reproducibilidad de imágenes

Las imágenes externas están fijadas por referencia explícita y digest SHA-256:

```text
Go builder:
golang:1.26.4-bookworm@sha256:b305420a68d0f229d91eb3b3ed9e519fcf2cf5461da4bef997bf927e8c0bfd2b

PostgreSQL:
postgres:17.10-alpine3.23@sha256:3da929dcc3e63e3f0cc81fdb114c073ca48bfc7280e83a6324d5652fbee63742

Prometheus, Alertmanager y Grafana también están fijados por digest en `deploy/compose.yaml`.
```

Una reconstrucción usa exactamente esos manifiestos mientras los digests no cambien en el repositorio. `internal/deployment` impide regresar accidentalmente a imágenes externas flotantes.

Actualizar una imagen base exige:

1. elegir una etiqueta explícita;
2. resolver y revisar su digest;
3. actualizar el archivo y la prueba contractual en el mismo commit;
4. ejecutar los smoke tests de imagen, Compose base y observabilidad;
5. revisar vulnerabilidades y notas de versión antes del merge.

## Propiedades de la imagen de API

- build multi-stage;
- dependencias verificadas con `go mod verify`;
- binarios estáticos con `CGO_ENABLED=0`, `-trimpath` y sin VCS implícito;
- runtime `scratch`, sin shell ni gestor de paquetes;
- UID/GID `65532:65532`;
- certificados CA y zona horaria UTC;
- healthcheck mediante un binario Go dedicado;
- metadatos OCI de versión, revisión y fecha;
- ninguna migración, documentación, dependencia Node o secreto dentro de la imagen.

## Inicialización local

Requisitos:

- Docker Desktop con contenedores Linux;
- PowerShell 5.1 o superior;
- Git disponible;
- un puerto local libre, `18080` por defecto.

Genera la configuración inicial:

```powershell
Set-Location C:\Users\mitsf\Desktop\albion-market-api

.\scripts\initialize-deployment.ps1 `
  -AllowedOrigins "https://frontend.example.com" `
  -IngestTokenId "receiver-202607"
```

Se crean archivos ignorados por Git:

```text
secrets/deployment/postgres-password
secrets/deployment/database-url
secrets/deployment/ingest-current.token
deploy/compose.env.local
```

El script no imprime secretos. Sin `-Force`, se niega a sobrescribir una configuración existente.

Revisa únicamente el archivo no secreto:

```powershell
Get-Content .\deploy\compose.env.local
```

No muestres ni confirmes el contenido de `secrets/deployment/`.

## Validar y arrancar

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
4. exige que el servicio `migrate` termine con código `0`;
5. solo entonces crea la API.

Si una migración falla, la API no se inicia.

Comprueba el estado:

```powershell
docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  ps --all

Invoke-RestMethod http://127.0.0.1:18080/healthz
Invoke-RestMethod http://127.0.0.1:18080/readyz
(Invoke-WebRequest http://127.0.0.1:18080/metrics).Content
```

El `HEALTHCHECK` de la imagen usa `/healthz`: una interrupción breve de PostgreSQL
no reinicia un proceso HTTP sano. Los balanceadores y orquestadores deben usar
`/readyz` para retirar tráfico cuando el pool, PostgreSQL o el esquema requerido
no estén disponibles.

La API se publica únicamente en `127.0.0.1`. Para acceso externo usa un proxy TLS confiable. Mantén `INGEST_REQUIRE_HTTPS=true`; activa `TRUST_PROXY_HEADERS=true` solo cuando el proxy reescriba y controle las cabeceras reenviadas.

## Secretos en runtime

Compose concede cada secreto únicamente al servicio que lo necesita:

| Destino | Consumidor |
|---|---|
| `/run/secrets/postgres_password` | PostgreSQL y migraciones |
| `/run/secrets/database_url` | API mediante `DATABASE_URL_FILE` |
| `/run/secrets/ingest_token` | API mediante `INGEST_BEARER_TOKEN_FILE` |

Los valores no se incorporan a la imagen ni a las variables del contenedor de API. Los archivos bajo `/run/secrets` se montan como solo lectura. En Linux, `initialize-deployment.ps1` protege el directorio anfitrión con modo `0700` y deja cada archivo en `0444`: el directorio impide que otros usuarios del host alcancen los valores, mientras que el archivo bind-mounted puede ser leído por el UID no root `65532` dentro del contenedor. Para archivos secretos usados directamente fuera de Docker Compose, se mantiene la recomendación `0600`.

## Endurecimiento del servicio API

`deploy/compose.yaml` aplica:

- usuario `65532:65532`;
- filesystem raíz de solo lectura;
- eliminación de todas las capacidades Linux;
- `no-new-privileges`;
- límite de procesos;
- `/tmp` temporal, no ejecutable y limitado;
- red backend interna;
- apagado por `SIGTERM` con 15 segundos de gracia;
- volumen persistente reservado únicamente para PostgreSQL.

## Detener y eliminar

Detener sin borrar los datos:

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

No uses `--volumes` en un entorno que contenga datos que deban conservarse.

## Pruebas locales

Prueba directa de la imagen:

```powershell
.\scripts\test-container.ps1
```

Prueba integral del contrato Compose:

```powershell
.\scripts\test-deployment-compose.ps1
```

La segunda prueba crea secretos y un volumen temporales, construye la imagen, verifica migraciones, healthcheck, usuario no root, filesystem de solo lectura, capacidades eliminadas, ausencia de secretos en el entorno y apagado ordenado. Al finalizar elimina contenedores, red, volumen, imagen temporal y archivos secretos.

## Escaneo de vulnerabilidades

El workflow construye la imagen final `scratch` y la analiza con Trivy antes del smoke test de Compose. La acción está fijada a un commit completo y la versión del CLI también es explícita.

La política bloquea el workflow cuando Trivy encuentra vulnerabilidades corregibles de severidad `HIGH` o `CRITICAL` en paquetes del sistema o librerías incorporadas en los binarios. Los hallazgos sin corrección disponible se informan, pero no bloquean el merge.

El escaneo no usa `continue-on-error`; una infracción produce código de salida `1`. La base de vulnerabilidades se actualiza en cada ejecución, por lo que un commit previamente válido puede fallar más adelante si se publica un nuevo aviso de seguridad.

## CI

`.github/workflows/container.yml` ejecuta las pruebas contractuales de configuración, construye y escanea la imagen de producción y después ejecuta el smoke test Compose en Linux. También admite ejecución manual con `workflow_dispatch` una vez que el workflow ya existe en la rama predeterminada. Para esta primera incorporación, el escaneo real se validará mediante los checks del pull request.

El workflow no publica imágenes. La distribución, firma, etiquetado y retención de artefactos se mantienen para la etapa de versionado y mantenimiento.

## Perfil de observabilidad

```powershell
docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  --profile observability `
  up --build --detach
```

Sin `--profile observability`, Compose ejecuta únicamente PostgreSQL, migraciones y API. Con el perfil se añaden Prometheus, Alertmanager y Grafana, todos publicados solo en `127.0.0.1`.

Valida el perfil completo con:

```powershell
.\scripts\test-observability-compose.ps1
```
