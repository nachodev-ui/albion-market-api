package accountadmin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	db  *pgxpool.Pool
	now func() time.Time
}

func NewPostgresRepository(db *pgxpool.Pool) (*PostgresRepository, error) {
	if db == nil {
		return nil, errors.New("database pool is required")
	}
	return &PostgresRepository{db: db, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (r *PostgresRepository) GrantPro(ctx context.Context, request GrantRequest) (OperationResult, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return OperationResult{}, fmt.Errorf("begin grant transaction: %w", err)
	}
	defer tx.Rollback(context.Background())

	user, err := resolveUser(ctx, tx, request.Selector, true)
	if err != nil {
		return OperationResult{}, err
	}
	before, err := resolveEffectiveAccess(ctx, tx, user.ID, "")
	if err != nil {
		return OperationResult{}, err
	}
	manual, err := resolveManualGrant(ctx, tx, user.ID, true)
	if err != nil {
		return OperationResult{}, err
	}

	result := OperationResult{
		Action:      "grant_pro",
		DryRun:      request.DryRun,
		User:        user,
		Before:      before,
		After:       before,
		ManualGrant: manual,
	}
	if activeManualGrant(manual, r.now()) {
		if err := tx.Commit(ctx); err != nil {
			return OperationResult{}, fmt.Errorf("commit grant no-op: %w", err)
		}
		return result, nil
	}

	now := r.now()
	accessUntil := now.Add(request.Duration)
	preview := &ManualGrant{
		SubscriptionID: manualSubscriptionID(user.ID),
		Status:         "active",
		AccessUntil:    &accessUntil,
		UpdatedAt:      now,
	}
	result.Changed = true
	result.ManualGrant = preview
	result.After = AccessSnapshot{Plan: "pro", Status: "active", AccessUntil: &accessUntil}
	if request.DryRun {
		if err := tx.Commit(ctx); err != nil {
			return OperationResult{}, fmt.Errorf("commit grant dry-run: %w", err)
		}
		return result, nil
	}

	manual, err = upsertManualGrant(ctx, tx, user.ID, now, accessUntil)
	if err != nil {
		return OperationResult{}, err
	}
	after, err := resolveEffectiveAccess(ctx, tx, user.ID, "")
	if err != nil {
		return OperationResult{}, err
	}
	if err := insertAudit(ctx, tx, user.ID, request.Actor, "grant_pro", request.Reason, before, after); err != nil {
		return OperationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OperationResult{}, fmt.Errorf("commit grant transaction: %w", err)
	}
	result.After = after
	result.ManualGrant = manual
	return result, nil
}

func (r *PostgresRepository) RevokePro(ctx context.Context, request RevokeRequest) (OperationResult, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return OperationResult{}, fmt.Errorf("begin revoke transaction: %w", err)
	}
	defer tx.Rollback(context.Background())

	user, err := resolveUser(ctx, tx, request.Selector, true)
	if err != nil {
		return OperationResult{}, err
	}
	before, err := resolveEffectiveAccess(ctx, tx, user.ID, "")
	if err != nil {
		return OperationResult{}, err
	}
	manual, err := resolveManualGrant(ctx, tx, user.ID, true)
	if err != nil {
		return OperationResult{}, err
	}
	result := OperationResult{
		Action:      "revoke_pro",
		DryRun:      request.DryRun,
		User:        user,
		Before:      before,
		After:       before,
		ManualGrant: manual,
	}
	if !activeManualGrant(manual, r.now()) {
		if err := tx.Commit(ctx); err != nil {
			return OperationResult{}, fmt.Errorf("commit revoke no-op: %w", err)
		}
		return result, nil
	}

	afterWithoutManual, err := resolveEffectiveAccess(ctx, tx, user.ID, manual.SubscriptionID)
	if err != nil {
		return OperationResult{}, err
	}
	result.Changed = true
	result.After = afterWithoutManual
	if request.DryRun {
		if err := tx.Commit(ctx); err != nil {
			return OperationResult{}, fmt.Errorf("commit revoke dry-run: %w", err)
		}
		return result, nil
	}

	now := r.now()
	const revoke = `
		update subscriptions
		set status = 'expired',
			current_period_end = $2,
			access_until = $2,
			cancel_at_period_end = false,
			updated_at = $2
		where user_id = $1::uuid
			and provider = 'manual'
		returning provider_subscription_id, status, access_until, updated_at
	`
	var revoked ManualGrant
	if err := tx.QueryRow(ctx, revoke, user.ID, now).Scan(
		&revoked.SubscriptionID,
		&revoked.Status,
		&revoked.AccessUntil,
		&revoked.UpdatedAt,
	); err != nil {
		return OperationResult{}, fmt.Errorf("revoke manual Pro: %w", err)
	}
	after, err := resolveEffectiveAccess(ctx, tx, user.ID, "")
	if err != nil {
		return OperationResult{}, err
	}
	if err := insertAudit(ctx, tx, user.ID, request.Actor, "revoke_pro", request.Reason, before, after); err != nil {
		return OperationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return OperationResult{}, fmt.Errorf("commit revoke transaction: %w", err)
	}
	result.After = after
	result.ManualGrant = &revoked
	return result, nil
}

