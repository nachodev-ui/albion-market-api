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
	WriteTimeout              time.Duration
	IdleTimeout               time.Duration
	IngestBearerToken         string
	IngestPreviousBearerToken string
	MaxIngestBodyBytes        int64
	LogColor                  string
}

func Load() (Config, error) {
	_ = godotenv.Load(".env.example")
	_ = godotenv.Overload(".env.local")

	cfg := Config{
		AppEnv:                    getEnv("APP_ENV", "development"),
		HTTPAddr:                  getEnv("HTTP_ADDR", ":8080"),
		DatabaseURL:               os.Getenv("DATABASE_URL"),
		ReadTimeout:               getDurationEnv("READ_TIMEOUT", 5*time.Second),
		WriteTimeout:              getDurationEnv("WRITE_TIMEOUT", 10*time.Second),
		IdleTimeout:               getDurationEnv("IDLE_TIMEOUT", 60*time.Second),
		IngestBearerToken:         strings.TrimSpace(os.Getenv("INGEST_BEARER_TOKEN")),
		IngestPreviousBearerToken: strings.TrimSpace(os.Getenv("INGEST_BEARER_TOKEN_PREVIOUS")),
		MaxIngestBodyBytes:        getInt64Env("MAX_INGEST_BODY_BYTES", 5<<20),
		LogColor:                  getEnv("LOG_COLOR", "auto"),
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

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		return fallback
	}
	return d
}

func getInt64Env(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}

	parsed, err := strconv.ParseInt(v, 10, 64)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
