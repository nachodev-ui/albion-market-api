package billing

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

func (s *Service) ProcessWebhookEvent(ctx context.Context, eventID string, raw []byte) (WebhookResult, error) {
	if s == nil || s.db == nil {
		return WebhookResult{}, errors.New("billing service is not configured")
	}
	if !strings.HasPrefix(eventID, "sha256:") || len(eventID) != len("sha256:")+sha256.Size*2 {
		return WebhookResult{}, fmt.Errorf("%w: invalid event identity", ErrInvalidWebhook)
	}

	var envelope productionWebhookEnvelope
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return WebhookResult{}, fmt.Errorf("%w: decode payload", ErrInvalidWebhook)
	}
	eventName := strings.TrimSpace(envelope.Meta.EventName)
	if !isSupportedWebhookEvent(eventName) {
		return WebhookResult{}, fmt.Errorf("%w: unsupported event", ErrInvalidWebhook)
	}
	if err := s.validateWebhookScope(envelope); err != nil {
		return WebhookResult{}, err
	}

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return WebhookResult{}, fmt.Errorf("begin billing webhook transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// The worker increments attempt_count when it atomically leases the job.
	// Lock the same row here so business mutations and the final event status
	// commit as one transaction. Any error therefore rolls back every mutation.
	const lockEvent = `
		select status
		from billing_webhook_events
		where provider = $1 and provider_event_id = $2
		for update
	`
	var eventStatus string
	if err := tx.QueryRow(ctx, lockEvent, s.providerName, eventID).Scan(&eventStatus); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return WebhookResult{}, fmt.Errorf("%w: webhook was not durably enqueued", ErrInvalidWebhook)
		}
		return WebhookResult{}, fmt.Errorf("lock billing webhook: %w", err)
	}
	if eventStatus == "processed" || eventStatus == "ignored" {
		if err := tx.Commit(ctx); err != nil {
			return WebhookResult{}, fmt.Errorf("commit duplicate webhook: %w", err)
		}
		return WebhookResult{Status: eventStatus, Duplicate: true}, nil
	}

	var finalStatus string
	switch eventName {
	case "order_created":
		finalStatus, err = s.processProductionOrder(ctx, tx, eventID, envelope)
	case "subscription_payment_failed", "subscription_payment_recovered", "subscription_payment_success":
		finalStatus, err = s.processProductionPayment(ctx, tx, eventID, envelope)
	default:
		finalStatus, err = s.processProductionSubscription(ctx, tx, envelope)
	}
	if err != nil {
		return WebhookResult{}, err
	}
	if err := finishProductionWebhook(ctx, tx, s.providerName, eventID, finalStatus); err != nil {
		return WebhookResult{}, err
	}
	return WebhookResult{Status: finalStatus}, nil
}
