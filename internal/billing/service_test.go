package billing

import (
	"testing"
	"time"
)

func TestResolveAccessPreservesCanceledPeriod(t *testing.T) {
	t.Parallel()
	service := &Service{pastDueGracePeriod: 7 * 24 * time.Hour}
	updatedAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	attributes := lemonSubscriptionAttributes{
		Status:   "cancelled",
		RenewsAt: "2026-08-13T12:00:00Z",
		EndsAt:   "2026-08-13T12:00:00Z",
	}
	status, accessUntil, err := service.resolveAccess(attributes, updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	if status != "canceled" || accessUntil == nil || !accessUntil.Equal(time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("status=%q accessUntil=%v", status, accessUntil)
	}
}

func TestResolveAccessAddsPastDueGracePeriod(t *testing.T) {
	t.Parallel()
	service := &Service{pastDueGracePeriod: 7 * 24 * time.Hour}
	updatedAt := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	status, accessUntil, err := service.resolveAccess(lemonSubscriptionAttributes{
		Status:   "past_due",
		RenewsAt: "2026-07-13T12:00:00Z",
	}, updatedAt)
	if err != nil {
		t.Fatal(err)
	}
	want := updatedAt.Add(7 * 24 * time.Hour)
	if status != "past_due" || accessUntil == nil || !accessUntil.Equal(want) {
		t.Fatalf("status=%q accessUntil=%v want=%v", status, accessUntil, want)
	}
}

func TestResolveAccessRejectsUnknownStatus(t *testing.T) {
	t.Parallel()
	service := &Service{pastDueGracePeriod: 7 * 24 * time.Hour}
	_, _, err := service.resolveAccess(lemonSubscriptionAttributes{Status: "unknown"}, time.Now())
	if err == nil {
		t.Fatal("unknown provider status was accepted")
	}
}
