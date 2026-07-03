# Política de mantenimiento

## Ramas y soporte

- `main` representa el estado publicable y estable.
- `develop` integra el siguiente conjunto de cambios.
- Las ramas `feat/*`, `fix/*`, `test/*` y `release/v*` son temporales; una solicitud válida elimina automáticamente su rama `release/v*`.
- Solo los tags SemVer creados desde el `main` vigente son releases soportados.
- La versión soportada por defecto es la última release estable.
- Una versión anterior recibe correcciones únicamente cuando una regresión impide actualizar inmediatamente o existe una vulnerabilidad crítica.

Las migraciones publicadas son inmutables. Toda corrección de esquema se entrega como una migración nueva y ascendente.

## Cadencia

| Actividad | Frecuencia |
|---|---|
| Dependencias Go, npm, Docker y GitHub Actions | revisión semanal mediante Dependabot |
| Vulnerabilidades HIGH/CRITICAL corregibles | bloqueo continuo en CI |
| Restauración de backup | semanal según el runbook PostgreSQL |
| Revisión de imágenes base y acciones fijadas | al menos mensual |
| Revisión de documentación y runbooks | en cada cambio operativo |
| Release funcional | cuando `develop → main` haya sido aprobado y exista valor publicable |

Dependabot abre sus PR contra `develop`; cada actualización debe superar el mismo CI que una modificación funcional.

## Actualizaciones de seguridad

Prioridad operativa:

1. credenciales o secretos expuestos: rotación inmediata;
2. vulnerabilidad crítica explotable: parche y PATCH release urgente;
3. vulnerabilidad alta corregible: resolver antes del siguiente release;
4. vulnerabilidad sin corrección: documentar riesgo, mitigación y seguimiento.

No se relaja Trivy con `continue-on-error` ni se elimina una regla contractual para hacer pasar un release.

## Retención

- Los tags Git y GitHub Releases son permanentes.
- Los digests publicados no se sobrescriben.
- Los artefactos de evidencia del workflow se conservan 90 días.
- El SBOM, `release-metadata.json` y `SHA256SUMS` quedan adjuntos al release sin vencimiento operativo.
- Los alias `latest` y `<major>.<minor>` son conveniencias; no constituyen una referencia inmutable.

## Fin de soporte y cambios incompatibles

Un cambio incompatible requiere una versión MAJOR, o MINOR mientras el proyecto sea `0.x`. Las notas deben incluir:

- comportamiento retirado o modificado;
- impacto en clientes y despliegues;
- migraciones requeridas;
- estrategia de rollback;
- fecha o versión desde la que deja de mantenerse la compatibilidad anterior.

No se elimina un endpoint o variable de configuración soportada sin documentación previa y una ruta de migración razonable.
