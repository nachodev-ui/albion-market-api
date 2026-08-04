package billing

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

type WebhookWorkerConfig struct {
	PollInterval   time.Duration
	JobTimeout     time.Duration
	LeaseDuration  time.Duration
	BaseRetryDelay time.Duration
	MaxRetryDelay  time.Duration
	BatchSize      int
	MaxAttempts    int
}

type WebhookWorker struct {
	service *Service
	db      *pgxpool.Pool
	logger  *observability.Logger
	cfg     WebhookWorkerConfig
	ownerID string
	now     func() time.Time
}

type queuedWebhook struct {
	EventID      string
	EventType    string
	Payload      []byte
	AttemptCount int
}

func NewWebhookWorker(
	service *Service,
	db *pgxpool.Pool,
	logger *observability.Logger,
	cfg WebhookWorkerConfig,
) *WebhookWorker {
	return &WebhookWorker{
		service: service,
		db:      db,
		logger:  logger,
		cfg:     cfg,
		ownerID: fmt.Sprintf("billing-worker-%d", time.Now().UTC().UnixNano()),
		now:     time.Now,
	}
}

func (s *Service) EnqueueWebhook(ctx context.Context, raw []byte, headerEventName string) (WebhookResult, error) {
	if s == nil || s.db == nil {
		return WebhookResult{}, errors.New("billing service is not configured")
	}

	envelope, sanitized, err := validateAndSanitizeWebhook(raw, headerEventName)
	if err != nil {
		return WebhookResult{}, err
	}
	if err := s.validateWebhookScope(envelope); err != nil {
		return WebhookResult{}, err
	}

	digest := sha256.Sum256(raw)
	eventID := fmt.Sprintf("sha256:%x", digest[:])
	const insertEvent = `
		insert into billing_webhook_events (
			provider, provider_event_id, event_type, payload_hash, raw_payload,
			object_type, object_id, status, delivery_count,
			last_received_at, next_attempt_at
		)
		values ($1, $2, $3, $4, $5::jsonb, $6, $7, 'pending', 1, now(), now())
		on conflict (provider, provider_event_id) do nothing
	`
	tag, err := s.db.Exec(
		ctx,
		insertEvent,
		s.providerName,
		eventID,
		envelope.Meta.EventName,
		digest[:],
		string(sanitized),
		envelope.Data.Type,
		envelope.Data.ID,
	)
	if err != nil {
		return WebhookResult{}, fmt.Errorf("enqueue billing webhook: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return WebhookResult{Status: "queued"}, nil
	}

	const touchDuplicate = `
		update billing_webhook_events
		set delivery_count = delivery_count + 1,
			last_received_at = now(),
			next_attempt_at = case
				when status = 'failed' then least(coalesce(next_attempt_at, now()), now())
				else next_attempt_at
			end
		where provider = $1 and provider_event_id = $2
		returning status
	`
	var status string
	if err := s.db.QueryRow(ctx, touchDuplicate, s.providerName, eventID).Scan(&status); err != nil {
		return WebhookResult{}, fmt.Errorf("touch duplicate billing webhook: %w", err)
	}
	return WebhookResult{Status: status, Duplicate: true}, nil
}

func (w *WebhookWorker) Run(ctx context.Context) {
	if w == nil || w.service == nil || w.db == nil || w.cfg.PollInterval <= 0 ||
		w.cfg.JobTimeout <= 0 || w.cfg.LeaseDuration <= 0 || w.cfg.BatchSize <= 0 ||
		w.cfg.MaxAttempts <= 0 {
		return
	}

	w.processBatch(ctx)
	ticker := time.NewTicker(w.cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *WebhookWorker) processBatch(ctx context.Context) {
	for index := 0; index < w.cfg.BatchSize; index++ {
		job, ok, err := w.claim(ctx)
		if err != nil {
			w.logError("billing.webhook_claim_failed", err)
			return
		}
		if !ok {
			return
		}
		w.processOne(ctx, job)
	}
}

func (w *WebhookWorker) claim(ctx context.Context) (queuedWebhook, bool, error) {
	leaseInterval := postgresInterval(w.cfg.LeaseDuration)
	const claim = `
		with candidate as (
			select id
			from billing_webhook_events
			where provider = $1
				and attempt_count < $2
				and (
					(status in ('pending', 'failed') and coalesce(next_attempt_at, created_at) <= now())
					or (status = 'processing' and locked_at < now() - $3::interval)
				)
			order by coalesce(next_attempt_at, created_at), created_at
			for update skip locked
			limit 1
		)
		update billing_webhook_events as event
		set status = 'processing',
			attempt_count = event.attempt_count + 1,
			locked_at = now(),
			locked_by = $4,
			last_attempt_at = now()
		from candidate
		where event.id = candidate.id
		returning event.provider_event_id, event.event_type,
			event.raw_payload::text, event.attempt_count
	`
	var job queuedWebhook
	var payload string
	err := w.db.QueryRow(
		ctx,
		claim,
		w.service.providerName,
		w.cfg.MaxAttempts,
		leaseInterval,
		w.ownerID,
	).Scan(&job.EventID, &job.EventType, &payload, &job.AttemptCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return queuedWebhook{}, false, nil
	}
	if err != nil {
		return queuedWebhook{}, false, err
	}
	job.Payload = []byte(payload)
	return job, true, nil
}

func (w *WebhookWorker) processOne(parent context.Context, job queuedWebhook) {
	started := w.now()
	ctx, cancel := context.WithTimeout(parent, w.cfg.JobTimeout)
	defer cancel()

	result, err := w.service.ProcessWebhookEvent(ctx, job.EventID, job.Payload)
	if err != nil {
		permanent := errors.Is(err, ErrInvalidWebhook)
		if finalizeErr := w.finalizeFailure(parent, job.EventID, permanent, err); finalizeErr != nil {
			w.logError("billing.webhook_finalize_failed", finalizeErr,
				observability.F("provider_event_id", job.EventID),
				observability.F("event_type", job.EventType),
			)
			return
		}
		w.logError("billing.webhook_processing_failed", err,
			observability.F("provider_event_id", job.EventID),
			observability.F("event_type", job.EventType),
			observability.F("permanent", permanent),
			observability.F("duration_ms", float64(time.Since(started).Microseconds())/1000),
		)
		return
	}

	if w.logger != nil {
		w.logger.Success(
			"billing.webhook_processed",
			observability.F("provider_event_id", job.EventID),
			observability.F("event_type", job.EventType),
			observability.F("status", result.Status),
			observability.F("duplicate", result.Duplicate),
			observability.F("duration_ms", float64(time.Since(started).Microseconds())/1000),
		)
	}
}

func (w *WebhookWorker) finalizeFailure(ctx context.Context, eventID string, permanent bool, processingErr error) error {
	const currentAttempts = `
		select attempt_count
		from billing_webhook_events
		where provider = $1 and provider_event_id = $2
	`
	var attempts int
	if err := w.db.QueryRow(ctx, currentAttempts, w.service.providerName, eventID).Scan(&attempts); err != nil {
		return err
	}
	deadLetter := permanent || attempts >= w.cfg.MaxAttempts
	delay := w.retryDelay(attempts, eventID)
	const finalize = `
		update billing_webhook_events
		set status = case when $3 then 'dead_letter' else 'failed' end,
			error_message = $4,
			next_attempt_at = case when $3 then null else now() + $5::interval end,
			dead_letter_at = case when $3 then now() else null end,
			locked_at = null,
			locked_by = null
		where provider = $1 and provider_event_id = $2
	`
	_, err := w.db.Exec(
		ctx,
		finalize,
		w.service.providerName,
		eventID,
		deadLetter,
		safeProcessingError(processingErr),
		postgresInterval(delay),
	)
	return err
}

func (w *WebhookWorker) retryDelay(attempt int, eventID string) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	shift := attempt - 1
	if shift > 20 {
		shift = 20
	}
	delay := w.cfg.BaseRetryDelay * time.Duration(1<<shift)
	if delay <= 0 || delay > w.cfg.MaxRetryDelay {
		delay = w.cfg.MaxRetryDelay
	}
	if delay <= 0 {
		return time.Second
	}

	// Stable 0-20% jitter prevents a thundering herd without global RNG state.
	digest := sha256.Sum256([]byte(eventID + ":" + strconv.Itoa(attempt)))
	jitterPercent := int(digest[0]) % 21
	jitter := delay * time.Duration(jitterPercent) / 100
	if delay+jitter > w.cfg.MaxRetryDelay && w.cfg.MaxRetryDelay > 0 {
		return w.cfg.MaxRetryDelay
	}
	return delay + jitter
}

func validateAndSanitizeWebhook(raw []byte, headerEventName string) (productionWebhookEnvelope, []byte, error) {
	var envelope productionWebhookEnvelope
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&envelope); err != nil {
		return productionWebhookEnvelope{}, nil, fmt.Errorf("%w: decode payload", ErrInvalidWebhook)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return productionWebhookEnvelope{}, nil, fmt.Errorf("%w: trailing JSON content", ErrInvalidWebhook)
	}
	if strings.TrimSpace(envelope.Meta.EventName) == "" || envelope.Meta.EventName != strings.TrimSpace(headerEventName) {
		return productionWebhookEnvelope{}, nil, fmt.Errorf("%w: event header mismatch", ErrInvalidWebhook)
	}
	if !isSupportedWebhookEvent(envelope.Meta.EventName) {
		return productionWebhookEnvelope{}, nil, fmt.Errorf("%w: unsupported event", ErrInvalidWebhook)
	}
	if strings.TrimSpace(envelope.Data.Type) == "" || strings.TrimSpace(envelope.Data.ID) == "" || len(envelope.Data.ID) > 128 {
		return productionWebhookEnvelope{}, nil, fmt.Errorf("%w: invalid data identity", ErrInvalidWebhook)
	}
	if userID := customString(envelope.Meta.CustomData, "user_id"); userID != "" && !looksLikeUUID(userID) {
		return productionWebhookEnvelope{}, nil, fmt.Errorf("%w: invalid checkout user", ErrInvalidWebhook)
	}

	sanitizedEnvelope := productionWebhookEnvelope{}
	sanitizedEnvelope.Meta.EventName = envelope.Meta.EventName
	if userID := customString(envelope.Meta.CustomData, "user_id"); userID != "" {
		sanitizedEnvelope.Meta.CustomData = map[string]any{"user_id": userID}
	}
	sanitizedEnvelope.Data.Type = envelope.Data.Type
	sanitizedEnvelope.Data.ID = envelope.Data.ID
	sanitizedEnvelope.Data.Attributes = envelope.Data.Attributes
	sanitized, err := json.Marshal(sanitizedEnvelope)
	if err != nil {
		return productionWebhookEnvelope{}, nil, fmt.Errorf("sanitize billing webhook: %w", err)
	}
	return envelope, sanitized, nil
}

