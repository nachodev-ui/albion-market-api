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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/accounts"
	"github.com/nachodev-ui/albion-market-api/internal/authn"
)

const lemonSqueezyProvider = "lemonsqueezy"

var (
	ErrAlreadySubscribed   = errors.New("an active billing subscription already exists")
	ErrSubscriptionMissing = errors.New("billing subscription not found")
	ErrInvalidWebhook      = errors.New("invalid billing webhook")
)

type ServiceConfig struct {
	ProviderName      string
	StoreID           string
	VariantID         string
	ExpectedTestMode  bool
	PastDueGracePeriod time.Duration
}

type Service struct {
	db                  *pgxpool.Pool
	accounts            *accounts.Service
	provider            Provider
	providerName        string
	storeID             string
	variantID           string
	expectedTestMode    bool
	pastDueGracePeriod  time.Duration
	now                 func() time.Time
}

func NewService(
	db *pgxpool.Pool,
	accountService *accounts.Service,
	provider Provider,
	cfg ServiceConfig,
) *Service {
	providerName := strings.ToLower(strings.TrimSpace(cfg.ProviderName))
	if providerName == "" {
		providerName = lemonSqueezyProvider
	}
	return &Service{
		db:                 db,
		accounts:           accountService,
		provider:           provider,
		providerName:       providerName,
		storeID:            strings.TrimSpace(cfg.StoreID),
		variantID:          strings.TrimSpace(cfg.VariantID),
		expectedTestMode:   cfg.ExpectedTestMode,
		pastDueGracePeriod: cfg.PastDueGracePeriod,
		now:                time.Now,
	}
}

func (s *Service) CreateCheckout(ctx context.Context, identity authn.Identity) (CheckoutSession, error) {
	if s == nil || s.db == nil || s.accounts == nil || s.provider == nil {
		return CheckoutSession{}, errors.New("billing service is not configured")
	}
	access, err := s.accounts.Current(ctx, identity)
	if err != nil {
		return CheckoutSession{}, err
	}

	const existing = `
		select exists (
			select 1
			from subscriptions
			where user_id = $1::uuid
				and provider = $2
				and provider_subscription_id is not null
				and (
					status in ('trialing', 'active', 'past_due')
					or (status = 'canceled' and access_until > now())
				)
		)
	`
	var alreadySubscribed bool
	if err := s.db.QueryRow(ctx, existing, access.User.ID, s.providerName).Scan(&alreadySubscribed); err != nil {
		return CheckoutSession{}, fmt.Errorf("check existing subscription: %w", err)
	}
	if alreadySubscribed {
		return CheckoutSession{}, ErrAlreadySubscribed
	}

	email := ""
	if access.User.Email != nil {
		email = *access.User.Email
	}
	displayName := ""
	if access.User.DisplayName != nil {
		displayName = *access.User.DisplayName
	}
	return s.provider.CreateCheckout(ctx, CheckoutRequest{
		UserID:      access.User.ID,
		Email:       email,
		DisplayName: displayName,
	})
}

func (s *Service) CustomerPortal(ctx context.Context, identity authn.Identity) (PortalSession, error) {
	if s == nil || s.db == nil || s.accounts == nil || s.provider == nil {
		return PortalSession{}, errors.New("billing service is not configured")
	}
	access, err := s.accounts.Current(ctx, identity)
	if err != nil {
		return PortalSession{}, err
	}

	const latestSubscription = `
		select provider_subscription_id
		from subscriptions
		where user_id = $1::uuid
			and provider = $2
			and provider_subscription_id is not null
		order by updated_at desc
		limit 1
	`
	var providerSubscriptionID string
	if err := s.db.QueryRow(ctx, latestSubscription, access.User.ID, s.providerName).Scan(&providerSubscriptionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PortalSession{}, ErrSubscriptionMissing
		}
		return PortalSession{}, fmt.Errorf("resolve billing subscription: %w", err)
	}
	return s.provider.CustomerPortal(ctx, providerSubscriptionID)
}

