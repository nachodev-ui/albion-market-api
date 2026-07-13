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

func TestRequiredReadinessRelationsContainSchemaMarker(t *testing.T) {
	t.Parallel()

	found := false
	for _, relation := range requiredReadinessRelations {
		if relation == "public.app_schema_state" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("readiness relations do not include public.app_schema_state")
	}
	if ExpectedSchemaVersion != 9 {
		t.Fatalf("ExpectedSchemaVersion = %d, want 9", ExpectedSchemaVersion)
	}
}