func (s *Service) validateWebhookScope(envelope productionWebhookEnvelope) error {
	attributes := envelope.Data.Attributes
	if strconv.FormatInt(attributes.StoreID, 10) != s.storeID || attributes.TestMode != s.expectedTestMode {
		return fmt.Errorf("%w: webhook scope mismatch", ErrInvalidWebhook)
	}

	switch envelope.Meta.EventName {
	case "order_created":
		if envelope.Data.Type != "orders" || attributes.Status != "paid" ||
			strconv.FormatInt(attributes.FirstOrderItem.VariantID, 10) != s.variantID {
			return fmt.Errorf("%w: invalid order webhook", ErrInvalidWebhook)
		}
	case "subscription_payment_failed", "subscription_payment_recovered", "subscription_payment_success":
		if envelope.Data.Type != "subscription-invoices" || attributes.SubscriptionID <= 0 {
			return fmt.Errorf("%w: invalid subscription invoice webhook", ErrInvalidWebhook)
		}
	case "subscription_created":
		if envelope.Data.Type != "subscriptions" ||
			strconv.FormatInt(attributes.VariantID, 10) != s.variantID {
			return fmt.Errorf("%w: invalid subscription webhook", ErrInvalidWebhook)
		}
	default:
		// Updates may legitimately move the subscription to another variant.
		// Keep accepting the provider object so the service can revoke Pro access.
		if envelope.Data.Type != "subscriptions" || attributes.VariantID <= 0 {
			return fmt.Errorf("%w: invalid subscription webhook", ErrInvalidWebhook)
		}
	}
	return nil
}

func isSupportedWebhookEvent(eventName string) bool {
	switch strings.TrimSpace(eventName) {
	case "order_created",
		"subscription_created",
		"subscription_updated",
		"subscription_cancelled",
		"subscription_resumed",
		"subscription_expired",
		"subscription_paused",
		"subscription_unpaused",
		"subscription_payment_failed",
		"subscription_payment_recovered",
		"subscription_payment_success":
		return true
	default:
		return false
	}
}

func looksLikeUUID(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}

func safeProcessingError(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded):
		return "processing_timeout"
	case errors.Is(err, context.Canceled):
		return "processing_cancelled"
	case errors.Is(err, ErrInvalidWebhook):
		return "invalid_webhook"
	default:
		return "processing_failed"
	}
}

func postgresInterval(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	return fmt.Sprintf("%f seconds", value.Seconds())
}

func (w *WebhookWorker) logError(event string, err error, fields ...observability.Field) {
	if w.logger == nil {
		return
	}
	fields = append(fields, observability.F("error_code", safeProcessingError(err)))
	w.logger.Error(event, fields...)
}
