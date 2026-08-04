package config

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultLemonSqueezyAPIBaseURL = "https://api.lemonsqueezy.com"

type BillingConfig struct {
	Enabled              bool
	Provider             string
	APIBaseURL           string
	APIKey               string
	StoreID              string
	VariantID            string
	WebhookSecret        string
	CheckoutRedirectURL  string
	TestMode             bool
	GracePeriod          time.Duration
	HTTPTimeout          time.Duration
	MaxWebhookBodyBytes  int64
	WebhookIngestTimeout time.Duration
	WorkerPollInterval   time.Duration
	WorkerJobTimeout     time.Duration
	WorkerLeaseDuration  time.Duration
	WorkerBaseRetryDelay time.Duration
	WorkerMaxRetryDelay  time.Duration
	WorkerBatchSize      int
	WorkerMaxAttempts    int
}

func LoadBilling(appEnv string) (BillingConfig, error) {
	enabled, err := boolEnv("BILLING_ENABLED", false)
	if err != nil {
		return BillingConfig{}, err
	}
	testMode, err := boolEnv("BILLING_TEST_MODE", true)
	if err != nil {
		return BillingConfig{}, err
	}
	gracePeriod, err := durationEnv("BILLING_GRACE_PERIOD", 7*24*time.Hour)
	if err != nil {
		return BillingConfig{}, err
	}
	httpTimeout, err := durationEnv("BILLING_HTTP_TIMEOUT", 10*time.Second)
	if err != nil {
		return BillingConfig{}, err
	}
	maxWebhookBodyBytes, err := int64Env("BILLING_MAX_WEBHOOK_BODY_BYTES", 1<<20)
	if err != nil {
		return BillingConfig{}, err
	}
	webhookIngestTimeout, err := durationEnv("BILLING_WEBHOOK_INGEST_TIMEOUT", 1500*time.Millisecond)
	if err != nil {
		return BillingConfig{}, err
	}
	workerPollInterval, err := durationEnv("BILLING_WEBHOOK_WORKER_POLL_INTERVAL", 250*time.Millisecond)
	if err != nil {
		return BillingConfig{}, err
	}
	workerJobTimeout, err := durationEnv("BILLING_WEBHOOK_JOB_TIMEOUT", 15*time.Second)
	if err != nil {
		return BillingConfig{}, err
	}
	workerLeaseDuration, err := durationEnv("BILLING_WEBHOOK_LEASE_DURATION", time.Minute)
	if err != nil {
		return BillingConfig{}, err
	}
	workerBaseRetryDelay, err := durationEnv("BILLING_WEBHOOK_BASE_RETRY_DELAY", 5*time.Second)
	if err != nil {
		return BillingConfig{}, err
	}
	workerMaxRetryDelay, err := durationEnv("BILLING_WEBHOOK_MAX_RETRY_DELAY", 15*time.Minute)
	if err != nil {
		return BillingConfig{}, err
	}
	workerBatchSize, err := intEnv("BILLING_WEBHOOK_BATCH_SIZE", 10)
	if err != nil {
		return BillingConfig{}, err
	}
	workerMaxAttempts, err := intEnv("BILLING_WEBHOOK_MAX_ATTEMPTS", 8)
	if err != nil {
		return BillingConfig{}, err
	}

	provider := strings.ToLower(strings.TrimSpace(getEnv("BILLING_PROVIDER", "lemonsqueezy")))
	apiBaseURL := strings.TrimRight(strings.TrimSpace(getEnv("LEMONSQUEEZY_API_BASE_URL", defaultLemonSqueezyAPIBaseURL)), "/")
	storeID := strings.TrimSpace(getEnv("LEMONSQUEEZY_STORE_ID", ""))
	variantID := strings.TrimSpace(getEnv("LEMONSQUEEZY_PRO_VARIANT_ID", ""))
	redirectURL := strings.TrimSpace(getEnv("BILLING_CHECKOUT_REDIRECT_URL", ""))

	apiKey, _, err := secretFromEnvOrFile("LEMONSQUEEZY_API_KEY", "LEMONSQUEEZY_API_KEY_FILE", appEnv)
	if err != nil {
		return BillingConfig{}, err
	}
	webhookSecret, _, err := secretFromEnvOrFile(
		"LEMONSQUEEZY_WEBHOOK_SECRET",
		"LEMONSQUEEZY_WEBHOOK_SECRET_FILE",
		appEnv,
	)
	if err != nil {
		return BillingConfig{}, err
	}

	cfg := BillingConfig{
		Enabled:              enabled,
		Provider:             provider,
		APIBaseURL:           apiBaseURL,
		APIKey:               apiKey,
		StoreID:              storeID,
		VariantID:            variantID,
		WebhookSecret:        webhookSecret,
		CheckoutRedirectURL:  redirectURL,
		TestMode:             testMode,
		GracePeriod:          gracePeriod,
		HTTPTimeout:          httpTimeout,
		MaxWebhookBodyBytes:  maxWebhookBodyBytes,
		WebhookIngestTimeout: webhookIngestTimeout,
		WorkerPollInterval:   workerPollInterval,
		WorkerJobTimeout:     workerJobTimeout,
		WorkerLeaseDuration:  workerLeaseDuration,
		WorkerBaseRetryDelay: workerBaseRetryDelay,
		WorkerMaxRetryDelay:  workerMaxRetryDelay,
		WorkerBatchSize:      workerBatchSize,
		WorkerMaxAttempts:    workerMaxAttempts,
	}
	if !enabled {
		return cfg, nil
	}

	if provider != "lemonsqueezy" {
		return BillingConfig{}, fmt.Errorf("BILLING_PROVIDER must be lemonsqueezy")
	}
	if apiKey == "" {
		return BillingConfig{}, fmt.Errorf("LEMONSQUEEZY_API_KEY or LEMONSQUEEZY_API_KEY_FILE is required when billing is enabled")
	}
	if webhookSecret == "" {
		return BillingConfig{}, fmt.Errorf("LEMONSQUEEZY_WEBHOOK_SECRET or LEMONSQUEEZY_WEBHOOK_SECRET_FILE is required when billing is enabled")
	}
	if err := positiveNumericIdentifier("LEMONSQUEEZY_STORE_ID", storeID); err != nil {
		return BillingConfig{}, err
	}
	if err := positiveNumericIdentifier("LEMONSQUEEZY_PRO_VARIANT_ID", variantID); err != nil {
		return BillingConfig{}, err
	}
	if gracePeriod < 0 || gracePeriod > 30*24*time.Hour {
		return BillingConfig{}, fmt.Errorf("BILLING_GRACE_PERIOD must be between 0 and 720h")
	}
	if httpTimeout < time.Second || httpTimeout > time.Minute {
		return BillingConfig{}, fmt.Errorf("BILLING_HTTP_TIMEOUT must be between 1s and 1m")
	}
	if maxWebhookBodyBytes < 1024 || maxWebhookBodyBytes > 5<<20 {
		return BillingConfig{}, fmt.Errorf("BILLING_MAX_WEBHOOK_BODY_BYTES must be between 1024 and 5242880")
	}
	if webhookIngestTimeout < 100*time.Millisecond || webhookIngestTimeout >= 2*time.Second {
		return BillingConfig{}, fmt.Errorf("BILLING_WEBHOOK_INGEST_TIMEOUT must be at least 100ms and less than 2s")
	}
	if workerPollInterval < 100*time.Millisecond || workerPollInterval > time.Minute {
		return BillingConfig{}, fmt.Errorf("BILLING_WEBHOOK_WORKER_POLL_INTERVAL must be between 100ms and 1m")
	}
	if workerJobTimeout < time.Second || workerJobTimeout > 5*time.Minute {
		return BillingConfig{}, fmt.Errorf("BILLING_WEBHOOK_JOB_TIMEOUT must be between 1s and 5m")
	}
	if workerLeaseDuration <= workerJobTimeout || workerLeaseDuration > 30*time.Minute {
		return BillingConfig{}, fmt.Errorf("BILLING_WEBHOOK_LEASE_DURATION must be greater than the job timeout and at most 30m")
	}
	if workerBaseRetryDelay < time.Second || workerBaseRetryDelay > time.Hour {
		return BillingConfig{}, fmt.Errorf("BILLING_WEBHOOK_BASE_RETRY_DELAY must be between 1s and 1h")
	}
	if workerMaxRetryDelay < workerBaseRetryDelay || workerMaxRetryDelay > 24*time.Hour {
		return BillingConfig{}, fmt.Errorf("BILLING_WEBHOOK_MAX_RETRY_DELAY must be between the base delay and 24h")
	}
	if workerBatchSize < 1 || workerBatchSize > 100 {
		return BillingConfig{}, fmt.Errorf("BILLING_WEBHOOK_BATCH_SIZE must be between 1 and 100")
	}
	if workerMaxAttempts < 1 || workerMaxAttempts > 50 {
		return BillingConfig{}, fmt.Errorf("BILLING_WEBHOOK_MAX_ATTEMPTS must be between 1 and 50")
	}

	apiURL, err := url.Parse(apiBaseURL)
	if err != nil || !apiURL.IsAbs() || apiURL.Host == "" {
		return BillingConfig{}, fmt.Errorf("LEMONSQUEEZY_API_BASE_URL must be an absolute URL")
	}
	redirect, err := url.Parse(redirectURL)
	if err != nil || !redirect.IsAbs() || redirect.Host == "" {
		return BillingConfig{}, fmt.Errorf("BILLING_CHECKOUT_REDIRECT_URL must be an absolute URL")
	}
	production := strings.EqualFold(strings.TrimSpace(appEnv), "production")
	if production && (apiURL.Scheme != "https" || redirect.Scheme != "https") {
		return BillingConfig{}, fmt.Errorf("billing URLs must use https in production")
	}
	if production && testMode {
		return BillingConfig{}, fmt.Errorf("BILLING_TEST_MODE must be false when billing is enabled in production")
	}
	if production && apiBaseURL != defaultLemonSqueezyAPIBaseURL {
		return BillingConfig{}, fmt.Errorf("LEMONSQUEEZY_API_BASE_URL must use the official Lemon Squeezy endpoint in production")
	}
	if production && len(webhookSecret) < 32 {
		return BillingConfig{}, fmt.Errorf("LEMONSQUEEZY_WEBHOOK_SECRET must contain at least 32 characters in production")
	}

	return cfg, nil
}

func positiveNumericIdentifier(name, value string) error {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return fmt.Errorf("%s must be a positive numeric identifier", name)
	}
	return nil
}
