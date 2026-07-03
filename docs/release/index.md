# Releases y distribución

La API se distribuye como una imagen OCI publicada en GitHub Container Registry y como un GitHub Release asociado a un tag SemVer estable.

## Artefactos publicados

Cada tag `vMAJOR.MINOR.PATCH` válido genera:

- imagen `ghcr.io/nachodev-ui/albion-market-api:<versión>`;
- alias `<major>.<minor>` y `latest`;
- digest inmutable `sha256:...`;
- SBOM SPDX JSON generado desde la imagen final;
- archivo `release-metadata.json` con tag, commit, imagen y digest;
- `SHA256SUMS` para verificar los archivos descargables;
- firma keyless de Cosign con identidad OIDC de GitHub Actions;
- attestations firmadas de provenance y SBOM;
- GitHub Release con notas generadas y todos los archivos de evidencia.

La referencia recomendada para producción es siempre el digest, no `latest`:

```powershell
$Image = "ghcr.io/nachodev-ui/albion-market-api@sha256:REEMPLAZAR_DIGEST"
docker pull $Image
docker inspect $Image
```

## Visibilidad inicial de GHCR

GitHub crea inicialmente los paquetes del Container registry con visibilidad privada. El workflow puede publicar y verificar la imagen con `GITHUB_TOKEN`, pero los consumidores externos deben autenticarse hasta que el propietario cambie una sola vez la visibilidad del paquete a **Public** desde `Profile → Packages → albion-market-api → Package settings → Change visibility`.

Mientras el paquete permanezca privado:

```bash
echo "$GHCR_TOKEN" | docker login ghcr.io -u nachodev-ui --password-stdin
docker pull ghcr.io/nachodev-ui/albion-market-api@sha256:REEMPLAZAR_DIGEST
```

No automatices ese cambio de visibilidad desde el workflow de release: es una decisión administrativa irreversible para el paquete actual.

## Política de versionado

Se usa versionado semántico:

| Cambio | Incremento | Ejemplo |
|---|---|---|
| Corrección compatible | PATCH | `v1.2.3` → `v1.2.4` |
| Funcionalidad compatible | MINOR | `v1.2.4` → `v1.3.0` |
| Cambio incompatible | MAJOR | `v1.3.0` → `v2.0.0` |

Mientras el proyecto permanezca en `0.x`, un incremento MINOR puede incluir cambios incompatibles y debe explicarse explícitamente en las notas del release.

El workflow rechaza:

- tags que no coincidan exactamente con `vMAJOR.MINOR.PATCH`;
- componentes con ceros iniciales;
- tags que no apunten al commit vigente de `main`;
- publicaciones cuyo build, SBOM, firma, verificación o attestation falle.

## Procedimiento de release

Antes de etiquetar:

1. fusiona la rama de trabajo en `develop`;
2. espera Quality checks, API contracts y Container checks;
3. abre y fusiona `develop → main`;
4. confirma que `main` esté verde y que la documentación se haya publicado;
5. selecciona la siguiente versión SemVer.

Desde Git Bash:

```bash
git fetch origin --prune --tags
git switch main
git pull --ff-only origin main

git status --short
git log --oneline -5

git tag -a v0.1.0 -m "release: v0.1.0"
git push origin v0.1.0
```

No muevas ni reutilices un tag publicado. Si el workflow falla antes de crear el release, corrige la causa en una rama nueva, promueve nuevamente a `main` y publica una versión PATCH nueva.

## Verificación del consumidor

### Firma Cosign

```bash
IMAGE="ghcr.io/nachodev-ui/albion-market-api@sha256:REEMPLAZAR_DIGEST"
TAG="v0.1.0"

cosign verify \
  --certificate-identity "https://github.com/nachodev-ui/albion-market-api/.github/workflows/release.yml@refs/tags/${TAG}" \
  --certificate-oidc-issuer "https://token.actions.githubusercontent.com" \
  "${IMAGE}"
```

### Attestations de GitHub

```bash
gh attestation verify \
  "oci://ghcr.io/nachodev-ui/albion-market-api@sha256:REEMPLAZAR_DIGEST" \
  --repo nachodev-ui/albion-market-api
```

### Archivos del release

```bash
sha256sum --check SHA256SUMS
```

La verificación debe realizarse antes de promover un digest a producción.

## Fallos y recuperación

- Un fallo antes del push no publica una imagen utilizable.
- Un fallo posterior al push puede dejar un digest en GHCR sin GitHub Release; no debe desplegarse.
- El artefacto temporal `release-evidence-<versión>` se conserva 90 días para diagnóstico.
- Nunca corrijas un release sobrescribiendo su tag o digest.

Consulta el [runbook de rollback](./rollback) y la [política de mantenimiento](./maintenance).
