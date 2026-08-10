package observability

import (
	"context"
	"testing"
	"time"
)

func TestPgxReadinessCheckerRejectsMissingPool(t *testing.T) {
	t.Parallel()

	readinessMetrics := NewReadinessMetrics()
	checker := NewPgxReadinessChecker(nil, NewDatabaseMetrics(), readinessMetrics)
	snapshot := checker.Check(context.Background())

	if snapshot.Ready {
		t.Fatal("readiness unexpectedly succeeded without a pool")
	}
	if snapshot.FailedComponent != ReadinessComponentPool {
		t.Fatalf("failed component = %q, want %q", snapshot.FailedComponent, ReadinessComponentPool)
	}
	if snapshot.Err == nil {
		t.Fatalf("snapshot = %#v, want an error", snapshot)
	}
	if snapshot.CheckedAt.IsZero() {
		t.Fatalf("snapshot = %#v, want a recorded check time", snapshot)
	}
	if snapshot.Duration < 0 {
		t.Fatalf("snapshot duration = %s, want a non-negative duration", snapshot.Duration)
	}

	metricsSnapshot := readinessMetrics.Snapshot()
	if metricsSnapshot.Ready || metricsSnapshot.Checks["error"] != 1 ||
		metricsSnapshot.Failures[ReadinessComponentPool] != 1 {
		t.Fatalf("readiness metrics = %#v", metricsSnapshot)
	}
}

func TestReadinessMetricsBoundsFailureComponents(t *testing.T) {
	t.Parallel()

	metrics := NewReadinessMetrics()
	metrics.Observe(ReadinessSnapshot{
		FailedComponent: "attacker-controlled-component",
		Duration:        time.Millisecond,
		CheckedAt:       time.Now(),
	})

	snapshot := metrics.Snapshot()
	if snapshot.Failures["unknown"] != 1 {
		t.Fatalf("failure labels = %#v, want unknown=1", snapshot.Failures)
	}
	if _, exists := snapshot.Failures["attacker-controlled-component"]; exists {
		t.Fatalf("unbounded readiness component leaked into metrics: %#v", snapshot.Failures)
	}
}

func TestRequiredReadinessRelationsContainBillingQueueSchema(t *testing.T) {
	t.Parallel()

	required := map[string]bool{
		"public.app_schema_state":            false,
		"public.account_admin_audit_events":  false,
		"public.app_admins":                  false,
		"public.player_economic_profiles":    false,
		"public.saved_presets":               false,
		"public.saved_calculations":          false,
		"public.billing_webhook_events":      false,
		"public.billing_orders":              false,
		"public.billing_notification_outbox": false,
		"public.albion_pvp_events":           false,
		"public.albion_pvp_ingest_state":     false,
	}
	for _, relation := range requiredReadinessRelations {
		if _, exists := required[relation]; exists {
			required[relation] = true
		}
	}
	for relation, found := range required {
		if !found {
			t.Fatalf("readiness relations do not include %s", relation)
		}
	}
	if ExpectedSchemaVersion != 19 {
		t.Fatalf("ExpectedSchemaVersion = %d, want 19", ExpectedSchemaVersion)
	}
}
