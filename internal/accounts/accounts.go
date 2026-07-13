package accounts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/authn"
)

type User struct {
	ID          string     `json:"id"`
	Email       *string    `json:"email"`
	DisplayName *string    `json:"displayName"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LastLoginAt *time.Time `json:"lastLoginAt"`
}

type Subscription struct {
	Plan        string     `json:"plan"`
	Status      string     `json:"status"`
	AccessUntil *time.Time `json:"accessUntil"`
}

type Access struct {
	User         User           `json:"user"`
	Subscription Subscription   `json:"subscription"`
	Entitlements map[string]any `json:"entitlements"`
}

type Service struct {
	db *pgxpool.Pool
}

func NewService(db *pgxpool.Pool) *Service {
	return &Service{db: db}
}

func (s *Service) Current(ctx context.Context, identity authn.Identity) (Access, error) {
	if s == nil || s.db == nil || strings.TrimSpace(identity.Subject) == "" {
		return Access{}, errors.New("account service is not configured")
	}

	const syncUser = `
		insert into app_users (auth_subject, email, display_name, last_login_at)
		values ($1, nullif($2, ''), nullif($3, ''), now())
		on conflict (auth_subject)
		do update set
			email = coalesce(nullif(excluded.email, ''), app_users.email),
			display_name = coalesce(nullif(excluded.display_name, ''), app_users.display_name),
			updated_at = now(),
			last_login_at = now()
		returning id::text, email, display_name, created_at, updated_at, last_login_at
	`
	var user User
	if err := s.db.QueryRow(
		ctx,
		syncUser,
		identity.Subject,
		identity.Email,
		identity.DisplayName,
	).Scan(
		&user.ID,
		&user.Email,
		&user.DisplayName,
		&user.CreatedAt,
		&user.UpdatedAt,
		&user.LastLoginAt,
	); err != nil {
		return Access{}, fmt.Errorf("sync user: %w", err)
	}

	subscription := Subscription{Plan: "free", Status: "none"}
	const activeSubscription = `
		select plan_code, status, access_until
		from subscriptions
		where user_id = $1::uuid
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
	err := s.db.QueryRow(ctx, activeSubscription, user.ID).Scan(
		&subscription.Plan,
		&subscription.Status,
		&subscription.AccessUntil,
	)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Access{}, fmt.Errorf("resolve subscription: %w", err)
	}

	const permissions = `
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
	rows, err := s.db.Query(ctx, permissions, subscription.Plan, user.ID)
	if err != nil {
		return Access{}, fmt.Errorf("resolve entitlements: %w", err)
	}
	defer rows.Close()

	entitlements := make(map[string]any)
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return Access{}, err
		}
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return Access{}, err
		}
		entitlements[key] = value
	}
	if err := rows.Err(); err != nil {
		return Access{}, err
	}

	return Access{
		User:         user,
		Subscription: subscription,
		Entitlements: entitlements,
	}, nil
}

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) Me(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, false)
}

func (h *Handler) Entitlements(w http.ResponseWriter, r *http.Request) {
	h.respond(w, r, true)
}

func (h *Handler) respond(w http.ResponseWriter, r *http.Request, entitlementsOnly bool) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	identity, ok := authn.IdentityFromContext(r.Context())
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	access, err := h.service.Current(r.Context(), identity)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	if entitlementsOnly {
		writeJSON(w, http.StatusOK, map[string]any{
			"subscription": access.Subscription,
			"entitlements": access.Entitlements,
		})
		return
	}
	writeJSON(w, http.StatusOK, access)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type Requirement func(any) bool

func BoolEnabled(value any) bool {
	enabled, ok := value.(bool)
	return ok && enabled
}

func NumberAtLeast(minimum float64) Requirement {
	return func(value any) bool {
		number, ok := value.(float64)
		return ok && number >= minimum
	}
}

func RequireEntitlement(
	service *Service,
	key string,
	requirement Requirement,
	next http.Handler,
) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := authn.IdentityFromContext(r.Context())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		access, err := service.Current(r.Context(), identity)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
			return
		}
		value, exists := access.Entitlements[key]
		if !exists || requirement == nil || !requirement(value) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "entitlement required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
