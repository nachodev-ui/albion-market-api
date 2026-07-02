package observability

import (
	"net/http"
	"sync"
	"testing"
	"time"
)

func TestHistoryIngestMetricsTracksSuccessDuplicateAndError(t *testing.T) {
	t.Parallel()

	metrics := NewHistoryIngestMetrics()
	base := time.Date(2026, time.June, 26, 12, 0, 0, 0, time.UTC)

	metrics.RequestStarted(base)
	metrics.RequestFinished(HistoryIngestObservation{
		CompletedAt:        base.Add(time.Second),
		Duration:           100 * time.Millisecond,
		StatusCode:         http.StatusAccepted,
		AcceptedEntries:    2,
		AcceptedBuckets:    50,
		HistoryRowsTouched: 40,
	})
	metrics.RequestStarted(base.Add(2 * time.Second))
	metrics.RequestFinished(HistoryIngestObservation{
		CompletedAt:     base.Add(3 * time.Second),
		Duration:        50 * time.Millisecond,
		StatusCode:      http.StatusOK,
		AcceptedEntries: 2,
		AcceptedBuckets: 50,
		Duplicate:       true,
	})
	metrics.RequestStarted(base.Add(4 * time.Second))
	metrics.RequestFinished(HistoryIngestObservation{
		CompletedAt: base.Add(5 * time.Second),
		Duration:    150 * time.Millisecond,
		StatusCode:  http.StatusInternalServerError,
		ErrorKind:   "database_error",
	})

	snapshot := metrics.Snapshot()
	if snapshot.RequestsTotal != 3 || snapshot.InFlight != 0 || snapshot.SucceededTotal != 2 ||
		snapshot.DuplicatesTotal != 1 || snapshot.ErrorsTotal != 1 {
		t.Fatalf("request metrics = %#v", snapshot)
	}
	if snapshot.AcceptedEntriesTotal != 2 || snapshot.AcceptedBucketsTotal != 50 ||
		snapshot.HistoryRowsTouchedTotal != 40 {
		t.Fatalf("accepted metrics = %#v", snapshot)
	}
	if snapshot.AverageDuration != 100*time.Millisecond || snapshot.MaxDuration != 150*time.Millisecond ||
		snapshot.LastDuration != 150*time.Millisecond {
		t.Fatalf("duration metrics = %#v", snapshot)
	}
	if snapshot.LastErrorKind != "database_error" || snapshot.LastSuccessAt == nil || snapshot.LastErrorAt == nil {
		t.Fatalf("last outcome metrics = %#v", snapshot)
	}
}

func TestHistoryIngestMetricsIsConcurrencySafe(t *testing.T) {
	t.Parallel()

	metrics := NewHistoryIngestMetrics()
	const workers = 100
	var group sync.WaitGroup
	group.Add(workers)
	for index := 0; index < workers; index++ {
		go func(offset int) {
			defer group.Done()
			at := time.Date(2026, time.June, 26, 12, 0, offset, 0, time.UTC)
			metrics.RequestStarted(at)
			metrics.RequestFinished(HistoryIngestObservation{
				CompletedAt:        at.Add(time.Millisecond),
				Duration:           time.Millisecond,
				StatusCode:         http.StatusAccepted,
				AcceptedEntries:    1,
				AcceptedBuckets:    2,
				HistoryRowsTouched: 1,
			})
		}(index)
	}
	group.Wait()

	snapshot := metrics.Snapshot()
	if snapshot.RequestsTotal != workers || snapshot.InFlight != 0 || snapshot.SucceededTotal != workers ||
		snapshot.AcceptedEntriesTotal != workers || snapshot.AcceptedBucketsTotal != workers*2 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}
