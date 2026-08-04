package config

import (
	"strings"
	"testing"
)

func TestLoadBillingRejectsTestModeInProduction(t *testing.T) {
	setValidBillingEnvironment(t)
	t.Setenv("BILLING_TEST_MODE", "true")

	_, err := LoadBilling("production")
	if err == nil || !strings.Contains(err.Error(), "BILLING_TEST_MODE") {
		t.Fatalf("error = %v, want production test-mode rejection", err)
	}
}

func TestLoadBillingAcceptsStrictProductionConfiguration(t *testing.T) {
	setValidBillingEnvironment(t)

	cfg, err := LoadBilling("production")
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.TestMode || cfg.WebhookIngestTimeout.Milliseconds() != 1500 {
		t.Fatalf("unexpected billing config: %#v", cfg)
	}
}

func setValidBillingEnvironment(t *testing.T) {
	t.Helper()
	values := map[string]string{
		"BILLING_ENABLED":                      "true",
		"BILLING_PROVIDER":                     "lemonsqueezy",
		"BILLING_TEST_MODE":                    "false",
		"BILLING_CHECKOUT_REDIRECT_URL":        "https://albioncalculator.app/account?checkout=success",
		"BILLING_GRACE_PERIOD":                 "168h",
		"BILLING_HTTP_TIMEOUT":                 "10s",
		"BILLING_MAX_WEBHOOK_BODY_BYTES":       "1048576",
		"BILLING_WEBHOOK_INGEST_TIMEOUT":       "1500ms",
		"BILLING_WEBHOOK_WORKER_POLL_INTERVAL": "250ms",
		"BILLING_WEBHOOK_JOB_TIMEOUT":          "15s",
		"BILLING_WEBHOOK_LEASE_DURATION":       "60s",
		"BILLING_WEBHOOK_BASE_RETRY_DELAY":     "5s",
		"BILLING_WEBHOOK_MAX_RETRY_DELAY":      "15m",
		"BILLING_WEBHOOK_BATCH_SIZE":           "10",
		"BILLING_WEBHOOK_MAX_ATTEMPTS":         "8",
		"LEMONSQUEEZY_API_BASE_URL":            "https://api.lemonsqueezy.com",
		"LEMONSQUEEZY_STORE_ID":                "123",
		"LEMONSQUEEZY_PRO_VARIANT_ID":          "456",
		"LEMONSQUEEZY_API_KEY":                 "live_api_key_for_contract_tests_only",
		"LEMONSQUEEZY_API_KEY_FILE":            "",
		"LEMONSQUEEZY_WEBHOOK_SECRET":          "0123456789abcdef0123456789abcdef",
		"LEMONSQUEEZY_WEBHOOK_SECRET_FILE":     "",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
}
