package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDatabaseURLFromEnvironmentUsesDirectValue(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example.test/albion")
	t.Setenv("DATABASE_URL_FILE", "")

	value, err := databaseURLFromEnvironment()
	if err != nil {
		t.Fatalf("databaseURLFromEnvironment() error = %v", err)
	}
	if value != "postgres://example.test/albion" {
		t.Fatalf("databaseURLFromEnvironment() = %q", value)
	}
}

func TestDatabaseURLFromEnvironmentUsesFile(t *testing.T) {
	t.Setenv("DATABASE_URL", "")
	directory := t.TempDir()
	path := filepath.Join(directory, "database-url")
	if err := os.WriteFile(path, []byte("postgres://example.test/albion\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATABASE_URL_FILE", path)

	value, err := databaseURLFromEnvironment()
	if err != nil {
		t.Fatalf("databaseURLFromEnvironment() error = %v", err)
	}
	if value != "postgres://example.test/albion" {
		t.Fatalf("databaseURLFromEnvironment() = %q", value)
	}
}

func TestDatabaseURLFromEnvironmentRejectsAmbiguousConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://example.test/albion")
	t.Setenv("DATABASE_URL_FILE", "/tmp/database-url")

	_, err := databaseURLFromEnvironment()
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("databaseURLFromEnvironment() error = %v", err)
	}
}

func TestListMigrationFilesReturnsSortedSQLFiles(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"000003_last.sql", "README.md", "000001_first.sql", "000002_middle.sql"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("select 1;"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(directory, "000000_directory.sql"), 0o700); err != nil {
		t.Fatal(err)
	}

	files, err := listMigrationFiles(directory)
	if err != nil {
		t.Fatalf("listMigrationFiles() error = %v", err)
	}

	want := []string{
		filepath.Join(directory, "000001_first.sql"),
		filepath.Join(directory, "000002_middle.sql"),
		filepath.Join(directory, "000003_last.sql"),
	}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("listMigrationFiles() = %#v, want %#v", files, want)
	}
}

func TestListMigrationFilesRejectsEmptyDirectory(t *testing.T) {
	_, err := listMigrationFiles(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "no SQL migrations") {
		t.Fatalf("listMigrationFiles() error = %v", err)
	}
}
