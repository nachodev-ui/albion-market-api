package billing

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func finishProductionWebhook(ctx context.Context, tx pgx.Tx, providerName, eventID, status string) error {
	const updateEvent = `
		update billing_webhook_events
		set status = $3,
			processed_at = now(),
			error_message = null,
			next_attempt_at = null,
			locked_at = null,
			locked_by = null,
			raw_payload = '{}'::jsonb
		where provider = $1 and provider_event_id = $2
	`
	if _, err := tx.Exec(ctx, updateEvent, providerName, eventID, status); err != nil {
		return fmt.Errorf("finish billing webhook: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit billing webhook: %w", err)
	}
	return nil
}

func productionProviderTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func productionOptionalProviderTime(value string) *time.Time {
	parsed := productionProviderTime(value)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}

type productionWebhookEnvelope struct {
	Meta struct {
		EventName  string         `json:"event_name"`
		CustomData map[string]any `json:"custom_data,omitempty"`
	} `json:"meta"`
	Data struct {
		Type       string                      `json:"type"`
		ID         string                      `json:"id"`
		Attributes productionWebhookAttributes `json:"attributes"`
	} `json:"data"`
}

type productionWebhookAttributes struct {
	StoreID        int64  `json:"store_id"`
	CustomerID     int64  `json:"customer_id"`
	OrderID        int64  `json:"order_id"`
	SubscriptionID int64  `json:"subscription_id"`
	VariantID      int64  `json:"variant_id"`
	Status         string `json:"status"`
	Cancelled      bool   `json:"cancelled"`
	RenewsAt       string `json:"renews_at"`
	EndsAt         string `json:"ends_at"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
	TestMode       bool   `json:"test_mode"`
	FirstOrderItem struct {
		VariantID int64 `json:"variant_id"`
	} `json:"first_order_item"`
}
