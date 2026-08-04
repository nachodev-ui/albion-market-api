package billing

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const provisionalOrderAccess = 15 * time.Minute

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

	digest := sha256.Sum256(raw)
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return WebhookResult{}, fmt.Errorf("begin billing webhook transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const recordEvent = `
		insert into billing_webhook_events (
			provider, provider_event_id, event_type, payload_hash,
			status, attempt_count, last_attempt_at
		)
		values ($1, $2, $3, $4, 'processing', 1, now())
		on conflict (provider, provider_event_id)
		do update set
			status = case
				when billing_webhook_events.status in ('processed', 'ignored') then billing_webhook_events.status
				else 'processing'
			end,
			attempt_count = billing_webhook_events.attempt_count + 1,
			last_attempt_at = now()
		returning status
	`
	var eventStatus string
	if err := tx.QueryRow(ctx, recordEvent, s.providerName, eventID, eventName, digest[:]).Scan(&eventStatus); err != nil {
		return WebhookResult{}, fmt.Errorf("record billing webhook: %w", err)
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
		_ = failProductionWebhook(ctx, tx, s.providerName, eventID, err)
		return WebhookResult{}, err
	}
	if err := finishProductionWebhook(ctx, tx, s.providerName, eventID, finalStatus); err != nil {
		return WebhookResult{}, err
	}
	return WebhookResult{Status: finalStatus}, nil
}

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

func resolveProductionWebhookUser(
	ctx context.Context,
	tx pgx.Tx,
	providerName string,
	envelope productionWebhookEnvelope,
) (string, error) {
	userID := customString(envelope.Meta.CustomData, "user_id")
	if userID != "" {
		const existsQuery = `select exists (select 1 from app_users where id = $1::uuid)`
		var exists bool
		if err := tx.QueryRow(ctx, existsQuery, userID).Scan(&exists); err != nil || !exists {
			return "", fmt.Errorf("%w: unknown checkout user", ErrInvalidWebhook)
		}
		return userID, nil
	}

	const existingUser = `
		select user_id::text
		from subscriptions
		where provider = $1 and provider_subscription_id = $2
		limit 1
	`
	if err := tx.QueryRow(ctx, existingUser, providerName, envelope.Data.ID).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("%w: webhook cannot be associated with a user", ErrInvalidWebhook)
		}
		return "", fmt.Errorf("resolve webhook user: %w", err)
	}
	return userID, nil
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

func failProductionWebhook(ctx context.Context, tx pgx.Tx, providerName, eventID string, processingErr error) error {
	const updateEvent = `
		update billing_webhook_events
		set status = 'failed',
			error_message = $3,
			locked_at = null,
			locked_by = null
		where provider = $1 and provider_event_id = $2
	`
	if _, err := tx.Exec(ctx, updateEvent, providerName, eventID, safeProcessingError(processingErr)); err != nil {
		return err
	}
	return tx.Commit(ctx)
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
