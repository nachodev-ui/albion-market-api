package accountadmin

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const ProductionConfirmation = "PRODUCTION"

var (
	ErrUserNotFound  = errors.New("account user not found")
	ErrAmbiguousUser = errors.New("account user selector matched multiple users")
	uuidPattern      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-5][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)
	wholeDaysPattern = regexp.MustCompile(`^([1-9][0-9]*)d$`)
)

type Selector struct {
	UserID      string `json:"userId,omitempty"`
	Email       string `json:"email,omitempty"`
	AuthSubject string `json:"authSubject,omitempty"`
}

func (s Selector) Normalize() Selector {
	return Selector{
		UserID:      strings.TrimSpace(s.UserID),
		Email:       strings.ToLower(strings.TrimSpace(s.Email)),
		AuthSubject: strings.TrimSpace(s.AuthSubject),
	}
}

func (s Selector) Validate() error {
	s = s.Normalize()
	count := 0
	for _, value := range []string{s.UserID, s.Email, s.AuthSubject} {
		if value != "" {
			count++
		}
	}
	if count != 1 {
		return errors.New("exactly one of --user-id, --email or --subject is required")
	}
	if s.UserID != "" && !uuidPattern.MatchString(s.UserID) {
		return errors.New("--user-id must be a valid UUID")
	}
	if s.Email != "" {
		if len(s.Email) > 320 || !strings.Contains(s.Email, "@") {
			return errors.New("--email must be a valid email address")
		}
	}
	if s.AuthSubject != "" && (len(s.AuthSubject) > 512 || containsLineBreak(s.AuthSubject)) {
		return errors.New("--subject must contain 1-512 characters without line breaks")
	}
	return nil
}

type User struct {
	ID          string  `json:"id"`
	AuthSubject string  `json:"authSubject"`
	Email       *string `json:"email,omitempty"`
	DisplayName *string `json:"displayName,omitempty"`
}

type AccessSnapshot struct {
	Plan        string     `json:"plan"`
	Status      string     `json:"status"`
	AccessUntil *time.Time `json:"accessUntil,omitempty"`
}

