# Runbook de rollback

Un rollback cambia la referencia desplegada a un digest anterior ya verificado. No reconstruye ni vuelve a etiquetar la imagen afectada.

## Cuándo ejecutarlo

Inicia rollback si una versión recién desplegada provoca alguno de estos síntomas:

- `/healthz` deja de responder;
- `/readyz` permanece en estado no preparado;
- aumenta la tasa de errores HTTP 5xx;
- se detiene la ingesta exitosa;
- aparecen errores repetidos de persistencia;
- se confirma una regresión funcional o de seguridad.

## Preparación

1. identifica el último digest estable;
2. verifica su firma y attestations;
3. confirma que su versión es compatible con el esquema PostgreSQL vigente;
4. conserva logs, métricas, release metadata y digest de la versión defectuosa.

Nunca reviertas migraciones destructivas automáticamente. El rollback de aplicación y el rollback de datos son decisiones separadas.

## Procedimiento

```powershell
$PreviousImage = "ghcr.io/nachodev-ui/albion-market-api@sha256:DIGEST_ESTABLE"

docker pull $PreviousImage
```

Actualiza la referencia de imagen utilizada por el entorno y recrea únicamente el servicio API. Para el Compose del repositorio, establece `API_IMAGE` en `deploy/compose.env.local`:

```powershell
$env:API_IMAGE = $PreviousImage

docker compose `
  --env-file .\deploy\compose.env.local `
  --file .\deploy\compose.yaml `
  up --detach --no-build api
```

Valida inmediatamente:

```powershell
Invoke-RestMethod http://127.0.0.1:18080/healthz
Invoke-RestMethod http://127.0.0.1:18080/readyz
(Invoke-WebRequest http://127.0.0.1:18080/metrics).Content
```

Después comprueba:

- estado de contenedores;
- logs estructurados de la API;
- alertas activas;
- conexión y pool PostgreSQL;
- ingesta autenticada de prueba;
- consultas de precios e historial.

## Cierre del incidente

Registra:

- versión y digest retirados;
- versión y digest restaurados;
- hora de inicio y término;
- alertas observadas;
- causa raíz conocida o hipótesis;
- datos afectados;
- acción correctiva y versión PATCH prevista.

La versión defectuosa no se elimina: se conserva para auditoría, pero no debe volver a desplegarse.
