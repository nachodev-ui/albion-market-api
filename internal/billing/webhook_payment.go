package billing

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5"
)

func (s *Service) processProductionPayment(
	ctx context.Context,
	tx pgx.Tx,
	eventID string,
	envelope productionWebhookEnvelope,
) (string, error) {
	subscriptionID := strconv.FormatInt(envelope.Data.Attributes.SubscriptionID, 10)
	const resolve = `
		select user_id::text
		from subscriptions
		where provider = $1 and provider_subscription_id = $2
		limit 1
	`
	var userID string
	if err := tx.QueryRow(ctx, resolve, s.providerName, subscriptionID).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("billing subscription is not synchronized yet")
		}
		return "", fmt.Errorf("resolve payment subscription: %w", err)
	}

	switch envelope.Meta.EventName {
	case "subscription_payment_failed":
		const markPastDue = `
			update subscriptions
			set provider_status = 'past_due',
				status = 'past_due',
				access_until = greatest(coalesce(current_period_end, now()), now()) + $3::interval,
				updated_at = now()
			where provider = $1 and provider_subscription_id = $2
		`
		if _, err := tx.Exec(ctx, markPastDue, s.providerName, subscriptionID, postgresInterval(s.pastDueGracePeriod)); err != nil {
			return "", fmt.Errorf("mark subscription past due: %w", err)
		}
		if err := enqueueProductionNotification(ctx, tx, userID, s.providerName, eventID, "payment_failed"); err != nil {
			return "", err
		}
	case "subscription_payment_recovered":
		const recoverPayment = `
			update subscriptions
			set provider_status = 'active',
				status = 'active',
				access_until = greatest(
					coalesce(access_until, now()),
					coalesce(current_period_end, now()),
					now()
				),
				updated_at = now()
			where provider = $1 and provider_subscription_id = $2
		`
		if _, err := tx.Exec(ctx, recoverPayment, s.providerName, subscriptionID); err != nil {
			return "", fmt.Errorf("recover subscription payment: %w", err)
		}
		if err := enqueueProductionNotification(ctx, tx, userID, s.providerName, eventID, "payment_recovered"); err != nil {
			return "", err
		}
	case "subscription_payment_success":
		if err := enqueueProductionNotification(ctx, tx, userID, s.providerName, eventID, "payment_success"); err != nil {
			return "", err
		}
	}
	return "processed", nil
}

func enqueueProductionNotification(
	ctx context.Context,
	tx pgx.Tx,
	userID, providerName, eventID, notificationType string,
) error {
	const insert = `
		insert into billing_notification_outbox (
			user_id, provider, provider_event_id, notification_type,
			payload, status, next_attempt_at, created_at, updated_at
		)
		values ($1::uuid, $2, $3, $4, '{}'::jsonb, 'pending', now(), now(), now())
		on conflict (provider, provider_event_id, notification_type) do nothing
	`
	if _, err := tx.Exec(ctx, insert, userID, providerName, eventID, notificationType); err != nil {
		return fmt.Errorf("enqueue billing notification: %w", err)
	}
	return nil
}