type ManualGrant struct {
	SubscriptionID string     `json:"subscriptionId"`
	Status         string     `json:"status"`
	AccessUntil    *time.Time `json:"accessUntil,omitempty"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type OperationResult struct {
	Action      string         `json:"action"`
	Changed     bool           `json:"changed"`
	DryRun      bool           `json:"dryRun"`
	User        User           `json:"user"`
	Before      AccessSnapshot `json:"before"`
	After       AccessSnapshot `json:"after"`
	ManualGrant *ManualGrant   `json:"manualGrant,omitempty"`
}

type StatusResult struct {
	User        User           `json:"user"`
	Effective   AccessSnapshot `json:"effective"`
	ManualGrant *ManualGrant   `json:"manualGrant,omitempty"`
}

type LifecycleVerification struct {
	FreeBefore  AccessSnapshot `json:"freeBefore"`
	ProGranted  AccessSnapshot `json:"proGranted"`
	FreeAfter   AccessSnapshot `json:"freeAfter"`
	AuditEvents int            `json:"auditEvents"`
	RolledBack  bool           `json:"rolledBack"`
}

type GrantRequest struct {
	Selector               Selector
	Duration               time.Duration
	Actor                  string
	Reason                 string
	DryRun                 bool
	ProductionConfirmation string
}

type RevokeRequest struct {
	Selector               Selector
	Actor                  string
	Reason                 string
	DryRun                 bool
	ProductionConfirmation string
}

type VerifyRequest struct {
	Actor                  string
	Reason                 string
	ProductionConfirmation string
}

type Repository interface {
	GrantPro(context.Context, GrantRequest) (OperationResult, error)
	RevokePro(context.Context, RevokeRequest) (OperationResult, error)
	Status(context.Context, Selector) (StatusResult, error)
	ListActiveManualGrants(context.Context, int) ([]StatusResult, error)
	VerifyLifecycle(context.Context, VerifyRequest) (LifecycleVerification, error)
}

type Service struct {
	repository  Repository
	environment string
	maxDuration time.Duration
}

func NewService(repository Repository, environment string, maxDuration time.Duration) (*Service, error) {
	if repository == nil {
		return nil, errors.New("account admin repository is required")
	}
	environment = strings.ToLower(strings.TrimSpace(environment))
	if environment == "" {
		environment = "development"
	}
	if maxDuration <= 0 {
		return nil, errors.New("maximum grant duration must be greater than zero")
	}
	return &Service{repository: repository, environment: environment, maxDuration: maxDuration}, nil
}

func (s *Service) GrantPro(ctx context.Context, request GrantRequest) (OperationResult, error) {
	request.Selector = request.Selector.Normalize()
	request.Actor = strings.TrimSpace(request.Actor)
	request.Reason = strings.TrimSpace(request.Reason)
	if err := request.Selector.Validate(); err != nil {
		return OperationResult{}, err
	}
	if err := validateActorAndReason(request.Actor, request.Reason); err != nil {
		return OperationResult{}, err
	}
	if request.Duration <= 0 {
		return OperationResult{}, errors.New("grant duration must be greater than zero")
	}
	if request.Duration > s.maxDuration {
		return OperationResult{}, fmt.Errorf("grant duration exceeds maximum of %s", FormatDuration(s.maxDuration))
	}
	if err := s.requireProductionConfirmation(request.DryRun, request.ProductionConfirmation); err != nil {
		return OperationResult{}, err
	}
	return s.repository.GrantPro(ctx, request)
}

func (s *Service) RevokePro(ctx context.Context, request RevokeRequest) (OperationResult, error) {
	request.Selector = request.Selector.Normalize()
	request.Actor = strings.TrimSpace(request.Actor)
	request.Reason = strings.TrimSpace(request.Reason)
	if err := request.Selector.Validate(); err != nil {
		return OperationResult{}, err
	}
	if err := validateActorAndReason(request.Actor, request.Reason); err != nil {
		return OperationResult{}, err
	}
	if err := s.requireProductionConfirmation(request.DryRun, request.ProductionConfirmation); err != nil {
		return OperationResult{}, err
	}
	return s.repository.RevokePro(ctx, request)
}

func (s *Service) Status(ctx context.Context, selector Selector) (StatusResult, error) {
	selector = selector.Normalize()
	if err := selector.Validate(); err != nil {
		return StatusResult{}, err
	}
	return s.repository.Status(ctx, selector)
}

func (s *Service) ListActiveManualGrants(ctx context.Context, limit int) ([]StatusResult, error) {
	if limit < 1 || limit > 1000 {
		return nil, errors.New("limit must be between 1 and 1000")
	}
	return s.repository.ListActiveManualGrants(ctx, limit)
}

func (s *Service) VerifyLifecycle(ctx context.Context, request VerifyRequest) (LifecycleVerification, error) {
	request.Actor = strings.TrimSpace(request.Actor)
	request.Reason = strings.TrimSpace(request.Reason)
	if err := validateActorAndReason(request.Actor, request.Reason); err != nil {
		return LifecycleVerification{}, err
	}
	if err := s.requireProductionConfirmation(false, request.ProductionConfirmation); err != nil {
		return LifecycleVerification{}, err
	}
	return s.repository.VerifyLifecycle(ctx, request)
}

func (s *Service) requireProductionConfirmation(dryRun bool, confirmation string) error {
	if s.environment != "production" || dryRun {
		return nil
	}
	if strings.TrimSpace(confirmation) != ProductionConfirmation {
		return fmt.Errorf("production mutation requires --confirm-production %s", ProductionConfirmation)
	}
	return nil
}

func validateActorAndReason(actor, reason string) error {
	if len(actor) < 3 || len(actor) > 200 || containsLineBreak(actor) {
		return errors.New("actor must contain 3-200 characters without line breaks")
	}
	if len(reason) < 3 || len(reason) > 500 || containsLineBreak(reason) {
		return errors.New("reason must contain 3-500 characters without line breaks")
	}
	return nil
}

func ParseDuration(value string) (time.Duration, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if matches := wholeDaysPattern.FindStringSubmatch(value); matches != nil {
		days, err := strconv.ParseInt(matches[1], 10, 64)
		if err != nil || days > 3650 {
			return 0, errors.New("duration is outside the supported range")
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, errors.New("duration must be a positive Go duration or whole days such as 30d")
	}
	return duration, nil
}

func FormatDuration(duration time.Duration) string {
	if duration > 0 && duration%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int64(duration/(24*time.Hour)))
	}
	return duration.String()
}

func activeManualGrant(grant *ManualGrant, now time.Time) bool {
	if grant == nil || (grant.Status != "active" && grant.Status != "trialing") {
		return false
	}
	return grant.AccessUntil == nil || grant.AccessUntil.After(now)
}

func manualSubscriptionID(userID string) string {
	return "manual:" + userID
}

func containsLineBreak(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}