func (r *PostgresRepository) Status(ctx context.Context, selector Selector) (StatusResult, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return StatusResult{}, fmt.Errorf("begin status transaction: %w", err)
	}
	defer tx.Rollback(context.Background())

	user, err := resolveUser(ctx, tx, selector, false)
	if err != nil {
		return StatusResult{}, err
	}
	effective, err := resolveEffectiveAccess(ctx, tx, user.ID, "")
	if err != nil {
		return StatusResult{}, err
	}
	manual, err := resolveManualGrant(ctx, tx, user.ID, false)
	if err != nil {
		return StatusResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return StatusResult{}, fmt.Errorf("commit status transaction: %w", err)
	}
	return StatusResult{User: user, Effective: effective, ManualGrant: manual}, nil
}

func (r *PostgresRepository) ListActiveManualGrants(ctx context.Context, limit int) ([]StatusResult, error) {
	const query = `
		select
			u.id::text, u.auth_subject, u.email, u.display_name,
			s.provider_subscription_id, s.status, s.access_until, s.updated_at
		from subscriptions s
		join app_users u on u.id = s.user_id
		where s.provider = 'manual'
			and s.status in ('trialing', 'active')
			and (s.access_until is null or s.access_until > now())
		order by coalesce(s.access_until, 'infinity'::timestamptz), u.id
		limit $1
	`
	rows, err := r.db.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("list active manual grants: %w", err)
	}
	defer rows.Close()

	results := make([]StatusResult, 0)
	for rows.Next() {
		var user User
		var manual ManualGrant
		if err := rows.Scan(
			&user.ID,
			&user.AuthSubject,
			&user.Email,
			&user.DisplayName,
			&manual.SubscriptionID,
			&manual.Status,
			&manual.AccessUntil,
			&manual.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan active manual grant: %w", err)
		}
		results = append(results, StatusResult{
			User:        user,
			Effective:   AccessSnapshot{Plan: "pro", Status: manual.Status, AccessUntil: manual.AccessUntil},
			ManualGrant: &manual,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active manual grants: %w", err)
	}
	return results, nil
}

func (r *PostgresRepository) VerifyLifecycle(ctx context.Context, request VerifyRequest) (LifecycleVerification, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return LifecycleVerification{}, fmt.Errorf("begin lifecycle verification: %w", err)
	}
	defer tx.Rollback(context.Background())

	now := r.now()
	subject := fmt.Sprintf("account-admin-e2e:%d", now.UnixNano())
	const createUser = `
		insert into app_users (auth_subject, email, display_name, last_login_at)
		values ($1, null, 'Account Admin E2E', $2)
		returning id::text
	`
	var userID string
	if err := tx.QueryRow(ctx, createUser, subject, now).Scan(&userID); err != nil {
		return LifecycleVerification{}, fmt.Errorf("create lifecycle test user: %w", err)
	}

	freeBefore, err := resolveEffectiveAccess(ctx, tx, userID, "")
	if err != nil {
		return LifecycleVerification{}, err
	}
	if freeBefore.Plan != "free" {
		return LifecycleVerification{}, fmt.Errorf("lifecycle expected free before grant, got %s", freeBefore.Plan)
	}

	accessUntil := now.Add(24 * time.Hour)
	if _, err := upsertManualGrant(ctx, tx, userID, now, accessUntil); err != nil {
		return LifecycleVerification{}, err
	}
	proGranted, err := resolveEffectiveAccess(ctx, tx, userID, "")
	if err != nil {
		return LifecycleVerification{}, err
	}
	if proGranted.Plan != "pro" || proGranted.Status != "active" {
		return LifecycleVerification{}, fmt.Errorf("lifecycle expected active Pro after grant, got %s/%s", proGranted.Plan, proGranted.Status)
	}
	if err := assertEntitlement(ctx, tx, "pro", "optimizer.liquidity", "true"); err != nil {
		return LifecycleVerification{}, err
	}
	if err := insertAudit(ctx, tx, userID, request.Actor, "grant_pro", request.Reason, freeBefore, proGranted); err != nil {
		return LifecycleVerification{}, err
	}

	const revoke = `
		update subscriptions
		set status = 'expired', current_period_end = $2, access_until = $2, updated_at = $2
		where user_id = $1::uuid and provider = 'manual'
	`
	if _, err := tx.Exec(ctx, revoke, userID, now.Add(time.Second)); err != nil {
		return LifecycleVerification{}, fmt.Errorf("revoke lifecycle grant: %w", err)
	}
	freeAfter, err := resolveEffectiveAccess(ctx, tx, userID, "")
	if err != nil {
		return LifecycleVerification{}, err
	}
	if freeAfter.Plan != "free" {
		return LifecycleVerification{}, fmt.Errorf("lifecycle expected free after revoke, got %s", freeAfter.Plan)
	}
	if err := assertEntitlement(ctx, tx, "free", "optimizer.liquidity", "false"); err != nil {
		return LifecycleVerification{}, err
	}
	if err := insertAudit(ctx, tx, userID, request.Actor, "revoke_pro", request.Reason, proGranted, freeAfter); err != nil {
		return LifecycleVerification{}, err
	}

	const auditCount = `select count(*) from account_admin_audit_events where user_id = $1::uuid`
	var count int
	if err := tx.QueryRow(ctx, auditCount, userID).Scan(&count); err != nil {
		return LifecycleVerification{}, fmt.Errorf("count lifecycle audit events: %w", err)
	}
	if count != 2 {
		return LifecycleVerification{}, fmt.Errorf("lifecycle audit event count=%d, want 2", count)
	}

	if err := tx.Rollback(ctx); err != nil {
		return LifecycleVerification{}, fmt.Errorf("rollback lifecycle verification: %w", err)
	}
	return LifecycleVerification{
		FreeBefore:  freeBefore,
		ProGranted:  proGranted,
		FreeAfter:   freeAfter,
		AuditEvents: count,
		RolledBack:  true,
	}, nil
}

