package billing

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Service) processProductionSubscription(
	ctx context.Context,
	tx pgx.Tx,
	envelope productionWebhookEnvelope,
) (string, error) {
	userID, err := resolveProductionWebhookUser(ctx, tx, s.providerName, envelope)
	if err != nil {
		return "", err
	}
	attributes := envelope.Data.Attributes
	providerUpdatedAt := productionProviderTime(attributes.UpdatedAt)
	if providerUpdatedAt.IsZero() {
		providerUpdatedAt = s.now().UTC()
	}

	status, accessUntil, err := resolveProductionAccess(attributes, providerUpdatedAt, s.pastDueGracePeriod)
	if err != nil {
		return "", err
	}
	planCode := "pro"
	if strconv.FormatInt(attributes.VariantID, 10) != s.variantID {
		planCode = "free"
		status = "expired"
		value := providerUpdatedAt
		accessUntil = &value
	}

	orderID := ""
	if attributes.OrderID > 0 {
		orderID = strconv.FormatInt(attributes.OrderID, 10)
		const removeProvisional = `
			delete from subscriptions
			where provider = $1
				and provider_order_id = $2
				and provider_subscription_id is null
		`
		if _, err := tx.Exec(ctx, removeProvisional, s.providerName, orderID); err != nil {
			return "", fmt.Errorf("remove provisional billing access: %w", err)
		}
	}

	currentPeriodStart := productionOptionalProviderTime(attributes.CreatedAt)
	currentPeriodEnd := productionOptionalProviderTime(attributes.RenewsAt)
	cancelAtPeriodEnd := attributes.Cancelled || status == "canceled"
	const upsertSubscription = `
		insert into subscriptions (
			user_id, provider, provider_customer_id, provider_subscription_id,
			provider_order_id, provider_variant_id, provider_status, provider_updated_at,
			plan_code, status, current_period_start, current_period_end,
			cancel_at_period_end, access_until, created_at, updated_at
		)
		values (
			$1::uuid, $2, $3, $4, nullif($5, ''), $6, $7, $8,
			$9, $10, $11, $12, $13, $14, now(), now()
		)
		on conflict (provider, provider_subscription_id)
		do update set
			user_id = excluded.user_id,
			provider_customer_id = excluded.provider_customer_id,
			provider_order_id = excluded.provider_order_id,
			provider_variant_id = excluded.provider_variant_id,
			provider_status = excluded.provider_status,
			provider_updated_at = excluded.provider_updated_at,
			plan_code = excluded.plan_code,
			status = excluded.status,
			current_period_start = coalesce(excluded.current_period_start, subscriptions.current_period_start),
			current_period_end = excluded.current_period_end,
			cancel_at_period_end = excluded.cancel_at_period_end,
			access_until = excluded.access_until,
			updated_at = now()
		where subscriptions.provider_updated_at is null
			or excluded.provider_updated_at >= subscriptions.provider_updated_at
	`
	tag, err := tx.Exec(
		ctx,
		upsertSubscription,
		userID,
		s.providerName,
		strconv.FormatInt(attributes.CustomerID, 10),
		envelope.Data.ID,
		orderID,
		strconv.FormatInt(attributes.VariantID, 10),
		attributes.Status,
		providerUpdatedAt,
		planCode,
		status,
		currentPeriodStart,
		currentPeriodEnd,
		cancelAtPeriodEnd,
		accessUntil,
	)
	if err != nil {
		return "", fmt.Errorf("upsert billing subscription: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "ignored", nil
	}
	return "processed", nil
}

func resolveProductionAccess(
	attributes productionWebhookAttributes,
	updatedAt time.Time,
	gracePeriod time.Duration,
) (string, *time.Time, error) {
	providerStatus := strings.ToLower(strings.TrimSpace(attributes.Status))
	renewsAt := productionOptionalProviderTime(attributes.RenewsAt)
	endsAt := productionOptionalProviderTime(attributes.EndsAt)

	var status string
	var accessUntil *time.Time
	switch providerStatus {
	case "on_trial":
		status = "trialing"
		accessUntil = renewsAt
	case "active":
		status = "active"
		accessUntil = renewsAt
	case "paused", "past_due", "unpaid":
		status = "past_due"
		base := updatedAt
		if renewsAt != nil && renewsAt.After(base) {
			base = *renewsAt
		}
		grace := base.Add(gracePeriod)
		accessUntil = &grace
	case "cancelled":
		status = "canceled"
		accessUntil = endsAt
		if accessUntil == nil {
			accessUntil = renewsAt
		}
	case "expired":
		status = "expired"
		accessUntil = endsAt
		if accessUntil == nil {
			value := updatedAt
			accessUntil = &value
		}
	default:
		return "", nil, fmt.Errorf("%w: unsupported subscription status", ErrInvalidWebhook)
	}
	if status != "expired" && accessUntil == nil {
		return "", nil, fmt.Errorf("%w: subscription access date is missing", ErrInvalidWebhook)
	}
	return status, accessUntil, nil
}
