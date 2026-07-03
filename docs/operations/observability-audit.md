# Auditoría final de observabilidad

Esta página cierra la etapa 6 de observabilidad de `albion-market-api`. La auditoría no sustituye los smoke tests: documenta qué se exige, dónde vive la evidencia y qué riesgos permanecen fuera del alcance local.

## Alcance auditado

| Bloque | Criterio de aceptación | Evidencia |
|---|---|---|
| 6.1 | logs estructurados, correlación, redacción, liveness y métricas base | tests Go, `/healthz`, `/metrics` y documentación operativa |
| 6.2 | métricas HTTP, readiness, ingesta, PostgreSQL, proceso y build con cardinalidad acotada | `internal/observability`, tests Go y dashboard |
| 6.3 | readiness separada de liveness, timeout acotado, esquema mínimo y recuperación tras caída de PostgreSQL | `scripts/test-deployment-compose.ps1` |
| 6.4 | doce alertas con severidad, ventana, descripción, runbook y prueba semántica | reglas Prometheus, `promtool` y contratos Go |
| 6.5 | Prometheus, Alertmanager y Grafana opcionales, reproducibles y endurecidos | `scripts/test-observability-compose.ps1` |
| 6.6 | trazabilidad automática entre reglas, pruebas, runbooks, smoke test y dashboard | `internal/deployment/observability_contract_test.go` |

## Catálogo cerrado

La auditoría exige exactamente estas doce alertas:

1. `AlbionMarketAPIUnavailable`
2. `AlbionMarketAPINotReady`
3. `AlbionMarketAPIRepeatedRestarts`
4. `AlbionMarketAPIHighHTTP5xxRate`
5. `AlbionMarketAPIHighHTTPLatency`
6. `AlbionMarketAPIAuthenticationFailuresHigh`
7. `AlbionMarketAPIIngestTrafficStopped`
8. `AlbionMarketAPINoSuccessfulIngest`
9. `AlbionMarketAPIIngestErrorsRepeated`
10. `AlbionMarketAPIIngestPersistenceErrorsRepeated`
11. `AlbionMarketAPIDatabasePoolSaturated`
12. `AlbionMarketAPIDatabaseAcquireSlow`

Para cada alerta, los contratos verifican:

- presencia única en el archivo de reglas;
- severidad `critical` o `warning`;
- ventana `for`;
- anotaciones `summary` y `description`;
- `runbook_url` publicado;
- entrada en el catálogo de alertas;
- sección de respuesta dedicada;
- caso ejecutable en `promtool test rules`;
- carga comprobada por el smoke test runtime.

## Dashboard auditado

El dashboard `Albion Market API · Overview` debe conservar doce paneles:

- disponibilidad y readiness;
- utilización del pool;
- antigüedad de la última ingesta exitosa;
- solicitudes y errores HTTP;
- latencia HTTP p95;
- resultados y entradas de ingesta;
- latencia p95 de CopyFrom y upsert;
- conexiones y errores PostgreSQL.

Los contratos comprueban los títulos y las métricas principales usadas por cada panel. Un cambio de nombre, métrica o panel exige actualizar simultáneamente el dashboard y su contrato.

## Validación reproducible

Desde la raíz del repositorio:

```powershell
gofmt -w .

go mod verify
go test ./...
go vet ./...

npm ci
npm run contracts:check
npm run docs:check

.\scripts\test-container.ps1
.\scripts\test-deployment-compose.ps1
.\scripts\test-observability-compose.ps1
```

El cierre es válido únicamente cuando todos los comandos terminan correctamente y no quedan contenedores temporales del smoke test.

## Evidencia mínima del smoke test

El resultado esperado incluye:

```text
[OK] API readiness is ready.
[OK] Prometheus is ready.
[OK] Alertmanager is ready.
[OK] Grafana is ready.
[OK] Prometheus target 'albion-market-api' is up.
[12/12] Observability smoke test completed.
```

Además, el script debe comprobar:

- puertos publicados solo en `127.0.0.1`;
- las doce reglas cargadas;
- API de Alertmanager disponible;
- dashboard provisionado;
- filesystem raíz de solo lectura;
- capacidades Linux eliminadas;
- `no-new-privileges`.

## Mantenimiento obligatorio

Cuando cambie una métrica o alerta, actualiza en el mismo PR:

1. instrumentación Go;
2. documentación de métricas;
3. regla Prometheus;
4. prueba `promtool`;
5. runbook;
6. dashboard;
7. contratos Go;
8. smoke test, cuando corresponda.

No se acepta una alerta nueva sin runbook y prueba semántica. Tampoco se elimina una métrica usada por una regla o panel sin actualizar sus consumidores.

## Riesgos residuales

El cierre local deja explícitamente fuera de alcance:

- alta disponibilidad de Prometheus, Alertmanager o Grafana;
- almacenamiento remoto de métricas;
- autenticación de Grafana expuesta fuera de loopback;
- envío real de avisos a correo, Slack, PagerDuty u otro receptor;
- trazas distribuidas;
- SLO y umbrales calibrados con tráfico productivo.

Los umbrales actuales son una base inicial. Antes de establecer guardias reales deben revisarse con datos representativos de volumen, latencia e ingesta.

## Decisión de cierre

La etapa 6 puede considerarse cerrada cuando:

- la matriz anterior está cubierta;
- las doce pruebas de reglas pasan;
- el target de la API aparece `up`;
- el dashboard se provisiona;
- Alertmanager conserva `local-null` como receptor seguro por defecto;
- los contratos y la documentación compilan;
- `develop` y `main` reciben el cambio mediante CI verde.
