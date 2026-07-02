# Mantener la documentación

La documentación sigue el principio **docs as code**: Markdown versionado, revisión en pull requests y despliegue automatizado.

## Fuente única

- `README.md` es la portada breve de GitHub;
- toda guía extensa vive en `docs/`;
- `docs/.vitepress/config.mts` define navegación y búsqueda;
- `migrations/` contiene únicamente archivos SQL;
- resultados, logs, dumps y reportes viven bajo `artifacts/` y no se versionan.

## Desarrollo local

```powershell
npm ci
npm run docs:dev
```

Validación de producción y contratos:

```powershell
npm run openapi:lint
npm run docs:build
npm run docs:preview
```

## Dónde agregar contenido

| Tema | Carpeta |
|---|---|
| Primeros pasos y arquitectura | `docs/guide/` |
| Contratos HTTP | `docs/api/` |
| PostgreSQL | `docs/database/` |
| Operación | `docs/operations/` |
| Seguridad | `docs/security/` |
| Pruebas | `docs/testing/` |
| Catálogos técnicos | `docs/reference/` |

## Reglas del pull request

1. Actualiza documentación en el mismo PR que cambia comportamiento público u operativo.
2. Añade la página al sidebar si debe ser descubrible.
3. Usa enlaces relativos entre páginas del portal.
4. No copies documentación dentro de carpetas de código.
5. Ejecuta `npm run openapi:lint` y `npm run docs:build` antes de hacer push.

El workflow `documentation.yml` valida cada cambio relevante y despliega a GitHub Pages únicamente desde `main`.
