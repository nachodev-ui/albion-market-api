package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

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
