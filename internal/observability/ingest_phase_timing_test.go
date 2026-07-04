package observability

import (
	"context"
	"testing"
	"time"
)

func TestIngestPhaseTimingRecordsBoundedRequestLocalDurations(t *testing.T) {
	t.Parallel()

	ctx := WithIngestPhaseTiming(context.Background(), "prices")
	RecordIngestTransaction(ctx, 12*time.Millisecond)
	RecordIngestCommit(ctx, 2*time.Millisecond)

	snapshot := IngestPhaseTiming(ctx)
	if snapshot.Stream != "prices" {
		t.Fatalf("stream = %q, want prices", snapshot.Stream)
	}
	if snapshot.Transaction != 12*time.Millisecond {
		t.Fatalf("transaction = %s, want 12ms", snapshot.Transaction)
	}
	if snapshot.Commit != 2*time.Millisecond {
		t.Fatalf("commit = %s, want 2ms", snapshot.Commit)
	}
}

func TestIngestPhaseTimingIgnoresMissingRecorder(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	RecordIngestTransaction(ctx, time.Second)
	RecordIngestCommit(ctx, time.Second)

	if snapshot := IngestPhaseTiming(ctx); snapshot != (IngestPhaseSnapshot{}) {
		t.Fatalf("snapshot = %#v, want zero value", snapshot)
	}
}
