package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/ingestauth"
	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

type fakeIngestService struct {
	response        domain.IngestPricesResponse
	err             error
	historyResponse domain.IngestHistoryResponse
	historyErr      error
}

func (f fakeIngestService) IngestPrices(context.Context, domain.IngestPricesRequest) (domain.IngestPricesResponse, error) {
	return f.response, f.err
}

func (f fakeIngestService) IngestHistory(context.Context, domain.IngestHistoryRequest) (domain.IngestHistoryResponse, error) {
	return f.historyResponse, f.historyErr
}

func TestIngestHandlerRecordsLatencyAndOrderedSuccessLog(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	metrics := observability.NewIngestMetrics()
	handler := NewIngestHandler(
		fakeIngestService{response: domain.IngestPricesResponse{
			RequestID:          "00112233-4455-6677-8899-aabbccddeeff",
			Accepted:           1,
			CurrentRowsTouched: 1,
		}},
		testAuthenticator(t, "secret"),
		1<<20,
		metrics,
		observability.NewLogger(&logs, "never"),
	)

	body := validIngestBody(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/prices", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.IngestPrices(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status code = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	snapshot := metrics.Snapshot()
	if snapshot.RequestsTotal != 1 || snapshot.SucceededTotal != 1 || snapshot.AcceptedEntriesTotal != 1 {
		t.Fatalf("metrics = %#v, want one successful accepted entry", snapshot)
	}
	line := logs.String()
	ordered := []string{
		"ingest.completed",
		`request_id="00112233-4455-6677-8899-aabbccddeeff"`,
		`server="west"`,
		"entries=1",
		"accepted=1",
		"current_rows_touched=1",
		"duplicate=false",
		"status=202",
		"duration_ms=",
		`auth_key_id="current"`,
	}
	last := -1
	for _, fragment := range ordered {
		index := strings.Index(line, fragment)
		if index <= last {
			t.Fatalf("log fields are not ordered; fragment %q in %q", fragment, line)
		}
		last = index
	}
}

func TestIngestHandlerHidesInternalErrorFromResponse(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := NewIngestHandler(
		fakeIngestService{err: errors.New("password authentication failed")},
		testAuthenticator(t, "secret"),
		1<<20,
		observability.NewIngestMetrics(),
		observability.NewLogger(&logs, "never"),
	)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/prices", strings.NewReader(validIngestBody(t)))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.IngestPrices(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if strings.Contains(response.Body.String(), "password authentication") {
		t.Fatalf("response leaked internal error: %s", response.Body.String())
	}
	if !strings.Contains(logs.String(), "password authentication failed") {
		t.Fatalf("internal error was not logged: %s", logs.String())
	}
}

func validIngestBody(t *testing.T) string {
	t.Helper()

	price := int64(1234)
	now := time.Date(2026, time.June, 25, 20, 0, 0, 0, time.UTC)
	body, err := json.Marshal(domain.IngestPricesRequest{
		RequestID: "00112233-4455-6677-8899-aabbccddeeff",
		Server:    domain.ServerWest,
		Entries: []domain.PriceIngest{{
			ObservedAt:     now,
			LocationID:     4002,
			ItemKey:        "T4_PLANKS_LEVEL4@4",
			Quality:        1,
			SellPriceMin:   &price,
			SellPriceMinAt: &now,
		}},
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return string(body)
}

func TestIngestPricesRejectsUnsupportedContentType(t *testing.T) {
	t.Parallel()

	handler := NewIngestHandler(
		fakeIngestService{},
		testAuthenticator(t, "secret"),
		1<<20,
		observability.NewIngestMetrics(),
		nil,
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/prices", strings.NewReader(validIngestBody(t)))
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	handler.IngestPrices(response, request)

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
}

func TestIngestHandlerReturnsBearerChallengeWithoutLeakingToken(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := NewIngestHandler(
		fakeIngestService{},
		testAuthenticator(t, "valid-secret"),
		1<<20,
		observability.NewIngestMetrics(),
		observability.NewLogger(&logs, "never"),
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/prices", strings.NewReader(validIngestBody(t)))
	request.Header.Set("Authorization", "Bearer invalid-secret")
	response := httptest.NewRecorder()

	handler.IngestPrices(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if got := response.Header().Get("WWW-Authenticate"); got != `Bearer realm="ingest"` {
		t.Fatalf("WWW-Authenticate = %q", got)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if strings.Contains(response.Body.String(), "invalid-secret") || strings.Contains(logs.String(), "invalid-secret") {
		t.Fatal("provided bearer token leaked to response or logs")
	}
}

func TestIngestHandlerRejectsPlainHTTPWhenRequired(t *testing.T) {
	t.Parallel()

	authenticator, err := ingestauth.New(
		[]ingestauth.Credential{{ID: "current", Token: "valid-secret"}},
		ingestauth.Options{RequireHTTPS: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewIngestHandler(
		fakeIngestService{},
		authenticator,
		1<<20,
		observability.NewIngestMetrics(),
		nil,
	)
	request := httptest.NewRequest(http.MethodPost, "http://api.example.test/api/v1/ingest/prices", strings.NewReader(validIngestBody(t)))
	request.Header.Set("Authorization", "Bearer valid-secret")
	response := httptest.NewRecorder()

	handler.IngestPrices(response, request)

	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUpgradeRequired)
	}
	if got := response.Header().Get("Upgrade"); got != "TLS/1.2" {
		t.Fatalf("Upgrade = %q, want TLS/1.2", got)
	}
}
