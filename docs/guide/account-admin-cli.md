# Administración manual de acceso Pro

`account-admin` concede y retira acceso Pro sin depender del proveedor de pagos. Las mutaciones son transaccionales, idempotentes y generan eventos de auditoría append-only.

## Seguridad operativa

- Usa exactamente un selector: UUID local, correo o `sub` de Auth0.
- El correo falla si coincide con más de una cuenta.
- `--actor` y `--reason` son obligatorios y quedan auditados.
- En `APP_ENV=production`, toda mutación exige `--confirm-production PRODUCTION`.
- `--dry-run` calcula el resultado sin escribir suscripciones ni auditoría.
- Un segundo `grant-pro` sobre un grant manual activo es un no-op.
- Un segundo `revoke-pro` sobre un grant ya retirado es un no-op.
- Retirar el grant manual no elimina una suscripción activa de Lemon Squeezy u otro proveedor.

## Compilar en Windows

Desde la raíz del repositorio, en PowerShell:

```powershell
New-Item -ItemType Directory -Force .\bin | Out-Null
go build -trimpath -o .\bin\account-admin.exe .\cmd\account-admin
```

La herramienta lee `DATABASE_URL` o `DATABASE_URL_FILE`. No imprime la cadena de conexión.

## Consultar una cuenta

```powershell
$env:APP_ENV = "production"
$env:DATABASE_URL = "<Neon pooled connection string>"

.\bin\account-admin.exe status `
  --email "qa@example.com"
```

## Vista previa de un grant

```powershell
.\bin\account-admin.exe grant-pro `
  --email "qa@example.com" `
  --duration "30d" `
  --actor "Ignacio Cisternas" `
  --reason "Acceso beta privado" `
  --dry-run
```

## Conceder Pro

```powershell
.\bin\account-admin.exe grant-pro `
  --email "qa@example.com" `
  --duration "30d" `
  --actor "Ignacio Cisternas" `
  --reason "Acceso beta privado" `
  --confirm-production "PRODUCTION"
```

Después, el usuario debe pulsar **Actualizar permisos** en `/account`. La API resolverá el plan `pro` y devolverá sus entitlements.

## Retirar Pro

```powershell
.\bin\account-admin.exe revoke-pro `
  --email "qa@example.com" `
  --actor "Ignacio Cisternas" `
  --reason "Fin del acceso beta" `
  --confirm-production "PRODUCTION"
```

Si no existe otra suscripción vigente, la siguiente consulta de cuenta volverá a `free`.

## Listar grants activos

```powershell
.\bin\account-admin.exe list-active-manual-grants --limit 100
```

## Verificación Free → Pro → Free

Este comando crea un usuario sintético, concede Pro, valida un entitlement Pro, retira Pro, valida Free y revierte toda la transacción. No deja usuarios, suscripciones ni auditorías persistentes.

```powershell
.\bin\account-admin.exe verify-lifecycle `
  --actor "deployment-verifier" `
  --reason "Hito 4A production verification" `
  --confirm-production "PRODUCTION"
```

La salida correcta contiene:

```json
{
  "ok": true,
  "result": {
    "freeBefore": { "plan": "free", "status": "none" },
    "proGranted": { "plan": "pro", "status": "active" },
    "freeAfter": { "plan": "free", "status": "none" },
    "auditEvents": 2,
    "rolledBack": true
  }
}
```

## Auditoría

Cada cambio real inserta una fila en `account_admin_audit_events` con:

- usuario afectado;
- administrador actor;
- acción `grant_pro` o `revoke_pro`;
- motivo;
- estado efectivo anterior y posterior;
- fecha UTC.

Las reglas de la tabla convierten `UPDATE` y `DELETE` en no-ops. Las vistas operativas deben consultar el historial, nunca reescribirlo.
