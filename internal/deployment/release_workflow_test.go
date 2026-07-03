package deployment

import (
	"regexp"
	"strings"
	"testing"
)

const (
	checkoutActionCommit  = "9c091bb21b7c1c1d1991bb908d89e4e9dddfe3e0"
	buildxActionCommit    = "d7f5e7f509e45cec5c76c4d5afdd7de93d0b3df5"
	loginActionCommit     = "650006c6eb7dba73a995cc03b0b2d7f5ca915bee"
	metadataActionCommit  = "80c7e94dd9b9319bd5eb7a0e0fe9291e23a2a2e9"
	buildPushActionCommit = "f9f3042f7e2789586610d6e8b85c8f03e5195baf"
	sbomActionCommit      = "e22c389904149dbc22b58101806040fa8d37a610"
	cosignActionCommit    = "6f9f17788090df1f26f669e9d70d6ae9567deba6"
	attestActionCommit    = "a1948c3f048ba23858d222213b7c278aabede763"
	uploadActionCommit    = "043fb46d1a93c77aae656e7c1c64a875d1fc6a0a"
)

func TestReleaseWorkflowPublishesSignedImmutableEvidence(t *testing.T) {
	workflow := readProjectFile(t, ".github", "workflows", "release.yml")

	for _, expected := range []string{
		"workflow_dispatch:",
		"tags:\n      - v*.*.*",
		"cancel-in-progress: false",
		"github.ref_type == 'tag'",
		"packages: write",
		"id-token: write",
		"attestations: write",
		"artifact-metadata: write",
		"^v(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$",
		"must point to the current main commit",
		"ghcr.io/${{ github.repository }}",
		"platforms: linux/amd64",
		"push: true",
		"provenance: mode=max",
		"sbom: true",
		"format: spdx-json",
		"syft-version: v1.42.3",
		"cosign-release: v3.0.6",
		"cosign sign --yes",
		"cosign verify",
		"--certificate-oidc-issuer \"https://token.actions.githubusercontent.com\"",
		"subject-digest: ${{ steps.build.outputs.digest }}",
		"sbom-path: dist/albion-market-api-${{ steps.release.outputs.version }}.spdx.json",
		"sha256sum",
		"retention-days: 90",
		"gh release create \"${GITHUB_REF_NAME}\"",
		"--verify-tag",
		"--generate-notes",
	} {
		requireContains(t, workflow, expected)
	}

	if strings.Contains(workflow, "continue-on-error: true") {
		t.Fatal("release integrity checks must not be allowed to fail silently")
	}
	if strings.Contains(strings.ToLower(workflow), "cosign_password") || strings.Contains(strings.ToLower(workflow), "private_key") {
		t.Fatal("release signing must remain keyless and must not depend on stored private-key secrets")
	}
}

func TestReleaseWorkflowThirdPartyActionsArePinned(t *testing.T) {
	workflow := readProjectFile(t, ".github", "workflows", "release.yml")

	expected := map[string]string{
		"actions/checkout":           checkoutActionCommit,
		"docker/setup-buildx-action": buildxActionCommit,
		"docker/login-action":        loginActionCommit,
		"docker/metadata-action":     metadataActionCommit,
		"docker/build-push-action":   buildPushActionCommit,
		"anchore/sbom-action":        sbomActionCommit,
		"sigstore/cosign-installer":  cosignActionCommit,
		"actions/attest":             attestActionCommit,
		"actions/upload-artifact":    uploadActionCommit,
	}

	for action, commit := range expected {
		pattern := regexp.MustCompile(`(?m)^\s*uses:\s+` + regexp.QuoteMeta(action) + `@([0-9a-f]{40})(?:\s+#.*)?$`)
		matches := pattern.FindAllStringSubmatch(workflow, -1)
		if len(matches) == 0 {
			t.Fatalf("action %s must be pinned to a full commit SHA", action)
		}
		for _, match := range matches {
			if match[1] != commit {
				t.Fatalf("action %s commit=%s, want %s", action, match[1], commit)
			}
		}
	}
}

func TestReleaseDocumentationCoversVerificationRollbackAndMaintenance(t *testing.T) {
	release := readProjectFile(t, "docs", "release", "index.md")
	rollback := readProjectFile(t, "docs", "release", "rollback.md")
	maintenance := readProjectFile(t, "docs", "release", "maintenance.md")
	dependabot := readProjectFile(t, ".github", "dependabot.yml")

	for _, expected := range []string{
		"vMAJOR.MINOR.PATCH",
		"ghcr.io/nachodev-ui/albion-market-api@sha256:",
		"cosign verify",
		"gh attestation verify",
		"sha256sum --check SHA256SUMS",
		"No muevas ni reutilices un tag publicado",
	} {
		requireContains(t, release, expected)
	}

	for _, expected := range []string{
		"último digest estable",
		"API_IMAGE",
		"/healthz",
		"/readyz",
		"Nunca reviertas migraciones destructivas automáticamente",
	} {
		requireContains(t, rollback, expected)
	}

	for _, expected := range []string{
		"última release estable",
		"Dependabot",
		"HIGH/CRITICAL",
		"90 días",
		"Los digests publicados no se sobrescriben",
	} {
		requireContains(t, maintenance, expected)
	}

	for _, ecosystem := range []string{"gomod", "npm", "docker", "github-actions"} {
		requireContains(t, dependabot, "package-ecosystem: "+ecosystem)
	}
	if count := strings.Count(dependabot, "target-branch: develop"); count != 4 {
		t.Fatalf("Dependabot target-branch count=%d, want 4", count)
	}
}

func TestReleaseRequestWorkflowCreatesImmutableTagAndDispatchesRelease(t *testing.T) {
	workflow := readProjectFile(t, ".github", "workflows", "release-request.yml")

	for _, expected := range []string{
		"create:",
		"github.event.ref_type == 'branch'",
		"startsWith(github.event.ref, 'release/v')",
		"actions: write",
		"contents: write",
		"cancel-in-progress: false",
		"ref: ${{ github.event.ref }}",
		"^v(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)\\.(0|[1-9][0-9]*)$",
		"must point to the current main commit",
		"git ls-remote --exit-code --tags origin \"refs/tags/${tag}\"",
		"must never be moved or reused",
		"git tag -a \"${TAG}\" \"${RELEASE_SHA}\" -m \"release: ${TAG}\"",
		"git push origin \"refs/tags/${TAG}\"",
		"actions/workflows/release.yml/dispatches",
		"-f ref=\"${TAG}\"",
		"git push origin --delete \"${REQUEST_REF}\"",
	} {
		requireContains(t, workflow, expected)
	}

	lower := strings.ToLower(workflow)
	for _, forbidden := range []string{"continue-on-error: true", "personal_access_token", "private_key", "github_pat"} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf("release request workflow must not contain %q", forbidden)
		}
	}
}

func TestReleaseRequestWorkflowActionsArePinned(t *testing.T) {
	workflow := readProjectFile(t, ".github", "workflows", "release-request.yml")
	pattern := regexp.MustCompile(`(?m)^\s*uses:\s+actions/checkout@([0-9a-f]{40})(?:\s+#.*)?$`)
	match := pattern.FindStringSubmatch(workflow)
	if len(match) == 0 {
		t.Fatal("release request checkout action must be pinned to a full commit SHA")
	}
	if match[1] != checkoutActionCommit {
		t.Fatalf("release request checkout commit=%s, want %s", match[1], checkoutActionCommit)
	}
}
