# Pruebas de contratos

El archivo `openapi/openapi.yaml` es la fuente canónica del contrato HTTP. Los
checks automáticos evitan que una modificación aislada en OpenAPI o Go llegue a
`develop` sin una señal de divergencia.

## Comprobación local

Instala las dependencias reproducibles del portal y del linter:

```powershell
npm ci
```

Ejecuta todo el bloque de contratos:

```powershell
npm run contracts:check
```

También puede ejecutarse cada capa por separado:

```powershell
npm run openapi:lint
go test ./internal/contracts -run TestOpenAPI -count=1
```

## Lint OpenAPI

`redocly.yaml` extiende el conjunto recomendado de Redocly CLI. El lint comprueba
la estructura OpenAPI 3.1, referencias, identificadores de operación, respuestas,
seguridad declarada y otras reglas de consistencia.

La regla de licencia se desactiva deliberadamente mientras el repositorio no
publique una licencia explícita. No se usa un archivo de exclusiones para ocultar
problemas conocidos.

## Divergencia OpenAPI ↔ Go

`internal/contracts/openapi_contract_test.go` analiza el código fuente con los
paquetes estándar `go/parser` y `go/ast`.

La prueba de rutas obtiene las llamadas `mux.HandleFunc` de
`internal/server/router.go`, identifica el guard de método de cada handler y
compara el inventario resultante con `paths` de OpenAPI. Falla ante una ruta o
método agregado, eliminado o cambiado solo en uno de los dos lados.

La prueba de esquemas usa reflexión sobre los DTO exportados de
`internal/domain/market.go`. Compara sus tags `json` con las propiedades de los
esquemas públicos e internos seleccionados. Los campos `json:"-"` se excluyen,
por lo que los IDs internos siguen sin formar parte de las respuestas públicas.

## Integración continua

El workflow `.github/workflows/contracts.yml` ejecuta:

1. `npm ci`;
2. `npm run openapi:lint`;
3. descarga y verificación de módulos Go;
4. las pruebas específicas de divergencia.

Se ejecuta en pull requests y pushes hacia `develop` y `main`, sin filtros de
rutas. Así el check siempre informa estado y puede configurarse como requisito
de protección de rama sin quedar pendiente por un workflow omitido.

## Regla para cambios futuros

Cuando se modifica una ruta, método o DTO HTTP, el mismo pull request debe
actualizar el handler o tipo Go, `openapi/openapi.yaml`, las pruebas afectadas y
la documentación funcional. No se debe debilitar el linter ni borrar una
comparación para hacer pasar un cambio incompatible.
