package adminpanel

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/accountadmin"
	"github.com/nachodev-ui/albion-market-api/internal/authn"
)

var (
	ErrForbidden      = errors.New("administrator access required")
	ErrInvalidRequest = errors.New("invalid administrator request")
)

const (
	GrantConfirmation  = "GRANT PRO"
	RevokeConfirmation = "REVOKE PRO"
)

type Session struct {
	AdminID     string     `json:"adminId"`
	UserID      string     `json:"userId"`
	AuthSubject string     `json:"authSubject"`
	Email       *string    `json:"email,omitempty"`
	DisplayName *string    `json:"displayName,omitempty"`
	Active      bool       `json:"active"`
	CreatedAt   time.Time  `json:"createdAt"`
	DisabledAt  *time.Time `json:"disabledAt,omitempty"`
}

type UserSummary struct {
	User            accountadmin.User           `json:"user"`
	Effective       accountadmin.AccessSnapshot `json:"effective"`
	ManualGrant     *accountadmin.ManualGrant   `json:"manualGrant,omitempty"`
	ActiveProviders []string                    `json:"activeProviders"`
}

type Subscription struct {
	ID                     string     `json:"id"`
	Provider               string     `json:"provider"`
	ProviderSubscriptionID *string    `json:"providerSubscriptionId,omitempty"`
	Plan                   string     `json:"plan"`
	Status                 string     `json:"status"`
	CurrentPeriodStart     *time.Time `json:"currentPeriodStart,omitempty"`
	CurrentPeriodEnd       *time.Time `json:"currentPeriodEnd,omitempty"`
	AccessUntil            *time.Time `json:"accessUntil,omitempty"`
	CancelAtPeriodEnd      bool       `json:"cancelAtPeriodEnd"`
	CreatedAt              time.Time  `json:"createdAt"`
	UpdatedAt              time.Time  `json:"updatedAt"`
}

type UserDetail struct {
	UserSummary
	Subscriptions []Subscription `json:"subscriptions"`
	Entitlements  map[string]any `json:"entitlements"`
}

type AuditEvent struct {
	ID        string            `json:"id"`
	User      accountadmin.User `json:"user"`
	Actor     string            `json:"actor"`
	Action    string            `json:"action"`
	Reason    string            `json:"reason"`
	Before    map[string]any    `json:"before"`
	After     map[string]any    `json:"after"`
	CreatedAt time.Time         `json:"createdAt"`
}

type Repository interface {
	AdminSession(context.Context, string) (Session, error)
	SearchUsers(context.Context, string, int) ([]UserSummary, error)
	UserDetail(context.Context, string) (UserDetail, error)
	AuditEvents(context.Context, int) ([]AuditEvent, error)
}

type AccountAdministrator interface {
	GrantPro(context.Context, accountadmin.GrantRequest) (accountadmin.OperationResult, error)
	RevokePro(context.Context, accountadmin.RevokeRequest) (accountadmin.OperationResult, error)
}

type Service struct {
	repository Repository
	accounts   AccountAdministrator
}

func NewService(repository Repository, accounts AccountAdministrator) (*Service, error) {
	if repository == nil {
		return nil, errors.New("admin panel repository is required")
	}
	if accounts == nil {
		return nil, errors.New("account administrator service is required")
	}
	return &Service{repository: repository, accounts: accounts}, nil
}

func (s *Service) Session(ctx context.Context, identity authn.Identity) (Session, error) {
	return s.authorize(ctx, identity)
}

func (s *Service) SearchUsers(
	ctx context.Context,
	identity authn.Identity,
	query string,
	limit int,
) ([]UserSummary, error) {
	if _, err := s.authorize(ctx, identity); err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if len(query) > 200 {
		return nil, fmt.Errorf("%w: search query is too long", ErrInvalidRequest)
	}
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("%w: limit must be between 1 and 100", ErrInvalidRequest)
	}
	return s.repository.SearchUsers(ctx, query, limit)
}

