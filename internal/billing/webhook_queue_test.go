package billing

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateAndSanitizeWebhookDropsPII(t *testing.T) {
	t.Parallel()
	raw := []byte(`{
		"meta":{"event_name":"subscription_created","custom_data":{"user_id":"5f31c2a6-4c51-4e25-8d81-9550134b637b","email":"private@example.com"}},
		"data":{"type":"subscriptions","id":"9001","attributes":{"store_id":11,"customer_id":22,"variant_id":33,"status":"active","renews_at":"2026-09-04T12:00:00Z","updated_at":"2026-08-04T12:00:00Z","test_mode":false,"user_email":"private@example.com"}}
	}`)
	envelope, sanitized, err := validateAndSanitizeWebhook(raw, "subscription_created")
	if err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID != "9001" {
		t.Fatalf("data id = %q", envelope.Data.ID)
	}
	if strings.Contains(string(sanitized), "private@example.com") || strings.Contains(string(sanitized), "user_email") {
		t.Fatalf("sanitized payload retained PII: %s", sanitized)
	}
	var decoded map[string]any
	if err := json.Unmarshal(sanitized, &decoded); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAndSanitizeWebhookRejectsHeaderMismatch(t *testing.T) {
	t.Parallel()
	raw := []byte(`{"meta":{"event_name":"subscription_created"},"data":{"type":"subscriptions","id":"1","attributes":{}}}`)
	_, _, err := validateAndSanitizeWebhook(raw, "subscription_updated")
	if !errors.Is(err, ErrInvalidWebhook) {
		t.Fatalf("error = %v, want ErrInvalidWebhook", err)
	}
}

func TestValidateWebhookScopeAllowsPlanDowngradeUpdates(t *testing.T) {
	t.Parallel()
	service := &Service{storeID: "11", variantID: "33", expectedTestMode: false}
	var envelope lemonWebhookEnvelope
	envelope.Meta.EventName = "subscription_updated"
	envelope.Data.Type = "subscriptions"
	envelope.Data.ID = "9001"
	envelope.Data.Attributes.StoreID = 11
	envelope.Data.Attributes.VariantID = 44
	if err := service.validateWebhookScope(envelope); err != nil {
		t.Fatalf("plan change update was rejected: %v", err)
	}
}

func TestValidateWebhookScopeRejectsWrongStore(t *testing.T) {
	t.Parallel()
	service := &Service{storeID: "11", variantID: "33", expectedTestMode: false}
	var envelope lemonWebhookEnvelope
	envelope.Meta.EventName = "subscription_created"
	envelope.Data.Type = "subscriptions"
	envelope.Data.ID = "9001"
	envelope.Data.Attributes.StoreID = 99
	envelope.Data.Attributes.VariantID = 33
	if !errors.Is(service.validateWebhookScope(envelope), ErrInvalidWebhook) {
		t.Fatal("wrong store was accepted")
	}
}

func TestRetryDelayIsBoundedAndStable(t *testing.T) {
	t.Parallel()
	worker := &WebhookWorker{cfg: WebhookWorkerConfig{
		BaseRetryDelay: 5 * time.Second,
		MaxRetryDelay:  time.Minute,
	}}
	first := worker.retryDelay(3, "sha256:abc")
	second := worker.retryDelay(3, "sha256:abc")
	if first != second {
		t.Fatalf("retry delay is not stable: %s != %s", first, second)
	}
	if first < 20*time.Second || first > time.Minute {
		t.Fatalf("retry delay = %s, outside expected bounds", first)
	}
	if capped := worker.retryDelay(50, "sha256:abc"); capped != time.Minute {
		t.Fatalf("capped delay = %s, want 1m", capped)
	}
}
