# Participación de `albion-market-api` en la prueba end-to-end

La prueba integrada se ejecuta desde el repositorio
`albion-market-data-platform` mediante:

```powershell
.\scripts\e2e-three-projects.ps1 `
  -DatabaseUrl "postgres://postgres:TU_CLAVE@localhost:5432/albion_market_e2e?sslmode=disable"
```

El arnés inicia esta API en `127.0.0.1:18080` con un token y una base PostgreSQL
exclusivos para la ejecución. Antes de comenzar aplica, en orden, todos los
archivos de `migrations/` y vacía únicamente esa base dedicada.

Las comprobaciones específicas de este repositorio son:

- autenticación del receiver;
- idempotencia por `request_id` para precios e historial;
- una sola fila raw por batch repetido;
- una sola fila caliente por combinación de precio;
- una sola fila por bucket histórico lógico;
- reemplazo del bucket cuando llega una captura más nueva;
- lectura pública por `marketKey` sin IDs numéricos de ubicación;
- métricas separadas de `ingest` y `history_ingest` en `/api/v1/status`.

No debe ejecutarse contra la base de desarrollo diaria. Por seguridad, el
orquestador rechaza nombres que no contengan `e2e` o `test`, salvo que se use de
forma explícita `-AllowNonE2EDatabase`.
