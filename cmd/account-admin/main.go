package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/accountadmin"
)

const (
	defaultMaxGrantDuration = 365 * 24 * time.Hour
	commandTimeout          = 30 * time.Second
)

type commandConfig struct {
	Environment string
	DatabaseURL string
	MaxDuration time.Duration
}

type selectorFlags struct {
	userID  string
	email   string
	subject string
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "account-admin: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		printUsage(stderr)
		return errors.New("a command is required")
	}
	if args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		printUsage(stdout)
		return nil
	}

	cfg, err := loadCommandConfig()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("create database pool: %w", err)
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}

	repository, err := accountadmin.NewPostgresRepository(pool)
	if err != nil {
		return err
	}
	service, err := accountadmin.NewService(repository, cfg.Environment, cfg.MaxDuration)
	if err != nil {
		return err
	}

	var result any
	switch args[0] {
	case "grant-pro":
		result, err = runGrant(ctx, service, args[1:], stderr)
	case "revoke-pro":
		result, err = runRevoke(ctx, service, args[1:], stderr)
	case "status":
		result, err = runStatus(ctx, service, args[1:], stderr)
	case "list-active-manual-grants":
		result, err = runList(ctx, service, args[1:], stderr)
	case "verify-lifecycle":
		result, err = runVerifyLifecycle(ctx, service, args[1:], stderr)
	default:
		printUsage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]any{"ok": true, "result": result})
}

func runGrant(ctx context.Context, service *accountadmin.Service, args []string, stderr io.Writer) (any, error) {
	flags := flag.NewFlagSet("grant-pro", flag.ContinueOnError)
	flags.SetOutput(stderr)
	selector := addSelectorFlags(flags)
	durationValue := flags.String("duration", "30d", "grant duration, for example 30d or 720h")
	actor := flags.String("actor", "", "administrator identity recorded in the audit log")
	reason := flags.String("reason", "", "required business reason")
	dryRun := flags.Bool("dry-run", false, "preview the operation without database writes")
	confirmation := flags.String("confirm-production", "", "must equal PRODUCTION for production mutations")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 {
		return nil, errors.New("grant-pro does not accept positional arguments")
	}
	duration, err := accountadmin.ParseDuration(*durationValue)
	if err != nil {
		return nil, err
	}
	return service.GrantPro(ctx, accountadmin.GrantRequest{
		Selector:               selector.value(),
		Duration:               duration,
		Actor:                  *actor,
		Reason:                 *reason,
		DryRun:                 *dryRun,
		ProductionConfirmation: *confirmation,
	})
}

func runRevoke(ctx context.Context, service *accountadmin.Service, args []string, stderr io.Writer) (any, error) {
	flags := flag.NewFlagSet("revoke-pro", flag.ContinueOnError)
	flags.SetOutput(stderr)
	selector := addSelectorFlags(flags)
	actor := flags.String("actor", "", "administrator identity recorded in the audit log")
	reason := flags.String("reason", "", "required business reason")
	dryRun := flags.Bool("dry-run", false, "preview the operation without database writes")
	confirmation := flags.String("confirm-production", "", "must equal PRODUCTION for production mutations")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 {
		return nil, errors.New("revoke-pro does not accept positional arguments")
	}
	return service.RevokePro(ctx, accountadmin.RevokeRequest{
		Selector:               selector.value(),
		Actor:                  *actor,
		Reason:                 *reason,
		DryRun:                 *dryRun,
		ProductionConfirmation: *confirmation,
	})
}

func runStatus(ctx context.Context, service *accountadmin.Service, args []string, stderr io.Writer) (any, error) {
	flags := flag.NewFlagSet("status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	selector := addSelectorFlags(flags)
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 {
		return nil, errors.New("status does not accept positional arguments")
	}
	return service.Status(ctx, selector.value())
}

func runList(ctx context.Context, service *accountadmin.Service, args []string, stderr io.Writer) (any, error) {
	flags := flag.NewFlagSet("list-active-manual-grants", flag.ContinueOnError)
	flags.SetOutput(stderr)
	limit := flags.Int("limit", 100, "maximum number of grants to return")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 {
		return nil, errors.New("list-active-manual-grants does not accept positional arguments")
	}
	return service.ListActiveManualGrants(ctx, *limit)
}

func runVerifyLifecycle(ctx context.Context, service *accountadmin.Service, args []string, stderr io.Writer) (any, error) {
	flags := flag.NewFlagSet("verify-lifecycle", flag.ContinueOnError)
	flags.SetOutput(stderr)
	actor := flags.String("actor", "", "administrator identity recorded in the rolled-back audit events")
	reason := flags.String("reason", "", "required verification reason")
	confirmation := flags.String("confirm-production", "", "must equal PRODUCTION in production")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 {
		return nil, errors.New("verify-lifecycle does not accept positional arguments")
	}
	return service.VerifyLifecycle(ctx, accountadmin.VerifyRequest{
		Actor:                  *actor,
		Reason:                 *reason,
		ProductionConfirmation: *confirmation,
	})
}

func addSelectorFlags(flags *flag.FlagSet) *selectorFlags {
	selector := &selectorFlags{}
	flags.StringVar(&selector.userID, "user-id", "", "app_users UUID")
	flags.StringVar(&selector.email, "email", "", "exact account email; fails when ambiguous")
	flags.StringVar(&selector.subject, "subject", "", "exact Auth0 subject")
	return selector
}

func (s *selectorFlags) value() accountadmin.Selector {
	return accountadmin.Selector{UserID: s.userID, Email: s.email, AuthSubject: s.subject}
}

func loadCommandConfig() (commandConfig, error) {
	databaseURL, err := databaseURLFromEnvironment()
	if err != nil {
		return commandConfig{}, err
	}
	environment := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	if environment == "" {
		environment = "development"
	}
	maxDuration := defaultMaxGrantDuration
	if value := strings.TrimSpace(os.Getenv("ACCOUNT_ADMIN_MAX_GRANT_DURATION")); value != "" {
		maxDuration, err = accountadmin.ParseDuration(value)
		if err != nil {
			return commandConfig{}, fmt.Errorf("ACCOUNT_ADMIN_MAX_GRANT_DURATION: %w", err)
		}
	}
	return commandConfig{Environment: environment, DatabaseURL: databaseURL, MaxDuration: maxDuration}, nil
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

func writeJSON(writer io.Writer, value any) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(true)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func printUsage(writer io.Writer) {
	_, _ = fmt.Fprintln(writer, `Usage: account-admin <command> [options]

Commands:
  grant-pro                   Grant one idempotent manual Pro subscription
  revoke-pro                  Revoke the active manual Pro subscription
  status                      Show effective and manual access for one user
  list-active-manual-grants   List active manual grants
  verify-lifecycle            Verify Free -> Pro -> Free in a rolled-back transaction

User selector (exactly one):
  --user-id <uuid>
  --email <address>
  --subject <auth0-sub>

Production mutations require:
  --confirm-production PRODUCTION`)
}