func resolveUser(ctx context.Context, tx pgx.Tx, selector Selector, lock bool) (User, error) {
	selector = selector.Normalize()
	lockClause := ""
	if lock {
		lockClause = " for update"
	}
	var query string
	var argument string
	switch {
	case selector.UserID != "":
		query = `select id::text, auth_subject, email, display_name from app_users where id = $1::uuid` + lockClause
		argument = selector.UserID
	case selector.AuthSubject != "":
		query = `select id::text, auth_subject, email, display_name from app_users where auth_subject = $1` + lockClause
		argument = selector.AuthSubject
	case selector.Email != "":
		query = `
			select id::text, auth_subject, email, display_name
			from app_users
			where lower(email) = lower($1)
			order by last_login_at desc nulls last, updated_at desc, id
			limit 2` + lockClause
		argument = selector.Email
	default:
		return User{}, errors.New("user selector is empty")
	}

	rows, err := tx.Query(ctx, query, argument)
	if err != nil {
		return User{}, fmt.Errorf("resolve account user: %w", err)
	}
	defer rows.Close()
	users := make([]User, 0, 2)
	for rows.Next() {
		var user User
		if err := rows.Scan(&user.ID, &user.AuthSubject, &user.Email, &user.DisplayName); err != nil {
			return User{}, fmt.Errorf("scan account user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return User{}, fmt.Errorf("iterate account users: %w", err)
	}
	if len(users) == 0 {
		return User{}, ErrUserNotFound
	}
	if len(users) > 1 {
		return User{}, ErrAmbiguousUser
	}
	return users[0], nil
}

func resolveEffectiveAccess(ctx context.Context, tx pgx.Tx, userID, excludedSubscriptionID string) (AccessSnapshot, error) {
	const query = `
		select plan_code, status, access_until
		from subscriptions
		where user_id = $1::uuid
			and ($2 = '' or provider_subscription_id is distinct from $2)
			and (
				status in ('trialing', 'active')
				or (status in ('past_due', 'canceled') and access_until > now())
			)
			and (access_until is null or access_until > now())
		order by
			case status
				when 'active' then 1
				when 'trialing' then 2
				when 'past_due' then 3
				else 4
			end,
			coalesce(access_until, 'infinity'::timestamptz) desc,
			updated_at desc
		limit 1
	`
	result := AccessSnapshot{Plan: "free", Status: "none"}
	if err := tx.QueryRow(ctx, query, userID, excludedSubscriptionID).Scan(
		&result.Plan,
		&result.Status,
		&result.AccessUntil,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return result, nil
		}
		return AccessSnapshot{}, fmt.Errorf("resolve effective access: %w", err)
	}
	return result, nil
}

func resolveManualGrant(ctx context.Context, tx pgx.Tx, userID string, lock bool) (*ManualGrant, error) {
	query := `
		select provider_subscription_id, status, access_until, updated_at
		from subscriptions
		where user_id = $1::uuid and provider = 'manual'
		limit 1`
	if lock {
		query += " for update"
	}
	var grant ManualGrant
	if err := tx.QueryRow(ctx, query, userID).Scan(
		&grant.SubscriptionID,
		&grant.Status,
		&grant.AccessUntil,
		&grant.UpdatedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve manual grant: %w", err)
	}
	return &grant, nil
}

func upsertManualGrant(ctx context.Context, tx pgx.Tx, userID string, now, accessUntil time.Time) (*ManualGrant, error) {
	const query = `
		insert into subscriptions (
			user_id, provider, provider_subscription_id, plan_code, status,
			current_period_start, current_period_end, access_until, cancel_at_period_end
		)
		values ($1::uuid, 'manual', $2, 'pro', 'active', $3, $4, $4, false)
		on conflict (provider, provider_subscription_id)
		do update set
			plan_code = excluded.plan_code,
			status = excluded.status,
			current_period_start = excluded.current_period_start,
			current_period_end = excluded.current_period_end,
			access_until = excluded.access_until,
			cancel_at_period_end = false,
			updated_at = $3
		returning provider_subscription_id, status, access_until, updated_at
	`
	var grant ManualGrant
	if err := tx.QueryRow(ctx, query, userID, manualSubscriptionID(userID), now, accessUntil).Scan(
		&grant.SubscriptionID,
		&grant.Status,
		&grant.AccessUntil,
		&grant.UpdatedAt,
	); err != nil {
		return nil, fmt.Errorf("grant manual Pro: %w", err)
	}
	return &grant, nil
}

func insertAudit(
	ctx context.Context,
	tx pgx.Tx,
	userID, actor, action, reason string,
	before, after AccessSnapshot,
) error {
	beforeJSON, err := json.Marshal(before)
	if err != nil {
		return fmt.Errorf("encode audit before state: %w", err)
	}
	afterJSON, err := json.Marshal(after)
	if err != nil {
		return fmt.Errorf("encode audit after state: %w", err)
	}
	const query = `
		insert into account_admin_audit_events (
			user_id, actor, action, reason, source, before_state, after_state
		)
		values ($1::uuid, $2, $3, $4, 'account-admin', $5::jsonb, $6::jsonb)
	`
	if _, err := tx.Exec(ctx, query, userID, actor, action, reason, beforeJSON, afterJSON); err != nil {
		return fmt.Errorf("insert account admin audit event: %w", err)
	}
	return nil
}

func assertEntitlement(ctx context.Context, tx pgx.Tx, plan, key, expected string) error {
	const query = `
		select entitlement_value::text
		from plan_entitlements
		where plan_code = $1 and entitlement_key = $2
	`
	var value string
	if err := tx.QueryRow(ctx, query, plan, key).Scan(&value); err != nil {
		return fmt.Errorf("resolve %s entitlement %s: %w", plan, key, err)
	}
	if value != expected {
		return fmt.Errorf("%s entitlement %s=%s, want %s", plan, key, value, expected)
	}
	return nil
}
