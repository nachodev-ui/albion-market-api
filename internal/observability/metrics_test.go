package observability

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type metricsDatabaseMonitor struct {
	snapshot DatabaseSnapshot
}

func (m metricsDatabaseMonitor) Snapshot(context.Context) DatabaseSnapshot {
	return m.snapshot
}

func TestPrometheusExporterExposesBoundedMetrics(t *testing.T) {
	t.Parallel()

	httpMetrics := NewHTTPMetrics()
	httpMetrics.RequestStarted()
	httpMetrics.RequestFinished("GET", "/readyz", 200, 25*time.Millisecond)

	databaseMetrics := NewDatabaseMetrics()
	databaseMetrics.Observe("query_current_prices", 10*time.Millisecond, nil)
	databaseMetrics.Observe("copy_raw_prices", 20*time.Millisecond, errors.New("copy failed"))

	ingest := NewIngestMetrics()
	now := time.Date(2026, time.July, 2, 18, 0, 0, 0, time.UTC)
	ingest.RequestStarted(now)
	ingest.RequestFinished(IngestObservation{
		CompletedAt:        now.Add(100 * time.Millisecond),
		Duration:           100 * time.Millisecond,
		StatusCode:         202,
		Accepted:           2,
		CurrentRowsTouched: 1,
	})

	exporter := NewPrometheusExporter(PrometheusExporterOptions{
		Service:     "albion-market-api",
		Environment: "test",
		Version:     "1.2.3",
		Revision:    "abc123",
		StartedAt:   now.Add(-time.Minute),
		HTTP:        httpMetrics,
		Database:    databaseMetrics,
		DatabasePool: metricsDatabaseMonitor{snapshot: DatabaseSnapshot{
			Healthy:     true,
			PingLatency: time.Millisecond,
			Pool: DatabasePoolStats{
				MaxConnections:      10,
				TotalConnections:    2,
				AcquiredConnections: 1,
				IdleConnections:     1,
			},
		}},
		Ingest: ingest,
	})

	var output bytes.Buffer
	if err := exporter.Write(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, expected := range []string{
		`albion_market_api_build_info{environment="test",revision="abc123",service="albion-market-api",version="1.2.3"} 1`,
		`albion_market_api_http_requests_total{method="GET",route="/readyz",status="200"} 1`,
		`albion_market_api_database_operations_total{operation="query_current_prices",result="success"} 1`,
		`albion_market_api_database_operations_total{operation="copy_raw_prices",result="error"} 1`,
		`albion_market_api_database_ready 1`,
		`albion_market_api_ingest_accepted_entries_total{stream="prices"} 2`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics output does not contain %q:\n%s", expected, body)
		}
	}
	for _, forbidden := range []string{"request_id", "item_id", "database_url", "token"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("high-cardinality or sensitive label %q found in metrics output", forbidden)
		}
	}
}

func TestMetricsAreConcurrencySafe(t *testing.T) {
	t.Parallel()

	httpMetrics := NewHTTPMetrics()
	databaseMetrics := NewDatabaseMetrics()
	const workers = 100
	done := make(chan struct{}, workers)
	for index := 0; index < workers; index++ {
		go func() {
			httpMetrics.RequestStarted()
			httpMetrics.RequestFinished("POST", "/api/v1/ingest/prices", 202, time.Millisecond)
			databaseMetrics.Observe("ingest_prices", time.Millisecond, nil)
			done <- struct{}{}
		}()
	}
	for index := 0; index < workers; index++ {
		<-done
	}

	httpSnapshot := httpMetrics.Snapshot()
	if httpSnapshot.InFlight != 0 || len(httpSnapshot.Requests) != 1 {
		t.Fatalf("HTTP snapshot = %#v", httpSnapshot)
	}
	databaseSnapshot := databaseMetrics.Snapshot()
	if databaseSnapshot.Operations[databaseOperationKey{Operation: "ingest_prices", Result: "success"}] != workers {
		t.Fatalf("database snapshot = %#v", databaseSnapshot)
	}
}

func TestHTTPMetricsBoundsUnknownMethods(t *testing.T) {
	t.Parallel()

	metrics := NewHTTPMetrics()
	metrics.RequestStarted()
	metrics.RequestFinished("ATTACKER-CONTROLLED-METHOD", "/healthz", 405, time.Millisecond)

	snapshot := metrics.Snapshot()
	key := httpRequestKey{Method: "OTHER", Route: "/healthz", Status: "405"}
	if snapshot.Requests[key] != 1 {
		t.Fatalf("bounded method metric = %#v, want OTHER=1", snapshot.Requests)
	}
}
