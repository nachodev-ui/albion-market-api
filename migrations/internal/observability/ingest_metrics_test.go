package observability

import (
	"sync"
	"testing"
	"time"
)

func TestIngestMetricsConcurrentUpdates(t *testing.T) {
	t.Parallel()

	metrics := NewIngestMetrics()
	startedAt := time.Date(2026, time.June, 25, 20, 0, 0, 0, time.UTC)

	const workers = 100
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func(index int) {
			defer wg.Done()
			metrics.RequestStarted(startedAt.Add(time.Duration(index) * time.Millisecond))
			metrics.RequestFinished(IngestObservation{
				CompletedAt:        startedAt.Add(time.Second),
				Duration:           time.Duration(index+1) * time.Millisecond,
				StatusCode:         202,
				Accepted:           500,
				CurrentRowsTouched: 500,
			})
		}(i)
	}
	wg.Wait()

	snapshot := metrics.Snapshot()
	if snapshot.RequestsTotal != workers {
		t.Fatalf("RequestsTotal = %d, want %d", snapshot.RequestsTotal, workers)
	}
	if snapshot.InFlight != 0 {
		t.Fatalf("InFlight = %d, want 0", snapshot.InFlight)
	}
	if snapshot.SucceededTotal != workers {
		t.Fatalf("SucceededTotal = %d, want %d", snapshot.SucceededTotal, workers)
	}
	if snapshot.ErrorsTotal != 0 {
		t.Fatalf("ErrorsTotal = %d, want 0", snapshot.ErrorsTotal)
	}
	if snapshot.AcceptedEntriesTotal != workers*500 {
		t.Fatalf("AcceptedEntriesTotal = %d, want %d", snapshot.AcceptedEntriesTotal, workers*500)
	}
	if snapshot.CurrentRowsTouchedTotal != workers*500 {
		t.Fatalf("CurrentRowsTouchedTotal = %d, want %d", snapshot.CurrentRowsTouchedTotal, workers*500)
	}
	if snapshot.MaxDuration != 100*time.Millisecond {
		t.Fatalf("MaxDuration = %s, want 100ms", snapshot.MaxDuration)
	}
}

func TestIngestMetricsDoesNotCountDuplicateEntriesTwice(t *testing.T) {
	t.Parallel()

	metrics := NewIngestMetrics()
	now := time.Now()
	metrics.RequestStarted(now)
	metrics.RequestFinished(IngestObservation{
		CompletedAt: now,
		Duration:    time.Millisecond,
		StatusCode:  200,
		Accepted:    500,
		Duplicate:   true,
	})

	snapshot := metrics.Snapshot()
	if snapshot.DuplicatesTotal != 1 {
		t.Fatalf("DuplicatesTotal = %d, want 1", snapshot.DuplicatesTotal)
	}
	if snapshot.AcceptedEntriesTotal != 0 {
		t.Fatalf("AcceptedEntriesTotal = %d, want 0", snapshot.AcceptedEntriesTotal)
	}
}
