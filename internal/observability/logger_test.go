package observability

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestLoggerKeepsFieldsOrdered(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := NewLogger(&output, "never")
	logger.now = func() time.Time {
		return time.Date(2026, time.June, 25, 20, 0, 0, 0, time.UTC)
	}

	logger.Success(
		"ingest.completed",
		F("request_id", "abc"),
		F("entries", 500),
		F("status", 202),
	)

	got := strings.TrimSpace(output.String())
	want := `2026-06-25T20:00:00Z [OK   ] ingest.completed request_id="abc" entries=500 status=202`
	if got != want {
		t.Fatalf("log line = %q, want %q", got, want)
	}
}

func TestLoggerCanForceColors(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	logger := NewLogger(&output, "always")
	logger.Info("api.started")

	if !strings.Contains(output.String(), "\x1b[36m") {
		t.Fatalf("colored log = %q, want ANSI cyan code", output.String())
	}
}
