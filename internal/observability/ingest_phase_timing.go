package observability

import (
	"context"
	"strings"
	"sync"
	"time"
)

type ingestPhaseContextKey struct{}

type IngestPhaseSnapshot struct {
	Stream      string
	Transaction time.Duration
	Commit      time.Duration
}

type ingestPhaseRecorder struct {
	mu          sync.RWMutex
	stream      string
	transaction time.Duration
	commit      time.Duration
}

func WithIngestPhaseTiming(ctx context.Context, stream string) context.Context {
	return context.WithValue(ctx, ingestPhaseContextKey{}, &ingestPhaseRecorder{
		stream: normalizeIngestStream(stream),
	})
}

func IngestPhaseTiming(ctx context.Context) IngestPhaseSnapshot {
	recorder := ingestPhaseRecorderFromContext(ctx)
	if recorder == nil {
		return IngestPhaseSnapshot{}
	}
	recorder.mu.RLock()
	defer recorder.mu.RUnlock()
	return IngestPhaseSnapshot{
		Stream:      recorder.stream,
		Transaction: recorder.transaction,
		Commit:      recorder.commit,
	}
}

func RecordIngestTransaction(ctx context.Context, duration time.Duration) {
	recorder := ingestPhaseRecorderFromContext(ctx)
	if recorder == nil || duration < 0 {
		return
	}
	recorder.mu.Lock()
	recorder.transaction = duration
	recorder.mu.Unlock()
}

func RecordIngestCommit(ctx context.Context, duration time.Duration) {
	recorder := ingestPhaseRecorderFromContext(ctx)
	if recorder == nil || duration < 0 {
		return
	}
	recorder.mu.Lock()
	recorder.commit = duration
	recorder.mu.Unlock()
}

func ingestPhaseRecorderFromContext(ctx context.Context) *ingestPhaseRecorder {
	if ctx == nil {
		return nil
	}
	recorder, _ := ctx.Value(ingestPhaseContextKey{}).(*ingestPhaseRecorder)
	return recorder
}

func normalizeIngestStream(stream string) string {
	switch strings.TrimSpace(stream) {
	case "prices", "history":
		return strings.TrimSpace(stream)
	default:
		return "unknown"
	}
}
