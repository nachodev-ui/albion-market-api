package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/observability"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

func TestHistoryIngestHandlerRecordsMetricsAndOrderedLog(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	priceMetrics := observability.NewIngestMetrics()
	historyMetrics := observability.NewHistoryIngestMetrics()
	handler := NewIngestHandler(
		fakeIngestService{historyResponse: domain.IngestHistoryResponse{
			RequestID:          "00112233-4455-6677-8899-aabbccddeeff",
			AcceptedEntries:    1,
			AcceptedBuckets:    2,
			HistoryRowsTouched: 2,
		}},
		testAuthenticator(t, "secret"),
		1<<20,
		priceMetrics,
		observability.NewLogger(&logs, "never"),
		historyMetrics,
	)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/history", strings.NewReader(validHistoryIngestBody(t)))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.IngestHistory(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status code = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	snapshot := historyMetrics.Snapshot()
	if snapshot.RequestsTotal != 1 || snapshot.SucceededTotal != 1 ||
		snapshot.AcceptedEntriesTotal != 1 || snapshot.AcceptedBucketsTotal != 2 ||
		snapshot.HistoryRowsTouchedTotal != 2 {
		t.Fatalf("history metrics = %#v", snapshot)
	}
	if priceMetrics.Snapshot().RequestsTotal != 0 {
		t.Fatalf("price ingest metrics were modified: %#v", priceMetrics.Snapshot())
	}

	line := logs.String()
	ordered := []string{
		"ingest.history_completed",
		`request_id="00112233-4455-6677-8899-aabbccddeeff"`,
		`server="west"`,
		"entries=1",
		"buckets=2",
		"accepted_entries=1",
		"accepted_buckets=2",
		"history_rows_touched=2",
		"duplicate=false",
		"status=202",
		"duration_ms=",
		`auth_key_id="current"`,
	}
	last := -1
	for _, fragment := range ordered {
		index := strings.Index(line, fragment)
		if index <= last {
			t.Fatalf("history log fields are not ordered; fragment %q in %q", fragment, line)
		}
		last = index
	}
}

func TestHistoryIngestHandlerReturnsOKForIdempotentDuplicate(t *testing.T) {
	t.Parallel()

	historyMetrics := observability.NewHistoryIngestMetrics()
	handler := NewIngestHandler(
		fakeIngestService{historyResponse: domain.IngestHistoryResponse{
			RequestID:          "00112233-4455-6677-8899-aabbccddeeff",
			AcceptedEntries:    1,
			AcceptedBuckets:    2,
			HistoryRowsTouched: 2,
			Duplicate:          true,
		}},
		testAuthenticator(t, "secret"),
		1<<20,
		observability.NewIngestMetrics(),
		nil,
		historyMetrics,
	)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/history", strings.NewReader(validHistoryIngestBody(t)))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.IngestHistory(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	snapshot := historyMetrics.Snapshot()
	if snapshot.DuplicatesTotal != 1 || snapshot.AcceptedBucketsTotal != 0 {
		t.Fatalf("duplicate metrics = %#v", snapshot)
	}
}

func TestHistoryIngestHandlerHidesInternalErrorAndLogsDetail(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := NewIngestHandler(
		fakeIngestService{historyErr: errors.New("password authentication failed")},
		testAuthenticator(t, "secret"),
		1<<20,
		observability.NewIngestMetrics(),
		observability.NewLogger(&logs, "never"),
	)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/history", strings.NewReader(validHistoryIngestBody(t)))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	handler.IngestHistory(response, request)

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

func TestHistoryIngestHandlerMapsValidationAndConflicts(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{name: "validation", err: service.ErrInvalidHistoryIngestRequest, wantStatus: http.StatusBadRequest},
		{name: "processing", err: service.ErrIngestRequestAlreadyProcessing, wantStatus: http.StatusConflict},
		{name: "payload conflict", err: service.ErrIngestRequestPayloadConflict, wantStatus: http.StatusConflict},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := NewIngestHandler(
				fakeIngestService{historyErr: test.err},
				testAuthenticator(t, "secret"),
				1<<20,
				observability.NewIngestMetrics(),
				nil,
			)
			request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/history", strings.NewReader(validHistoryIngestBody(t)))
			request.Header.Set("Authorization", "Bearer secret")
			response := httptest.NewRecorder()
			handler.IngestHistory(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status code = %d, want %d; body=%s", response.Code, test.wantStatus, response.Body.String())
			}
		})
	}
}

func validHistoryIngestBody(t *testing.T) string {
	t.Helper()

	price := int64(4500)
	body, err := json.Marshal(domain.IngestHistoryRequest{
		RequestID: "00112233-4455-6677-8899-aabbccddeeff",
		Server:    domain.ServerWest,
		Entries: []domain.HistoryIngest{{
			ObservedAt: time.Date(2026, time.June, 26, 20, 0, 0, 0, time.UTC),
			LocationID: 4002,
			ItemKey:    "T4_BAG",
			Quality:    1,
			History: []domain.HistoryBucketIngest{
				{Timestamp: time.Date(2026, time.June, 24, 0, 0, 0, 0, time.UTC), ItemCount: 10, AverageUnitPrice: &price},
				{Timestamp: time.Date(2026, time.June, 25, 0, 0, 0, 0, time.UTC), ItemCount: 12, AverageUnitPrice: &price},
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal history body: %v", err)
	}
	return string(body)
}

func TestIngestHistoryRejectsUnsupportedContentType(t *testing.T) {
	t.Parallel()

	handler := NewIngestHandler(
		fakeIngestService{},
		testAuthenticator(t, "secret"),
		1<<20,
		observability.NewIngestMetrics(),
		nil,
		observability.NewHistoryIngestMetrics(),
	)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/history", strings.NewReader(validHistoryIngestBody(t)))
	request.Header.Set("Authorization", "Bearer secret")
	request.Header.Set("Content-Type", "text/plain")
	response := httptest.NewRecorder()

	handler.IngestHistory(response, request)

	if response.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnsupportedMediaType)
	}
}