func (s *Service) UserDetail(
	ctx context.Context,
	identity authn.Identity,
	userID string,
) (UserDetail, error) {
	if _, err := s.authorize(ctx, identity); err != nil {
		return UserDetail{}, err
	}
	selector := accountadmin.Selector{UserID: strings.TrimSpace(userID)}
	if err := selector.Validate(); err != nil {
		return UserDetail{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	return s.repository.UserDetail(ctx, selector.UserID)
}

func (s *Service) GrantPro(
	ctx context.Context,
	identity authn.Identity,
	userID string,
	durationDays int,
	reason string,
	confirmation string,
) (accountadmin.OperationResult, error) {
	if _, err := s.authorize(ctx, identity); err != nil {
		return accountadmin.OperationResult{}, err
	}
	if strings.TrimSpace(confirmation) != GrantConfirmation {
		return accountadmin.OperationResult{}, fmt.Errorf("%w: confirmation must equal %q", ErrInvalidRequest, GrantConfirmation)
	}
	if durationDays < 1 || durationDays > 365 {
		return accountadmin.OperationResult{}, fmt.Errorf("%w: durationDays must be between 1 and 365", ErrInvalidRequest)
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 3 || len(reason) > 500 || strings.ContainsAny(reason, "\r\n") {
		return accountadmin.OperationResult{}, fmt.Errorf("%w: reason must contain 3-500 characters without line breaks", ErrInvalidRequest)
	}
	selector := accountadmin.Selector{UserID: strings.TrimSpace(userID)}
	if err := selector.Validate(); err != nil {
		return accountadmin.OperationResult{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	return s.accounts.GrantPro(ctx, accountadmin.GrantRequest{
		Selector:               selector,
		Duration:               time.Duration(durationDays) * 24 * time.Hour,
		Actor:                  identity.Subject,
		Reason:                 reason,
		ProductionConfirmation: accountadmin.ProductionConfirmation,
	})
}

func (s *Service) RevokePro(
	ctx context.Context,
	identity authn.Identity,
	userID string,
	reason string,
	confirmation string,
) (accountadmin.OperationResult, error) {
	if _, err := s.authorize(ctx, identity); err != nil {
		return accountadmin.OperationResult{}, err
	}
	if strings.TrimSpace(confirmation) != RevokeConfirmation {
		return accountadmin.OperationResult{}, fmt.Errorf("%w: confirmation must equal %q", ErrInvalidRequest, RevokeConfirmation)
	}
	reason = strings.TrimSpace(reason)
	if len(reason) < 3 || len(reason) > 500 || strings.ContainsAny(reason, "\r\n") {
		return accountadmin.OperationResult{}, fmt.Errorf("%w: reason must contain 3-500 characters without line breaks", ErrInvalidRequest)
	}
	selector := accountadmin.Selector{UserID: strings.TrimSpace(userID)}
	if err := selector.Validate(); err != nil {
		return accountadmin.OperationResult{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	return s.accounts.RevokePro(ctx, accountadmin.RevokeRequest{
		Selector:               selector,
		Actor:                  identity.Subject,
		Reason:                 reason,
		ProductionConfirmation: accountadmin.ProductionConfirmation,
	})
}

func (s *Service) AuditEvents(
	ctx context.Context,
	identity authn.Identity,
	limit int,
) ([]AuditEvent, error) {
	if _, err := s.authorize(ctx, identity); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 500 {
		return nil, fmt.Errorf("%w: limit must be between 1 and 500", ErrInvalidRequest)
	}
	return s.repository.AuditEvents(ctx, limit)
}

func (s *Service) authorize(ctx context.Context, identity authn.Identity) (Session, error) {
	subject := strings.TrimSpace(identity.Subject)
	if subject == "" {
		return Session{}, ErrForbidden
	}
	return s.repository.AdminSession(ctx, subject)
}
