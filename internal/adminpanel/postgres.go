package adminpanel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/accountadmin"
)

type PostgresRepository struct {
	db *pgxpool.Pool
}

func NewPostgresRepository(db *pgxpool.Pool) (*PostgresRepository, error) {
	if db == nil {
		return nil, errors.New("database pool is required")
	}
	return &PostgresRepository{db: db}, nil
}

func (r *PostgresRepository) AdminSession(ctx context.Context, authSubject string) (Session, error) {
	const query = `
		select
			a.id::text,
			u.id::text,
			u.auth_subject,
			u.email,
			u.display_name,
			a.active,
			a.created_at,
			a.disabled_at
		from app_admins a
		join app_users u on u.id = a.user_id
		where u.auth_subject = $1
			and a.active = true
		limit 1
	`
	var session Session
	if err := r.db.QueryRow(ctx, query, authSubject).Scan(
		&session.AdminID,
		&session.UserID,
		&session.AuthSubject,
		&session.Email,
		&session.DisplayName,
		&session.Active,
		&session.CreatedAt,
		&session.DisabledAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Session{}, ErrForbidden
		}
		return Session{}, fmt.Errorf("resolve administrator session: %w", err)
	}
	return session, nil
}

func (r *PostgresRepository) SearchUsers(ctx context.Context, queryText string, limit int) ([]UserSummary, error) {
	const query = `
		select
			u.id::text,
			u.auth_subject,
			u.email,
			u.display_name,
			coalesce(effective.plan_code, 'free'),
			coalesce(effective.status, 'none'),
			effective.access_until,
			manual.provider_subscription_id,
			manual.status,
			manual.access_until,
			manual.updated_at,
			coalesce((
				select array_agg(distinct sx.provider order by sx.provider)
				from subscriptions sx
				where sx.user_id = u.id
					and (
						sx.status in ('trialing', 'active')
						or (sx.status in ('past_due', 'canceled') and sx.access_until > now())
					)
					and (sx.access_until is null or sx.access_until > now())
			), array[]::text[])
		from app_users u
		left join lateral (
			select s.plan_code, s.status, s.access_until
			from subscriptions s
			where s.user_id = u.id
				and (
					s.status in ('trialing', 'active')
					or (s.status in ('past_due', 'canceled') and s.access_until > now())
				)
				and (s.access_until is null or s.access_until > now())
			order by
				case s.status
					when 'active' then 1
					when 'trialing' then 2
					when 'past_due' then 3
					else 4
				end,
				coalesce(s.access_until, 'infinity'::timestamptz) desc,
				s.updated_at desc
			limit 1
		) effective on true
		left join lateral (
			select s.provider_subscription_id, s.status, s.access_until, s.updated_at
			from subscriptions s
			where s.user_id = u.id and s.provider = 'manual'
			order by s.updated_at desc
			limit 1
		) manual on true
		where $1 = ''
			or u.id::text ilike '%' || $1 || '%'
			or u.auth_subject ilike '%' || $1 || '%'
			or coalesce(u.email, '') ilike '%' || $1 || '%'
			or coalesce(u.display_name, '') ilike '%' || $1 || '%'
		order by coalesce(u.last_login_at, u.updated_at, u.created_at) desc, u.id
		limit $2
	`
	rows, err := r.db.Query(ctx, query, queryText, limit)
	if err != nil {
		return nil, fmt.Errorf("search administrator users: %w", err)
	}
	defer rows.Close()

	results := make([]UserSummary, 0)
	for rows.Next() {
		summary, err := scanUserSummary(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate administrator users: %w", err)
	}
	return results, nil
}

func (r *PostgresRepository) UserDetail(ctx context.Context, userID string) (UserDetail, error) {
	const summaryQuery = `
		select
			u.id::text,
			u.auth_subject,
			u.email,
			u.display_name,
			coalesce(effective.plan_code, 'free'),
			coalesce(effective.status, 'none'),
			effective.access_until,
			manual.provider_subscription_id,
			manual.status,
			manual.access_until,
			manual.updated_at,
			coalesce((
				select array_agg(distinct sx.provider order by sx.provider)
				from subscriptions sx
				where sx.user_id = u.id
					and (
						sx.status in ('trialing', 'active')
						or (sx.status in ('past_due', 'canceled') and sx.access_until > now())
					)
					and (sx.access_until is null or sx.access_until > now())
			), array[]::text[])
		from app_users u
		left join lateral (
			select s.plan_code, s.status, s.access_until
			from subscriptions s
			where s.user_id = u.id
				and (
					s.status in ('trialing', 'active')
					or (s.status in ('past_due', 'canceled') and s.access_until > now())
				)
				and (s.access_until is null or s.access_until > now())
			order by
				case s.status
					when 'active' then 1
					when 'trialing' then 2
					when 'past_due' then 3
					else 4
				end,
				coalesce(s.access_until, 'infinity'::timestamptz) desc,
				s.updated_at desc
			limit 1
		) effective on true
		left join lateral (
			select s.provider_subscription_id, s.status, s.access_until, s.updated_at
			from subscriptions s
			where s.user_id = u.id and s.provider = 'manual'
			order by s.updated_at desc
			limit 1
		) manual on true
		where u.id = $1::uuid
	`
	summary, err := scanUserSummary(r.db.QueryRow(ctx, summaryQuery, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserDetail{}, accountadmin.ErrUserNotFound
		}
		return UserDetail{}, err
	}

	const subscriptionsQuery = `
		select id::text, provider, provider_subscription_id, plan_code, status,
			current_period_start, current_period_end, access_until,
			cancel_at_period_end, created_at, updated_at
		from subscriptions
		where user_id = $1::uuid
		order by updated_at desc, id
	`
	rows, err := r.db.Query(ctx, subscriptionsQuery, userID)
	if err != nil {
		return UserDetail{}, fmt.Errorf("query administrator subscriptions: %w", err)
	}
	subscriptions := make([]Subscription, 0)
	for rows.Next() {
		var subscription Subscription
		if err := rows.Scan(
			&subscription.ID,
			&subscription.Provider,
			&subscription.ProviderSubscriptionID,
			&subscription.Plan,
			&subscription.Status,
			&subscription.CurrentPeriodStart,
			&subscription.CurrentPeriodEnd,
			&subscription.AccessUntil,
			&subscription.CancelAtPeriodEnd,
			&subscription.CreatedAt,
			&subscription.UpdatedAt,
		); err != nil {
			rows.Close()
			return UserDetail{}, fmt.Errorf("scan administrator subscription: %w", err)
		}
		subscriptions = append(subscriptions, subscription)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return UserDetail{}, fmt.Errorf("iterate administrator subscriptions: %w", err)
	}
	rows.Close()

	const entitlementsQuery = `
		select entitlement_key, entitlement_value
		from (
			select entitlement_key, entitlement_value, 0 as priority
			from plan_entitlements
			where plan_code = $1
			union all
			select entitlement_key, entitlement_value, 1 as priority
			from user_entitlement_overrides
			where user_id = $2::uuid
				and (expires_at is null or expires_at > now())
		) resolved
		order by entitlement_key, priority
	`
	entitlementRows, err := r.db.Query(ctx, entitlementsQuery, summary.Effective.Plan, userID)
	if err != nil {
		return UserDetail{}, fmt.Errorf("query administrator entitlements: %w", err)
	}
	defer entitlementRows.Close()
	entitlements := make(map[string]any)
	for entitlementRows.Next() {
		var key string
		var raw []byte
		if err := entitlementRows.Scan(&key, &raw); err != nil {
			return UserDetail{}, fmt.Errorf("scan administrator entitlement: %w", err)
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return UserDetail{}, fmt.Errorf("decode administrator entitlement: %w", err)
		}
		entitlements[key] = value
	}
	if err := entitlementRows.Err(); err != nil {
		return UserDetail{}, fmt.Errorf("iterate administrator entitlements: %w", err)
	}

	return UserDetail{UserSummary: summary, Subscriptions: subscriptions, Entitlements: entitlements}, nil
}

func (r *PostgresRepository) AuditEvents(ctx context.Context, limit int) ([]AuditEvent, error) {
	const query = `
		select a.id::text, u.id::text, u.auth_subject, u.email, u.display_name,
			a.actor, a.action, a.reason, a.before_state, a.after_state, a.created_at
		from account_admin_audit_events a
		join app_users u on u.id = a.user_id
		order by a.created_at desc, a.id desc
		limit $1
	`
	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("query administrator audit events: %w", err)
	}
	defer rows.Close()

	events := make([]AuditEvent, 0)
	for rows.Next() {
		var event AuditEvent
		var beforeRaw, afterRaw []byte
		if err := rows.Scan(
			&event.ID,
			&event.User.ID,
			&event.User.AuthSubject,
			&event.User.Email,
			&event.User.DisplayName,
			&event.Actor,
			&event.Action,
			&event.Reason,
			&beforeRaw,
			&afterRaw,
			&event.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan administrator audit event: %w", err)
		}
		if err := json.Unmarshal(beforeRaw, &event.Before); err != nil {
			return nil, fmt.Errorf("decode administrator audit before state: %w", err)
		}
		if err := json.Unmarshal(afterRaw, &event.After); err != nil {
			return nil, fmt.Errorf("decode administrator audit after state: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate administrator audit events: %w", err)
	}
	return events, nil
}

type rowScanner interface {
	Scan(...any) error
}

func scanUserSummary(row rowScanner) (UserSummary, error) {
	var summary UserSummary
	var manualID, manualStatus *string
	var manualAccessUntil, manualUpdatedAt *time.Time
	if err := row.Scan(
		&summary.User.ID,
		&summary.User.AuthSubject,
		&summary.User.Email,
		&summary.User.DisplayName,
		&summary.Effective.Plan,
		&summary.Effective.Status,
		&summary.Effective.AccessUntil,
		&manualID,
		&manualStatus,
		&manualAccessUntil,
		&manualUpdatedAt,
		&summary.ActiveProviders,
	); err != nil {
		return UserSummary{}, err
	}
	if manualID != nil && manualStatus != nil && manualUpdatedAt != nil {
		summary.ManualGrant = &accountadmin.ManualGrant{
			SubscriptionID: *manualID,
			Status:         *manualStatus,
			AccessUntil:    manualAccessUntil,
			UpdatedAt:      *manualUpdatedAt,
		}
	}
	if summary.ActiveProviders == nil {
		summary.ActiveProviders = []string{}
	}
	return summary, nil
}
