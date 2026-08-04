package billing

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

const provisionalOrderAccess = 15 * time.Minute

func (s *Service) processProductionOrder(
	ctx context.Context,
	tx pgx.Tx,
	eventID string,
	envelope productionWebhookEnvelope,
) (string, error) {
	userID, err := resolveProductionWebhookUser(ctx, tx, s.providerName, envelope)
	if err != nil {
		return "", err
	}
	attributes := envelope.Data.Attributes
	providerUpdatedAt := productionProviderTime(attributes.UpdatedAt)
	if providerUpdatedAt.IsZero() {
		providerUpdatedAt = productionProviderTime(attributes.CreatedAt)
	}
	if providerUpdatedAt.IsZero() {
		providerUpdatedAt = s.now().UTC()
	}

	const upsertOrder = `
		insert into billing_orders (
			user_id, provider, provider_order_id, provider_customer_id,
			provider_variant_id, provider_status, provider_updated_at,
			created_at, updated_at
		)
		values ($1::uuid, $2, $3, $4, $5, $6, $7, now(), now())
		on conflict (provider, provider_order_id)
		do update set
			user_id = excluded.user_id,
			provider_customer_id = excluded.provider_customer_id,
			provider_variant_id = excluded.provider_variant_id,
			provider_status = excluded.provider_status,
			provider_updated_at = excluded.provider_updated_at,
			updated_at = now()
		where billing_orders.provider_updated_at is null
			or excluded.provider_updated_at >= billing_orders.provider_updated_at
	`
	if _, err := tx.Exec(
		ctx,
		upsertOrder,
		userID,
		s.providerName,
		envelope.Data.ID,
		strconv.FormatInt(attributes.CustomerID, 10),
		strconv.FormatInt(attributes.FirstOrderItem.VariantID, 10),
		attributes.Status,
		providerUpdatedAt,
	); err != nil {
		return "", fmt.Errorf("upsert billing order: %w", err)
	}

	// Lemon Squeezy emits subscription_created with order_created. A bounded
	// provisional grant prevents visible checkout latency without permanent access.
	accessUntil := s.now().UTC().Add(provisionalOrderAccess)
	const provisional = `
		insert into subscriptions (
			user_id, provider, provider_customer_id, provider_subscription_id,
			provider_order_id, provider_variant_id, provider_status, provider_updated_at,
			plan_code, status, current_period_start, current_period_end,
			cancel_at_period_end, access_until, created_at, updated_at
		)
		select
			$1::uuid, $2, $3, null, $4, $5, 'order_created', $6,
			'pro', 'active', now(), $7, false, $7, now(), now()
		where not exists (
			select 1
			from subscriptions
			where provider = $2 and provider_order_id = $4
		)
	`
	if _, err := tx.Exec(
		ctx,
		provisional,
		userID,
		s.providerName,
		strconv.FormatInt(attributes.CustomerID, 10),
		envelope.Data.ID,
		strconv.FormatInt(attributes.FirstOrderItem.VariantID, 10),
		providerUpdatedAt,
		accessUntil,
	); err != nil {
		return "", fmt.Errorf("create provisional billing access: %w", err)
	}

	if err := enqueueProductionNotification(ctx, tx, userID, s.providerName, eventID, "order_confirmed"); err != nil {
		return "", err
	}
	return "processed", nil
}
