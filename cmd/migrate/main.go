package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	defaultMigrationsDirectory = "/migrations"
	migrationLockID            = int64(2_026_070_901)
)

func main() {
	if err := run(); err != nil {
		log.Printf("migration.failed error=%q", err)
		os.Exit(1)
	}
}

func run() error {
	databaseURL, err := databaseURLFromEnvironment()
	if err != nil {
		return err
	}

	migrationsDirectory := strings.TrimSpace(os.Getenv("MIGRATIONS_DIR"))
	if migrationsDirectory == "" {
		migrationsDirectory = defaultMigrationsDirectory
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	connection, err := pgx.Connect(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer connection.Close(context.Background())

	if _, err := connection.Exec(ctx, "select pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		unlockCtx, unlockCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer unlockCancel()
		if _, unlockErr := connection.Exec(unlockCtx, "select pg_advisory_unlock($1)", migrationLockID); unlockErr != nil {
			log.Printf("migration.unlock_failed error=%q", unlockErr)
		}
	}()

	migrationFiles, err := listMigrationFiles(migrationsDirectory)
	if err != nil {
		return err
	}

	for _, migrationFile := range migrationFiles {
		if err := applyMigration(ctx, connection, migrationFile); err != nil {
			return err
		}
	}

	log.Printf("migration.completed count=%d", len(migrationFiles))
	return nil
}

func databaseURLFromEnvironment() (string, error) {
	value := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	filePath := strings.TrimSpace(os.Getenv("DATABASE_URL_FILE"))

	if value != "" && filePath != "" {
		return "", errors.New("DATABASE_URL and DATABASE_URL_FILE are mutually exclusive")
	}
	if value != "" {
		return value, nil
	}
	if filePath == "" {
		return "", errors.New("DATABASE_URL or DATABASE_URL_FILE is required")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read DATABASE_URL_FILE: %w", err)
	}

	value = strings.TrimSpace(string(content))
	if value == "" {
		return "", errors.New("DATABASE_URL_FILE is empty")
	}
	return value, nil
}

func listMigrationFiles(directory string) ([]string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		files = append(files, filepath.Join(directory, entry.Name()))
	}
	sort.Strings(files)

	if len(files) == 0 {
		return nil, fmt.Errorf("no SQL migrations found in %s", directory)
	}
	return files, nil
}

func applyMigration(ctx context.Context, connection *pgx.Conn, path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", filepath.Base(path), err)
	}

	transaction, err := connection.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", filepath.Base(path), err)
	}
	defer transaction.Rollback(context.Background())

	if _, err := transaction.Exec(ctx, string(content), pgx.QueryExecModeSimpleProtocol); err != nil {
		return fmt.Errorf("apply migration %s: %w", filepath.Base(path), err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", filepath.Base(path), err)
	}

	log.Printf("migration.applied file=%q", filepath.Base(path))
	return nil
}
