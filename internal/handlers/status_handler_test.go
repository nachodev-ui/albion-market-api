package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

type fakeDatabaseMonitor struct {
	snapshot observability.DatabaseSnapshot
}

func (f fakeDatabaseMonitor) Snapshot(context.Context) observability.DatabaseSnapshot {
	return f.snapshot
}

func TestStatusHandlerReportsDatabaseAndIngestMetrics(t *testing.T) {
	t.Parallel()

	startedAt := time.Now().UTC().Add(-time.Minute)
	metrics := observability.NewIngestMetrics()
	metrics.RequestStarted(startedAt.Add(10 * time.Second))
	metrics.RequestFinished(observability.IngestObservation{
		CompletedAt:        startedAt.Add(11 * time.Second),
		Duration:           75 * time.Millisecond,
		StatusCode:         http.StatusAccepted,
		Accepted:           500,
		CurrentRowsTouched: 400,
	})

	handler := NewStatusHandler(
		"albion-market-api",
		"test",
		startedAt,
		fakeDatabaseMonitor{snapshot: observability.DatabaseSnapshot{
			Healthy:     true,
			PingLatency: 1250 * time.Microsecond,
			Pool: observability.DatabasePoolStats{
				MaxConnections:      10,
				TotalConnections:    4,
				AcquiredConnections: 1,
				IdleConnections:     3,
				AcquireCount:        12,
			},
		}},
		metrics,
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	handler.Status(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}

	var body statusResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "ok" || body.Database.Status != "ok" {
		t.Fatalf("status = %q database = %q, want ok/ok", body.Status, body.Database.Status)
	}
	if body.Database.Pool.TotalConnections != 4 || body.Database.Pool.IdleConnections != 3 {
		t.Fatalf("pool = %#v, want total=4 idle=3", body.Database.Pool)
	}
	if body.Ingest.RequestsTotal != 1 || body.Ingest.AcceptedEntriesTotal != 500 {
		t.Fatalf("ingest = %#v, want one request and 500 accepted", body.Ingest)
	}
	if body.Ingest.CurrentRowsTouchedTotal != 400 {
		t.Fatalf("current rows touched = %d, want 400", body.Ingest.CurrentRowsTouchedTotal)
	}
}

func TestStatusHandlerReturnsServiceUnavailableWhenDatabaseIsDown(t *testing.T) {
	t.Parallel()

	handler := NewStatusHandler(
		"albion-market-api",
		"test",
		time.Now().UTC(),
		fakeDatabaseMonitor{snapshot: observability.DatabaseSnapshot{
			Healthy: false,
			Err:     errors.New("connection refused"),
		}},
		observability.NewIngestMetrics(),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	handler.Status(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}

	var body statusResponse
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Status != "degraded" || body.Database.Status != "unavailable" {
		t.Fatalf("status = %q database = %q, want degraded/unavailable", body.Status, body.Database.Status)
	}
}

func TestStatusHandlerRejectsNonGETMethods(t *testing.T) {
	t.Parallel()

	handler := NewStatusHandler(
		"albion-market-api",
		"test",
		time.Now().UTC(),
		fakeDatabaseMonitor{},
		observability.NewIngestMetrics(),
	)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	handler.Status(response, request)

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