type WebhookResult struct {
	Status    string `json:"status"`
	Duplicate bool   `json:"duplicate"`
}

func (s *Service) ProcessWebhook(ctx context.Context, raw []byte) (WebhookResult, error) {
	if s == nil || s.db == nil {
		return WebhookResult{}, errors.New("billing service is not configured")
	}

	var envelope lemonWebhookEnvelope
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return WebhookResult{}, fmt.Errorf("%w: decode payload", ErrInvalidWebhook)
	}
	eventName := strings.TrimSpace(envelope.Meta.EventName)
	if eventName == "" {
		return WebhookResult{}, fmt.Errorf("%w: missing event name", ErrInvalidWebhook)
	}

	digest := sha256.Sum256(raw)
	eventID := fmt.Sprintf("sha256:%x", digest[:])
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
		values ($1, $2, $3, $4, 'pending', 1, now())
		on conflict (provider, provider_event_id)
		do update set
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

	if !isSupportedSubscriptionEvent(eventName) || envelope.Data.Type != "subscriptions" {
		if err := finishWebhook(ctx, tx, s.providerName, eventID, "ignored", ""); err != nil {
			return WebhookResult{}, err
		}
		return WebhookResult{Status: "ignored"}, nil
	}
	if envelope.Data.ID == "" || strconv.FormatInt(envelope.Data.Attributes.StoreID, 10) != s.storeID ||
		strconv.FormatInt(envelope.Data.Attributes.VariantID, 10) != s.variantID ||
		envelope.Data.Attributes.TestMode != s.expectedTestMode {
		if err := finishWebhook(ctx, tx, s.providerName, eventID, "ignored", ""); err != nil {
			return WebhookResult{}, err
		}
		return WebhookResult{Status: "ignored"}, nil
	}

	userID, err := s.resolveWebhookUser(ctx, tx, envelope)
	if err != nil {
		_ = failWebhook(ctx, tx, s.providerName, eventID, err)
		return WebhookResult{}, err
	}

	providerUpdatedAt := parseProviderTime(envelope.Data.Attributes.UpdatedAt)
	if providerUpdatedAt.IsZero() {
		providerUpdatedAt = s.now().UTC()
	}
	status, accessUntil, err := s.resolveAccess(envelope.Data.Attributes, providerUpdatedAt)
	if err != nil {
		_ = failWebhook(ctx, tx, s.providerName, eventID, err)
		return WebhookResult{}, err
	}

	currentPeriodEnd := parseOptionalProviderTime(envelope.Data.Attributes.RenewsAt)
	cancelAtPeriodEnd := envelope.Data.Attributes.Cancelled || status == "canceled"
	const upsertSubscription = `
		insert into subscriptions (
			user_id, provider, provider_customer_id, provider_subscription_id,
			provider_variant_id, provider_status, provider_updated_at,
			plan_code, status, current_period_end, cancel_at_period_end,
			access_until, created_at, updated_at
		)
		values (
			$1::uuid, $2, $3, $4, $5, $6, $7,
			'pro', $8, $9, $10, $11, now(), now()
		)
		on conflict (provider, provider_subscription_id)
		do update set
			user_id = excluded.user_id,
			provider_customer_id = excluded.provider_customer_id,
			provider_variant_id = excluded.provider_variant_id,
			provider_status = excluded.provider_status,
			provider_updated_at = excluded.provider_updated_at,
			plan_code = excluded.plan_code,
			status = excluded.status,
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
		strconv.FormatInt(envelope.Data.Attributes.CustomerID, 10),
		envelope.Data.ID,
		strconv.FormatInt(envelope.Data.Attributes.VariantID, 10),
		envelope.Data.Attributes.Status,
		providerUpdatedAt,
		status,
		currentPeriodEnd,
		cancelAtPeriodEnd,
		accessUntil,
	)
	if err != nil {
		_ = failWebhook(ctx, tx, s.providerName, eventID, err)
		return WebhookResult{}, fmt.Errorf("upsert billing subscription: %w", err)
	}
	finalStatus := "processed"
	if tag.RowsAffected() == 0 {
		finalStatus = "ignored"
	}
	if err := finishWebhook(ctx, tx, s.providerName, eventID, finalStatus, ""); err != nil {
		return WebhookResult{}, err
	}
	return WebhookResult{Status: finalStatus}, nil
}

func (s *Service) resolveWebhookUser(
	ctx context.Context,
	tx pgx.Tx,
	envelope lemonWebhookEnvelope,
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
	if err := tx.QueryRow(ctx, existingUser, s.providerName, envelope.Data.ID).Scan(&userID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("%w: webhook cannot be associated with a user", ErrInvalidWebhook)
		}
		return "", fmt.Errorf("resolve webhook user: %w", err)
	}
	return userID, nil
}

func (s *Service) resolveAccess(attributes lemonSubscriptionAttributes, updatedAt time.Time) (string, *time.Time, error) {
	providerStatus := strings.ToLower(strings.TrimSpace(attributes.Status))
	renewsAt := parseOptionalProviderTime(attributes.RenewsAt)
	endsAt := parseOptionalProviderTime(attributes.EndsAt)

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
		grace := base.Add(s.pastDueGracePeriod)
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

func finishWebhook(
	ctx context.Context,
	tx pgx.Tx,
	providerName, eventID, status, errorMessage string,
) error {
	const updateEvent = `
		update billing_webhook_events
		set status = $3,
			processed_at = now(),
			error_message = nullif($4, '')
		where provider = $1 and provider_event_id = $2
	`
	if _, err := tx.Exec(ctx, updateEvent, providerName, eventID, status, truncateError(errorMessage)); err != nil {
		return fmt.Errorf("finish billing webhook: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit billing webhook: %w", err)
	}
	return nil
}

func failWebhook(ctx context.Context, tx pgx.Tx, providerName, eventID string, processingErr error) error {
	const updateEvent = `
		update billing_webhook_events
		set status = 'failed', error_message = $3
		where provider = $1 and provider_event_id = $2
	`
	if _, err := tx.Exec(ctx, updateEvent, providerName, eventID, truncateError(processingErr.Error())); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func isSupportedSubscriptionEvent(eventName string) bool {
	switch eventName {
	case "subscription_created", "subscription_updated", "subscription_cancelled",
		"subscription_resumed", "subscription_expired", "subscription_paused",
		"subscription_unpaused":
		return true
	default:
		return false
	}
}

func parseProviderTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func parseOptionalProviderTime(value string) *time.Time {
	parsed := parseProviderTime(value)
	if parsed.IsZero() {
		return nil
	}
	return &parsed
}

func customString(values map[string]any, key string) string {
	value, ok := values[key]
	if !ok {
		return ""
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	default:
		return ""
	}
}

func truncateError(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 2000 {
		return value
	}
	return value[:2000]
}

type lemonWebhookEnvelope struct {
	Meta struct {
		EventName  string         `json:"event_name"`
		CustomData map[string]any `json:"custom_data"`
	} `json:"meta"`
	Data struct {
		Type       string                      `json:"type"`
		ID         string                      `json:"id"`
		Attributes lemonSubscriptionAttributes `json:"attributes"`
	} `json:"data"`
}

type lemonSubscriptionAttributes struct {
	StoreID    int64  `json:"store_id"`
	CustomerID int64  `json:"customer_id"`
	VariantID  int64  `json:"variant_id"`
	Status     string `json:"status"`
	Cancelled  bool   `json:"cancelled"`
	RenewsAt   string `json:"renews_at"`
	EndsAt     string `json:"ends_at"`
	UpdatedAt  string `json:"updated_at"`
	TestMode   bool   `json:"test_mode"`
}
