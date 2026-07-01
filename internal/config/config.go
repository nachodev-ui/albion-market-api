package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv                    string
	HTTPAddr                  string
	DatabaseURL               string
	ReadTimeout               time.Duration
	ReadHeaderTimeout         time.Duration
	WriteTimeout              time.Duration
	IdleTimeout               time.Duration
	MaxHeaderBytes            int
	IngestBearerToken         string
	IngestPreviousBearerToken string
	MaxIngestBodyBytes        int64
	MaxPublicBodyBytes        int64
	CORSAllowedOrigins        []string
	RateLimitEnabled          bool
	RateLimitRequestsPerSec   float64
	RateLimitBurst            int
	RateLimitClientTTL        time.Duration
	TrustProxyHeaders         bool
	LogColor                  string
}

func Load() (Config, error) {
	_ = godotenv.Load(".env.example")
	_ = godotenv.Overload(".env.local")

	appEnv := strings.ToLower(strings.TrimSpace(getEnv("APP_ENV", "development")))

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
	maxPublicBodyBytes, err := int64Env("MAX_PUBLIC_BODY_BYTES", 64<<10)
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

	cfg := Config{
		AppEnv:                    appEnv,
		HTTPAddr:                  strings.TrimSpace(getEnv("HTTP_ADDR", ":8080")),
		DatabaseURL:               strings.TrimSpace(os.Getenv("DATABASE_URL")),
		ReadTimeout:               readTimeout,
		ReadHeaderTimeout:         readHeaderTimeout,
		WriteTimeout:              writeTimeout,
		IdleTimeout:               idleTimeout,
		MaxHeaderBytes:            maxHeaderBytes,
		IngestBearerToken:         strings.TrimSpace(os.Getenv("INGEST_BEARER_TOKEN")),
		IngestPreviousBearerToken: strings.TrimSpace(os.Getenv("INGEST_BEARER_TOKEN_PREVIOUS")),
		MaxIngestBodyBytes:        maxIngestBodyBytes,
		MaxPublicBodyBytes:        maxPublicBodyBytes,
		CORSAllowedOrigins:        csvEnv("CORS_ALLOWED_ORIGINS", defaultCORSOrigins(appEnv)),
		RateLimitEnabled:          rateLimitEnabled,
		RateLimitRequestsPerSec:   rateLimitRequestsPerSec,
		RateLimitBurst:            rateLimitBurst,
		RateLimitClientTTL:        rateLimitClientTTL,
		TrustProxyHeaders:         trustProxyHeaders,
		LogColor:                  getEnv("LOG_COLOR", "auto"),
	}

	if cfg.HTTPAddr == "" {
		return Config{}, fmt.Errorf("HTTP_ADDR must not be empty")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.IngestBearerToken == "" {
		return Config{}, fmt.Errorf("INGEST_BEARER_TOKEN is required")
	}
	if cfg.IngestPreviousBearerToken != "" && cfg.IngestPreviousBearerToken == cfg.IngestBearerToken {
		return Config{}, fmt.Errorf("INGEST_BEARER_TOKEN_PREVIOUS must be different from INGEST_BEARER_TOKEN")
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

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
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
