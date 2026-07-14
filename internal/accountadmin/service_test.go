package accountadmin

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryRepository struct {
	user        User
	effective   AccessSnapshot
	manual      *ManualGrant
	auditEvents int
	now         time.Time
}

func newMemoryRepository() *memoryRepository {
	return &memoryRepository{
		user: User{
			ID:          "11111111-1111-4111-8111-111111111111",
			AuthSubject: "auth0|qa-user",
		},
		effective: AccessSnapshot{Plan: "free", Status: "none"},
		now:       time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC),
	}
}

func (r *memoryRepository) GrantPro(_ context.Context, request GrantRequest) (OperationResult, error) {
	before := r.effective
	result := OperationResult{Action: "grant_pro", User: r.user, Before: before, After: before, DryRun: request.DryRun, ManualGrant: r.manual}
	if activeManualGrant(r.manual, r.now) {
		return result, nil
	}
	until := r.now.Add(request.Duration)
	grant := &ManualGrant{SubscriptionID: manualSubscriptionID(r.user.ID), Status: "active", AccessUntil: &until, UpdatedAt: r.now}
	result.Changed = true
	result.After = AccessSnapshot{Plan: "pro", Status: "active", AccessUntil: &until}
	result.ManualGrant = grant
	if !request.DryRun {
		r.manual = grant
		r.effective = result.After
		r.auditEvents++
	}
	return result, nil
}

func (r *memoryRepository) RevokePro(_ context.Context, request RevokeRequest) (OperationResult, error) {
	before := r.effective
	result := OperationResult{Action: "revoke_pro", User: r.user, Before: before, After: before, DryRun: request.DryRun, ManualGrant: r.manual}
	if !activeManualGrant(r.manual, r.now) {
		return result, nil
	}
	result.Changed = true
	result.After = AccessSnapshot{Plan: "free", Status: "none"}
	if !request.DryRun {
		revoked := *r.manual
		revoked.Status = "expired"
		revoked.AccessUntil = &r.now
		r.manual = &revoked
		r.effective = result.After
		r.auditEvents++
		result.ManualGrant = &revoked
	}
	return result, nil
}

func (r *memoryRepository) Status(_ context.Context, selector Selector) (StatusResult, error) {
	if selector.AuthSubject != r.user.AuthSubject && selector.UserID != r.user.ID {
		return StatusResult{}, ErrUserNotFound
	}
	return StatusResult{User: r.user, Effective: r.effective, ManualGrant: r.manual}, nil
}

func (r *memoryRepository) ListActiveManualGrants(_ context.Context, _ int) ([]StatusResult, error) {
	if !activeManualGrant(r.manual, r.now) {
		return []StatusResult{}, nil
	}
	return []StatusResult{{User: r.user, Effective: r.effective, ManualGrant: r.manual}}, nil
}

func (r *memoryRepository) VerifyLifecycle(_ context.Context, _ VerifyRequest) (LifecycleVerification, error) {
	return LifecycleVerification{
		FreeBefore:  AccessSnapshot{Plan: "free", Status: "none"},
		ProGranted:  AccessSnapshot{Plan: "pro", Status: "active"},
		FreeAfter:   AccessSnapshot{Plan: "free", Status: "none"},
		AuditEvents: 2,
		RolledBack:  true,
	}, nil
}

func TestManualProLifecycleIsIdempotent(t *testing.T) {
	t.Parallel()
	repository := newMemoryRepository()
	service, err := NewService(repository, "development", 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	selector := Selector{AuthSubject: repository.user.AuthSubject}
	grant := GrantRequest{Selector: selector, Duration: 30 * 24 * time.Hour, Actor: "Ignacio", Reason: "Private beta"}

	firstGrant, err := service.GrantPro(context.Background(), grant)
	if err != nil {
		t.Fatal(err)
	}
	if !firstGrant.Changed || firstGrant.After.Plan != "pro" || repository.auditEvents != 1 {
		t.Fatalf("first grant = %#v audit=%d", firstGrant, repository.auditEvents)
	}
	secondGrant, err := service.GrantPro(context.Background(), grant)
	if err != nil {
		t.Fatal(err)
	}
	if secondGrant.Changed || repository.auditEvents != 1 {
		t.Fatalf("repeated grant = %#v audit=%d", secondGrant, repository.auditEvents)
	}

	revoke := RevokeRequest{Selector: selector, Actor: "Ignacio", Reason: "Private beta ended"}
	firstRevoke, err := service.RevokePro(context.Background(), revoke)
	if err != nil {
		t.Fatal(err)
	}
	if !firstRevoke.Changed || firstRevoke.After.Plan != "free" || repository.auditEvents != 2 {
		t.Fatalf("first revoke = %#v audit=%d", firstRevoke, repository.auditEvents)
	}
	secondRevoke, err := service.RevokePro(context.Background(), revoke)
	if err != nil {
		t.Fatal(err)
	}
	if secondRevoke.Changed || repository.auditEvents != 2 {
		t.Fatalf("repeated revoke = %#v audit=%d", secondRevoke, repository.auditEvents)
	}
}

func TestProductionMutationsRequireExplicitConfirmation(t *testing.T) {
	t.Parallel()
	repository := newMemoryRepository()
	service, err := NewService(repository, "production", 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request := GrantRequest{
		Selector: Selector{AuthSubject: repository.user.AuthSubject},
		Duration: 24 * time.Hour,
		Actor:    "Ignacio",
		Reason:   "Private beta",
	}
	if _, err := service.GrantPro(context.Background(), request); err == nil {
		t.Fatal("production grant succeeded without confirmation")
	}
	request.ProductionConfirmation = ProductionConfirmation
	if _, err := service.GrantPro(context.Background(), request); err != nil {
		t.Fatalf("confirmed production grant failed: %v", err)
	}
}

func TestDryRunDoesNotRequireProductionConfirmationOrWrite(t *testing.T) {
	t.Parallel()
	repository := newMemoryRepository()
	service, err := NewService(repository, "production", 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.GrantPro(context.Background(), GrantRequest{
		Selector: Selector{AuthSubject: repository.user.AuthSubject},
		Duration: 24 * time.Hour,
		Actor:    "Ignacio",
		Reason:   "Preview beta grant",
		DryRun:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || repository.effective.Plan != "free" || repository.auditEvents != 0 {
		t.Fatalf("dry-run result=%#v effective=%#v audit=%d", result, repository.effective, repository.auditEvents)
	}
}

func TestSelectorAndDurationValidation(t *testing.T) {
	t.Parallel()
	for _, selector := range []Selector{
		{},
		{Email: "a@example.com", AuthSubject: "auth0|a"},
		{UserID: "not-a-uuid"},
		{Email: "invalid"},
	} {
		if err := selector.Validate(); err == nil {
			t.Fatalf("selector unexpectedly valid: %#v", selector)
		}
	}
	if err := (Selector{UserID: "11111111-1111-4111-8111-111111111111"}).Validate(); err != nil {
		t.Fatal(err)
	}
	if duration, err := ParseDuration("30d"); err != nil || duration != 30*24*time.Hour {
		t.Fatalf("duration=%s err=%v", duration, err)
	}
	if _, err := ParseDuration("0d"); err == nil {
		t.Fatal("zero duration was accepted")
	}
}

func TestStatusReturnsUserNotFound(t *testing.T) {
	t.Parallel()
	repository := newMemoryRepository()
	service, err := NewService(repository, "development", 365*24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Status(context.Background(), Selector{AuthSubject: "auth0|missing"})
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("status error=%v, want ErrUserNotFound", err)
	}
}
