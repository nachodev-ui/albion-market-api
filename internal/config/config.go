package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/joho/godotenv"

	"github.com/nachodev-ui/albion-market-api/internal/ingestauth"
)

const maxSecretFileBytes = 16 << 10

type CredentialSource struct {
	ID     string
	Source string
}

type Config struct {
	AppEnv                  string
	HTTPAddr                string
	DatabaseURL             string
	ReadTimeout             time.Duration
	ReadHeaderTimeout       time.Duration
	WriteTimeout            time.Duration
	IdleTimeout             time.Duration
	MaxHeaderBytes          int
	IngestCredentials       []ingestauth.Credential
	IngestCredentialSources []CredentialSource
	IngestRequireHTTPS      bool
	MaxIngestBodyBytes      int64
	MaxPublicBodyBytes      int64
	CORSAllowedOrigins      []string
	RateLimitEnabled        bool
	RateLimitRequestsPerSec float64
	RateLimitBurst          int
	RateLimitClientTTL      time.Duration
	TrustProxyHeaders       bool
	LogColor                string
	LogFormat               string
}

func Load() (Config, error) {
	if err := loadDotEnvFiles(); err != nil {
		return Config{}, err
	}

	appEnv := strings.ToLower(strings.TrimSpace(getEnv("APP_ENV", "development")))
	if appEnv == "" {
		appEnv = "development"
	}

	readTimeout, err := durationEnv("READ_TIMEOUT", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	readHeaderTimeout, err := durationEnv("READ_HEADER_TIMEOUT", 2*time.Second)
	if err != nil {
		return Config{}, err
	}
	writeTimeout, err := durationEnv("WRITE_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	idleTimeout, err := durationEnv("IDLE_TIMEOUT", 60*time.Second)
	if err != nil {
		return Config{}, err
	}
	maxHeaderBytes, err := intEnv("MAX_HEADER_BYTES", 1<<20)
	if err != nil {
		return Config{}, err
	}
	maxIngestBodyBytes, err := int64Env("MAX_INGEST_BODY_BYTES", 5<<20)
	if err != nil {
		return Config{}, err
	}
	maxPublicBodyBytes, err := int64Env("MAX_PUBLIC_BODY_BYTES", 1<<20)
	if err != nil {
		return Config{}, err
	}
	rateLimitEnabled, err := boolEnv("RATE_LIMIT_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	rateLimitRequestsPerSec, err := float64Env("RATE_LIMIT_REQUESTS_PER_SECOND", 20)
	if err != nil {
		return Config{}, err
	}
	rateLimitBurst, err := intEnv("RATE_LIMIT_BURST", 40)
	if err != nil {
		return Config{}, err
	}
	rateLimitClientTTL, err := durationEnv("RATE_LIMIT_CLIENT_TTL", 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	trustProxyHeaders, err := boolEnv("TRUST_PROXY_HEADERS", false)
	if err != nil {
		return Config{}, err
	}
	ingestRequireHTTPS, err := boolEnv("INGEST_REQUIRE_HTTPS", appEnv == "production")
	if err != nil {
		return Config{}, err
	}
	logFormat := strings.ToLower(strings.TrimSpace(getEnv("LOG_FORMAT", defaultLogFormat(appEnv))))
	if logFormat != "text" && logFormat != "json" {
		return Config{}, fmt.Errorf("LOG_FORMAT must be text or json")
	}
	minimumTokenLength, err := intEnv("INGEST_MIN_TOKEN_LENGTH", 32)
	if err != nil {
		return Config{}, err
	}
	if minimumTokenLength < 16 || minimumTokenLength > maxSecretFileBytes {
		return Config{}, fmt.Errorf("INGEST_MIN_TOKEN_LENGTH must be between 16 and %d", maxSecretFileBytes)
	}
	if appEnv == "production" && minimumTokenLength < 32 {
		return Config{}, fmt.Errorf("INGEST_MIN_TOKEN_LENGTH must be at least 32 in production")
	}

	databaseURL, _, err := secretFromEnvOrFile("DATABASE_URL", "DATABASE_URL_FILE", appEnv)
	if err != nil {
		return Config{}, err
	}

	credentials, credentialSources, err := loadIngestCredentials(appEnv, minimumTokenLength)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		AppEnv:                  appEnv,
		HTTPAddr:                strings.TrimSpace(getEnv("HTTP_ADDR", ":8080")),
		DatabaseURL:             databaseURL,
		ReadTimeout:             readTimeout,
		ReadHeaderTimeout:       readHeaderTimeout,
		WriteTimeout:            writeTimeout,
		IdleTimeout:             idleTimeout,
		MaxHeaderBytes:          maxHeaderBytes,
		IngestCredentials:       credentials,
		IngestCredentialSources: credentialSources,
		IngestRequireHTTPS:      ingestRequireHTTPS,
		MaxIngestBodyBytes:      maxIngestBodyBytes,
		MaxPublicBodyBytes:      maxPublicBodyBytes,
		CORSAllowedOrigins:      csvEnv("CORS_ALLOWED_ORIGINS", defaultCORSOrigins(appEnv)),
		RateLimitEnabled:        rateLimitEnabled,
		RateLimitRequestsPerSec: rateLimitRequestsPerSec,
		RateLimitBurst:          rateLimitBurst,
		RateLimitClientTTL:      rateLimitClientTTL,
		TrustProxyHeaders:       trustProxyHeaders,
		LogColor:                getEnv("LOG_COLOR", "auto"),
		LogFormat:               logFormat,
	}

	if cfg.HTTPAddr == "" {
		return Config{}, fmt.Errorf("HTTP_ADDR must not be empty")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL or DATABASE_URL_FILE is required")
	}
	if cfg.RateLimitEnabled && cfg.RateLimitRequestsPerSec <= 0 {
		return Config{}, fmt.Errorf("RATE_LIMIT_REQUESTS_PER_SECOND must be greater than zero when rate limiting is enabled")
	}
	if cfg.RateLimitEnabled && cfg.RateLimitBurst <= 0 {
		return Config{}, fmt.Errorf("RATE_LIMIT_BURST must be greater than zero when rate limiting is enabled")
	}
	if cfg.AppEnv == "production" && containsString(cfg.CORSAllowedOrigins, "*") {
		return Config{}, fmt.Errorf("CORS_ALLOWED_ORIGINS must not contain * in production")
	}

	return cfg, nil
}

func loadDotEnvFiles() error {
	initialEnvironment := strings.ToLower(strings.TrimSpace(getEnv("APP_ENV", "development")))
	loadFiles, err := boolEnv("LOAD_DOTENV", initialEnvironment != "production")
	if err != nil {
		return err
	}
	if !loadFiles {
		return nil
	}

	// Process-level environment variables always win. .env.example is never
	// loaded at runtime because it contains documentation placeholders.
	_ = godotenv.Load(".env.local")
	environment := strings.ToLower(strings.TrimSpace(getEnv("APP_ENV", initialEnvironment)))
	if environment != "" {
		_ = godotenv.Load(".env." + environment + ".local")
		_ = godotenv.Load(".env." + environment)
	}
	_ = godotenv.Load(".env")
	return nil
}

func loadIngestCredentials(appEnv string, minimumLength int) ([]ingestauth.Credential, []CredentialSource, error) {
	currentID := strings.TrimSpace(getEnv("INGEST_BEARER_TOKEN_ID", "current"))
	currentToken, currentSource, err := secretFromEnvOrFile("INGEST_BEARER_TOKEN", "INGEST_BEARER_TOKEN_FILE", appEnv)
	if err != nil {
		return nil, nil, err
	}
	if currentToken == "" {
		return nil, nil, fmt.Errorf("INGEST_BEARER_TOKEN or INGEST_BEARER_TOKEN_FILE is required")
	}
	if err := validateCredential(currentID, currentToken, minimumLength); err != nil {
		return nil, nil, fmt.Errorf("current ingest credential: %w", err)
	}

	credentials := []ingestauth.Credential{{ID: currentID, Token: currentToken}}
	sources := []CredentialSource{{ID: currentID, Source: currentSource}}

	previousID := strings.TrimSpace(getEnv("INGEST_BEARER_TOKEN_PREVIOUS_ID", "previous"))
	previousToken, previousSource, err := secretFromEnvOrFile(
		"INGEST_BEARER_TOKEN_PREVIOUS",
		"INGEST_BEARER_TOKEN_PREVIOUS_FILE",
		appEnv,
	)
	if err != nil {
		return nil, nil, err
	}
	if previousToken != "" {
		if err := validateCredential(previousID, previousToken, minimumLength); err != nil {
			return nil, nil, fmt.Errorf("previous ingest credential: %w", err)
		}
		if previousID == currentID {
			return nil, nil, fmt.Errorf("INGEST_BEARER_TOKEN_PREVIOUS_ID must be different from INGEST_BEARER_TOKEN_ID")
		}
		if previousToken == currentToken {
			return nil, nil, fmt.Errorf("previous ingest token must be different from the current token")
		}
		credentials = append(credentials, ingestauth.Credential{ID: previousID, Token: previousToken})
		sources = append(sources, CredentialSource{ID: previousID, Source: previousSource})
	}

	return credentials, sources, nil
}

func secretFromEnvOrFile(valueEnv, fileEnv, appEnv string) (string, string, error) {
	value := strings.TrimSpace(os.Getenv(valueEnv))
	filePath := strings.TrimSpace(os.Getenv(fileEnv))
	if value != "" && filePath != "" {
		return "", "", fmt.Errorf("%s and %s are mutually exclusive", valueEnv, fileEnv)
	}
	if value != "" {
		return value, "environment", nil
	}
	if filePath == "" {
		return "", "", nil
	}

	info, err := os.Stat(filePath)
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", fileEnv, err)
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("%s must reference a file", fileEnv)
	}
	if info.Size() > maxSecretFileBytes {
		return "", "", fmt.Errorf("%s exceeds %d bytes", fileEnv, maxSecretFileBytes)
	}
	if err := validateSecretFilePermissions(filePath, info.Mode(), appEnv); err != nil {
		return "", "", fmt.Errorf("%s: %w", fileEnv, err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", "", fmt.Errorf("read %s: %w", fileEnv, err)
	}
	secret := strings.TrimSpace(string(content))
	if secret == "" {
		return "", "", fmt.Errorf("%s is empty", fileEnv)
	}
	return secret, "file", nil
}

func validateSecretFilePermissions(filePath string, mode os.FileMode, appEnv string) error {
	if appEnv != "production" || runtime.GOOS == "windows" {
		return nil
	}

	// Docker Compose grants /run/secrets files explicitly to the service and
	// mounts them read-only. Compose implementations based on bind mounts may
	// expose those files as 0444, so host-style 0600 checks are not applicable
	// inside this runtime-managed directory.
	cleanPath := filepath.ToSlash(filepath.Clean(filePath))
	if cleanPath == "/run/secrets" || strings.HasPrefix(cleanPath, "/run/secrets/") {
		return nil
	}

	if mode.Perm()&0o077 != 0 {
		return fmt.Errorf("permissions are too broad; use 0600 or stricter")
	}
	return nil
}

func validateCredential(id, token string, minimumLength int) error {
	if !validCredentialID(id) {
		return fmt.Errorf("credential id must contain 1-64 letters, numbers, dots, underscores or hyphens")
	}
	if len(token) < minimumLength {
		return fmt.Errorf("token must contain at least %d characters", minimumLength)
	}
	if len(token) > maxSecretFileBytes {
		return fmt.Errorf("token exceeds %d characters", maxSecretFileBytes)
	}
	for _, character := range token {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return fmt.Errorf("token must not contain whitespace or control characters")
		}
	}
	upper := strings.ToUpper(token)
	if strings.Contains(upper, "CHANGE_ME") || strings.Contains(upper, "REPLACE_ME") {
		return fmt.Errorf("token still contains a placeholder")
	}
	switch strings.ToLower(token) {
	case "secret", "token", "password", "changeme":
		return fmt.Errorf("token is a known insecure placeholder")
	}
	return nil
}

func validCredentialID(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}

func defaultLogFormat(appEnv string) string {
	if appEnv == "production" {
		return "json"
	}
	return "text"
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return parsed, nil
}

func int64Env(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func intEnv(key string, fallback int) (int, error) {
	parsed, err := int64Env(key, int64(fallback))
	if err != nil {
		return 0, err
	}
	return int(parsed), nil
}

func float64Env(key string, fallback float64) (float64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive number", key)
	}
	return parsed, nil
}

func boolEnv(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s must be true or false", key)
	}
	return parsed, nil
}

func csvEnv(key string, fallback []string) []string {
	value, exists := os.LookupEnv(key)
	if !exists {
		return append([]string(nil), fallback...)
	}
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, raw := range strings.Split(value, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		if _, exists := seen[entry]; exists {
			continue
		}
		seen[entry] = struct{}{}
		result = append(result, entry)
	}
	return result
}

func defaultCORSOrigins(appEnv string) []string {
	if appEnv == "production" {
		return nil
	}
	return []string{
		"http://localhost:5173",
		"http://127.0.0.1:5173",
		"http://localhost:4173",
		"http://127.0.0.1:4173",
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
