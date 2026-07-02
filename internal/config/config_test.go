package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSecretFromEnvOrFileReadsPrivateFile(t *testing.T) {

	path := filepath.Join(t.TempDir(), "ingest.token")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 48)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_SECRET_VALUE", "")
	t.Setenv("TEST_SECRET_FILE", path)

	value, source, err := secretFromEnvOrFile("TEST_SECRET_VALUE", "TEST_SECRET_FILE", "production")
	if err != nil {
		t.Fatal(err)
	}
	if value != strings.Repeat("a", 48) || source != "file" {
		t.Fatalf("value length=%d source=%q", len(value), source)
	}
}

func TestSecretFromEnvOrFileRejectsAmbiguousSources(t *testing.T) {

	path := filepath.Join(t.TempDir(), "ingest.token")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 48)), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_SECRET_VALUE", strings.Repeat("b", 48))
	t.Setenv("TEST_SECRET_FILE", path)

	if _, _, err := secretFromEnvOrFile("TEST_SECRET_VALUE", "TEST_SECRET_FILE", "development"); err == nil {
		t.Fatal("expected ambiguous secret source error")
	}
}

func TestValidateCredentialRejectsWeakOrPlaceholderTokens(t *testing.T) {

	for _, token := range []string{
		"short",
		"CHANGE_ME_STRONG_RANDOM_TOKEN_123456789",
		strings.Repeat("a", 20) + " ",
	} {
		if err := validateCredential("current", token, 32); err == nil {
			t.Fatalf("token %q was accepted", token)
		}
	}
	if err := validateCredential("invalid id", strings.Repeat("a", 48), 32); err == nil {
		t.Fatal("invalid credential id was accepted")
	}
}

func TestLoadIngestCredentialsSupportsRotation(t *testing.T) {
	currentPath := filepath.Join(t.TempDir(), "current.token")
	previousPath := filepath.Join(t.TempDir(), "previous.token")
	if err := os.WriteFile(currentPath, []byte(strings.Repeat("a", 48)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(previousPath, []byte(strings.Repeat("b", 48)), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{
		"INGEST_BEARER_TOKEN",
		"INGEST_BEARER_TOKEN_PREVIOUS",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("INGEST_BEARER_TOKEN_FILE", currentPath)
	t.Setenv("INGEST_BEARER_TOKEN_PREVIOUS_FILE", previousPath)
	t.Setenv("INGEST_BEARER_TOKEN_ID", "receiver-2026-07")
	t.Setenv("INGEST_BEARER_TOKEN_PREVIOUS_ID", "receiver-2026-06")

	credentials, sources, err := loadIngestCredentials("production", 32)
	if err != nil {
		t.Fatal(err)
	}
	if len(credentials) != 2 || len(sources) != 2 {
		t.Fatalf("credentials=%d sources=%d, want 2", len(credentials), len(sources))
	}
	if credentials[0].ID != "receiver-2026-07" || credentials[1].ID != "receiver-2026-06" {
		t.Fatalf("credential ids = %q, %q", credentials[0].ID, credentials[1].ID)
	}
	if sources[0].Source != "file" || sources[1].Source != "file" {
		t.Fatalf("sources = %+v, want files", sources)
	}
}

func TestSecretFromEnvOrFileSupportsDatabaseURLFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "database-url")
	want := "postgres://albion_market:secret@postgres:5432/albion_market?sslmode=disable"
	if err := os.WriteFile(path, []byte(want+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL", "")
	t.Setenv("DATABASE_URL_FILE", path)

	value, source, err := secretFromEnvOrFile("DATABASE_URL", "DATABASE_URL_FILE", "production")
	if err != nil {
		t.Fatal(err)
	}
	if value != want || source != "file" {
		t.Fatalf("value=%q source=%q, want database URL from file", value, source)
	}
}

func TestValidateSecretFilePermissionsAcceptsRuntimeManagedSecrets(t *testing.T) {
	if err := validateSecretFilePermissions("/run/secrets/database_url", 0o444, "production"); err != nil {
		t.Fatalf("runtime-managed secret was rejected: %v", err)
	}
}

func TestValidateSecretFilePermissionsRejectsBroadHostFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not enforce POSIX secret file permissions")
	}
	if err := validateSecretFilePermissions("/tmp/database-url", 0o644, "production"); err == nil {
		t.Fatal("expected broad host file permissions to be rejected")
	}
	if err := validateSecretFilePermissions("/tmp/database-url", 0o600, "production"); err != nil {
		t.Fatalf("private host file was rejected: %v", err)
	}
}
