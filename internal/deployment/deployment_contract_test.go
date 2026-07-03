package deployment

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

const (
	goBuilderImage = "golang:1.26.4-bookworm@sha256:b305420a68d0f229d91eb3b3ed9e519fcf2cf5461da4bef997bf927e8c0bfd2b"
	postgresImage  = "postgres:17.10-alpine3.23@sha256:3da929dcc3e63e3f0cc81fdb114c073ca48bfc7280e83a6324d5652fbee63742"
)

func TestContainerBaseImagesArePinnedByDigest(t *testing.T) {
	dockerfile := readProjectFile(t, "Dockerfile")
	compose := readProjectFile(t, "deploy", "compose.yaml")

	if !strings.Contains(dockerfile, "FROM "+goBuilderImage+" AS builder") {
		t.Fatalf("Dockerfile builder must use %s", goBuilderImage)
	}
	if count := strings.Count(compose, "image: "+postgresImage); count != 2 {
		t.Fatalf("compose PostgreSQL image count=%d, want 2 pinned references", count)
	}

	fromPattern := regexp.MustCompile(`(?m)^FROM\s+([^\s]+)`) // Dockerfile stages only.
	for _, match := range fromPattern.FindAllStringSubmatch(dockerfile, -1) {
		image := match[1]
		if image == "scratch" {
			continue
		}
		if !strings.Contains(image, "@sha256:") {
			t.Fatalf("Dockerfile base image is not digest-pinned: %s", image)
		}
	}

	imagePattern := regexp.MustCompile(`(?m)^\s+image:\s+([^\s]+)\s*$`)
	for _, match := range imagePattern.FindAllStringSubmatch(compose, -1) {
		image := strings.Trim(imagePattern.FindStringSubmatch(match[0])[1], `"'`)
		if strings.HasPrefix(image, "${") {
			continue // Locally built API artifact, not an external base image.
		}
		if !strings.Contains(image, "@sha256:") {
			t.Fatalf("Compose external image is not digest-pinned: %s", image)
		}
	}

	if strings.Contains(strings.ToLower(dockerfile+compose), ":latest") {
		t.Fatal("deployment files must not use floating latest tags")
	}
}

func TestComposeRequiresSuccessfulMigrationsBeforeAPI(t *testing.T) {
	compose := readProjectFile(t, "deploy", "compose.yaml")
	api := composeServiceBlock(t, compose, "api")
	migrate := composeServiceBlock(t, compose, "migrate")

	requireContains(t, api, "condition: service_completed_successfully")
	requireContains(t, api, "DATABASE_URL_FILE: /run/secrets/database_url")
	requireContains(t, api, "INGEST_BEARER_TOKEN_FILE: /run/secrets/ingest_token")
	requireContains(t, api, "read_only: true")
	requireContains(t, api, "- ALL")
	requireContains(t, api, "- no-new-privileges:true")
	requireContains(t, api, "127.0.0.1:${API_HOST_PORT:-8080}:8080")
	requireContains(t, api, `user: "65532:65532"`)

	requireContains(t, migrate, "restart: \"no\"")
	requireContains(t, migrate, "read_only: true")
	requireContains(t, migrate, "../migrations:/migrations:ro")
	requireContains(t, migrate, "ON_ERROR_STOP=1")
	requireContains(t, migrate, "test \"$${migration_count}\" -gt 0")
}

func TestDeploymentSecretsAreFileBackedAndExcludedFromBuild(t *testing.T) {
	compose := readProjectFile(t, "deploy", "compose.yaml")
	dockerignore := readProjectFile(t, ".dockerignore")
	gitignore := readProjectFile(t, ".gitignore")

	for _, expected := range []string{
		"POSTGRES_PASSWORD_FILE: /run/secrets/postgres_password",
		`file: "${POSTGRES_PASSWORD_SECRET_FILE:-../secrets/deployment/postgres-password}"`,
		`file: "${DATABASE_URL_SECRET_FILE:-../secrets/deployment/database-url}"`,
		`file: "${INGEST_TOKEN_SECRET_FILE:-../secrets/deployment/ingest-current.token}"`,
	} {
		requireContains(t, compose, expected)
	}

	requireContains(t, dockerignore, "secrets/")
	requireContains(t, dockerignore, "deploy/")
	requireContains(t, gitignore, "secrets/")
	requireContains(t, gitignore, "/deploy/compose.env.local")
}

func TestLinuxSecretFilesSupportNonRootComposeRuntime(t *testing.T) {
	for _, scriptPath := range [][]string{
		{"scripts", "initialize-deployment.ps1"},
		{"scripts", "test-container.ps1"},
	} {
		script := readProjectFile(t, scriptPath...)
		requireContains(t, script, "& chmod 700 $Parent")
		requireContains(t, script, "& chmod 444 $Path")
		if strings.Contains(script, "& chmod 600 $Path") {
			t.Fatalf("%s must not create file-backed Compose secrets readable only by the host UID", filepath.Join(scriptPath...))
		}
	}

	compose := readProjectFile(t, "deploy", "compose.yaml")
	migrate := composeServiceBlock(t, compose, "migrate")
	requireContains(t, migrate, "command:\n      - |")
	requireContains(t, migrate, `echo "Applying $${migration##*/}"`)
}

func TestContainerHealthcheckUsesLiveness(t *testing.T) {
	dockerfile := readProjectFile(t, "Dockerfile")
	requireContains(t, dockerfile, "HEALTHCHECK_URL=http://127.0.0.1:8080/healthz")
	if strings.Contains(dockerfile, "HEALTHCHECK_URL=http://127.0.0.1:8080/readyz") {
		t.Fatal("container healthcheck must not restart the API for a transient PostgreSQL outage")
	}
}

func TestReadinessMigrationPublishesExpectedSchemaVersion(t *testing.T) {
	migration := readProjectFile(t, "migrations", "000006_observability_readiness.sql")
	for _, expected := range []string{
		"create table if not exists app_schema_state",
		"singleton boolean primary key",
		"values (true, 6, now())",
		"version = greatest(app_schema_state.version, excluded.version)",
	} {
		requireContains(t, strings.ToLower(migration), strings.ToLower(expected))
	}
}

func readProjectFile(t *testing.T, parts ...string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("could not resolve deployment test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	path := filepath.Join(append([]string{root}, parts...)...)
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	value := strings.ReplaceAll(string(content), "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func composeServiceBlock(t *testing.T, compose, service string) string {
	t.Helper()
	lines := strings.Split(compose, "\n")
	start := -1
	serviceHeader := "  " + service + ":"
	for index, line := range lines {
		if line == serviceHeader {
			start = index + 1
			break
		}
	}
	if start == -1 {
		t.Fatalf("service %q was not found", service)
	}

	end := len(lines)
	nextService := regexp.MustCompile(`^  [A-Za-z0-9_-]+:$`)
	for index := start; index < len(lines); index++ {
		if nextService.MatchString(lines[index]) || (lines[index] != "" && !strings.HasPrefix(lines[index], " ")) {
			end = index
			break
		}
	}
	return strings.Join(lines[start:end], "\n")
}

func requireContains(t *testing.T, value, expected string) {
	t.Helper()
	if !strings.Contains(value, expected) {
		t.Fatalf("expected deployment contract to contain %q", expected)
	}
}
