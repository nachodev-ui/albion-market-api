package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

func TestIngestPricesExposesServerTimingWithoutChangingJSON(t *testing.T) {
	t.Parallel()

	handler := NewIngestHandler(
		fakeIngestService{response: domain.IngestPricesResponse{
			RequestID:          "00112233-4455-6677-8899-aabbccddeeff",
			Accepted:           1,
			CurrentRowsTouched: 1,
			PersistenceTiming: domain.IngestPersistenceTiming{
				Transaction: 12 * time.Millisecond,
				Commit:      2 * time.Millisecond,
			},
		}},
		testAuthenticator(t, "secret"),
		1<<20,
		observability.NewIngestMetrics(),
		nil,
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/ingest/prices",
		strings.NewReader(validIngestBody(t)),
	)
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.IngestPrices(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	serverTiming := response.Header().Get("Server-Timing")
	for _, expected := range []string{"api;dur=", "postgres-tx;dur=12.000", "postgres-commit;dur=2.000"} {
		if !strings.Contains(serverTiming, expected) {
			t.Fatalf("Server-Timing = %q, want %q", serverTiming, expected)
		}
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "timing") {
		t.Fatalf("internal timing leaked into JSON: %s", response.Body.String())
	}
}

func TestIngestHistoryExposesServerTimingWithoutChangingJSON(t *testing.T) {
	t.Parallel()

	handler := NewIngestHandler(
		fakeIngestService{historyResponse: domain.IngestHistoryResponse{
			RequestID:          "00112233-4455-6677-8899-aabbccddeeff",
			AcceptedEntries:    1,
			AcceptedBuckets:    2,
			HistoryRowsTouched: 2,
			PersistenceTiming: domain.IngestPersistenceTiming{
				Transaction: 20 * time.Millisecond,
				Commit:      3 * time.Millisecond,
			},
		}},
		testAuthenticator(t, "secret"),
		1<<20,
		observability.NewIngestMetrics(),
		nil,
		observability.NewHistoryIngestMetrics(),
	)

	request := httptest.NewRequest(
		http.MethodPost,
		"/api/v1/ingest/history",
		strings.NewReader(validHistoryIngestBody(t)),
	)
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	handler.IngestHistory(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	serverTiming := response.Header().Get("Server-Timing")
	for _, expected := range []string{"api;dur=", "postgres-tx;dur=20.000", "postgres-commit;dur=3.000"} {
		if !strings.Contains(serverTiming, expected) {
			t.Fatalf("Server-Timing = %q, want %q", serverTiming, expected)
		}
	}
	if strings.Contains(strings.ToLower(response.Body.String()), "timing") {
		t.Fatalf("internal timing leaked into JSON: %s", response.Body.String())
	}
}
