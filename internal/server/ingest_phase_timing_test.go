package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

func TestIngestPhaseTimingPreservesResponseAndAddsHeader(t *testing.T) {
	t.Parallel()

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observability.RecordIngestTransaction(r.Context(), 12*time.Millisecond)
		observability.RecordIngestCommit(r.Context(), 2*time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"accepted":1}`))
	})

	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/prices", nil)
	response := httptest.NewRecorder()
	withIngestPhaseTiming("prices", next).ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusAccepted)
	}
	if response.Body.String() != `{"accepted":1}` {
		t.Fatalf("body = %q", response.Body.String())
	}
	if response.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
	}

	header := response.Header().Get("Server-Timing")
	for _, expected := range []string{
		"api;dur=",
		"postgres-tx;dur=12.000",
		"postgres-commit;dur=2.000",
	} {
		if !strings.Contains(header, expected) {
			t.Fatalf("Server-Timing = %q, want %q", header, expected)
		}
	}
}

func TestIngestPhaseTimingOmitsUnavailableDatabasePhases(t *testing.T) {
	t.Parallel()

	response := httptest.NewRecorder()
	withIngestPhaseTiming("prices", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/ingest/prices", nil))

	header := response.Header().Get("Server-Timing")
	if !strings.Contains(header, "api;dur=") {
		t.Fatalf("Server-Timing = %q, want api duration", header)
	}
	if strings.Contains(header, "postgres-") {
		t.Fatalf("Server-Timing = %q, unexpected postgres timing", header)
	}
}
