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
	snapshot  observability.DatabaseSnapshot
	dataTrust observability.DataTrustSnapshot
}

func (f fakeDatabaseMonitor) Snapshot(context.Context) observability.DatabaseSnapshot {
	return f.snapshot
}

func (f fakeDatabaseMonitor) DataTrustSnapshot(context.Context, time.Time) observability.DataTrustSnapshot {
	return f.dataTrust
}

func TestStatusHandlerReportsDatabaseIngestAndDataTrustMetrics(t *testing.T) {
	t.Parallel()

	startedAt := time.Now().UTC().Add(-time.Minute)
	lastPrice := startedAt.Add(30 * time.Second)
	lastHistory := startedAt.Add(20 * time.Second)
	lastMarket := startedAt.Add(40 * time.Second)
	metrics := observability.NewIngestMetrics()
	historyMetrics := observability.NewHistoryIngestMetrics()
	metrics.RequestStarted(startedAt.Add(10 * time.Second))
	metrics.RequestFinished(observability.IngestObservation{
		CompletedAt:        startedAt.Add(11 * time.Second),
		Duration:           75 * time.Millisecond,
		StatusCode:         http.StatusAccepted,
		Accepted:           500,
		CurrentRowsTouched: 400,
	})
	historyMetrics.RequestStarted(startedAt.Add(20 * time.Second))
	historyMetrics.RequestFinished(observability.HistoryIngestObservation{
		CompletedAt:        startedAt.Add(21 * time.Second),
		Duration:           50 * time.Millisecond,
		StatusCode:         http.StatusAccepted,
		AcceptedEntries:    2,
		AcceptedBuckets:    68,
		HistoryRowsTouched: 60,
	})

	handler := NewStatusHandler(
		"albion-market-api",
		"test",
		startedAt,
		fakeDatabaseMonitor{
			snapshot: observability.DatabaseSnapshot{
				Healthy:     true,
				PingLatency: 1250 * time.Microsecond,
				Pool: observability.DatabasePoolStats{
					MaxConnections:      10,
					TotalConnections:    4,
					AcquiredConnections: 1,
					IdleConnections:     3,
					AcquireCount:        12,
				},
			},
			dataTrust: observability.DataTrustSnapshot{
				LastPriceReceptionAt:   &lastPrice,
				LastHistoryReceptionAt: &lastHistory,
				TotalObjects:           100,
				RecentObjects:          75,
				Servers: []observability.DataCoverage{{
					Key: "west", Name: "Americas", TotalObjects: 80, RecentObjects: 64, LastUpdatedAt: &lastMarket,
				}},
				Markets: []observability.DataCoverage{{
					Key: "martlock", Name: "Martlock", TotalObjects: 50, RecentObjects: 25, LastUpdatedAt: &lastMarket,
				}},
			},
		},
		metrics,
		historyMetrics,
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
	if body.HistoryIngest.RequestsTotal != 1 || body.HistoryIngest.AcceptedEntriesTotal != 2 ||
		body.HistoryIngest.AcceptedBucketsTotal != 68 || body.HistoryIngest.HistoryRowsTouchedTotal != 60 {
		t.Fatalf("history ingest = %#v", body.HistoryIngest)
	}
	if body.DataTrust.Status != "ok" || body.DataTrust.RecentObjectsPercent != 75 {
		t.Fatalf("data trust = %#v, want status ok and 75 percent", body.DataTrust)
	}
	if len(body.DataTrust.Markets) != 1 || body.DataTrust.Markets[0].RecentObjectsPercent != 50 {
		t.Fatalf("market coverage = %#v", body.DataTrust.Markets)
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
	if body.DataTrust.Status != "unavailable" {
		t.Fatalf("data trust status = %q, want unavailable", body.DataTrust.Status)
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
