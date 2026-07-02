package observability

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type metricsReadinessChecker struct {
	metrics  *ReadinessMetrics
	snapshot ReadinessSnapshot
}

func (c metricsReadinessChecker) Check(context.Context) ReadinessSnapshot {
	c.metrics.Observe(c.snapshot)
	return c.snapshot
}

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
	httpMetrics.RequestFinished("GET", "/readyz", 503, 25*time.Millisecond)

	databaseMetrics := NewDatabaseMetrics()
	databaseMetrics.Observe("query_current_prices", 10*time.Millisecond, nil)
	databaseMetrics.Observe("copy_raw_prices", 20*time.Millisecond, errors.New("copy failed"))
	databaseMetrics.Observe("upsert_current_prices", 30*time.Millisecond, nil)
	databaseMetrics.Observe("transaction_prices", 40*time.Millisecond, nil)

	ingest := NewIngestMetrics()
	now := time.Date(2026, time.July, 2, 18, 0, 0, 0, time.UTC)
	ingest.RequestStarted(now)
	ingest.RequestFinished(IngestObservation{
		CompletedAt:        now.Add(100 * time.Millisecond),
		Duration:           100 * time.Millisecond,
		StatusCode:         202,
		Received:           3,
		Accepted:           2,
		Stored:             2,
		Rejected:           1,
		CurrentRowsTouched: 1,
	})

	readiness := NewReadinessMetrics()
	readiness.Observe(ReadinessSnapshot{
		Ready:     true,
		Duration:  5 * time.Millisecond,
		CheckedAt: now,
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
			Healthy:            true,
			AcquisitionLatency: 2 * time.Millisecond,
			PingLatency:        time.Millisecond,
			Pool: DatabasePoolStats{
				MaxConnections:      10,
				TotalConnections:    2,
				AcquiredConnections: 1,
				IdleConnections:     1,
			},
		}},
		Ingest:    ingest,
		Readiness: readiness,
	})

	var output bytes.Buffer
	if err := exporter.Write(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, expected := range []string{
		`albion_market_api_build_info{environment="test",revision="abc123",service="albion-market-api",version="1.2.3"} 1`,
		`albion_market_api_http_requests_total{method="GET",route="/readyz",status="503"} 1`,
		`albion_market_api_http_errors_total{class="5xx",method="GET",route="/readyz"} 1`,
		`albion_market_api_readiness_ready 1`,
		`albion_market_api_readiness_checks_total{result="success"} 1`,
		`albion_market_api_database_operations_total{operation="query_current_prices",result="success"} 1`,
		`albion_market_api_database_operations_total{operation="copy_raw_prices",result="error"} 1`,
		`albion_market_api_database_errors_total{operation="copy_raw_prices"} 1`,
		`albion_market_api_database_ready 1`,
		`albion_market_api_database_pool_acquisition_duration_seconds 0.002`,
		`albion_market_api_ingest_entries_received_total{stream="prices"} 3`,
		`albion_market_api_ingest_accepted_entries_total{stream="prices"} 2`,
		`albion_market_api_ingest_entries_stored_total{stream="prices"} 2`,
		`albion_market_api_ingest_entries_rejected_total{stream="prices"} 1`,
		`albion_market_api_ingest_copy_duration_seconds_count{stream="prices"} 1`,
		`albion_market_api_ingest_upsert_duration_seconds_count{stream="prices"} 1`,
		`albion_market_api_database_transaction_duration_seconds_count{stream="prices"} 1`,
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
	errorKey := httpErrorKey{Method: "OTHER", Route: "/healthz", Class: "4xx"}
	if snapshot.Errors[errorKey] != 1 {
		t.Fatalf("bounded HTTP error metric = %#v, want OTHER/4xx=1", snapshot.Errors)
	}
}

func TestPrometheusExporterRefreshesReadinessDuringScrape(t *testing.T) {
	t.Parallel()

	readiness := NewReadinessMetrics()
	now := time.Now().UTC()
	exporter := NewPrometheusExporter(PrometheusExporterOptions{
		StartedAt: now.Add(-time.Minute),
		HTTP:      NewHTTPMetrics(),
		Database:  NewDatabaseMetrics(),
		Readiness: readiness,
		ReadinessChecker: metricsReadinessChecker{
			metrics: readiness,
			snapshot: ReadinessSnapshot{
				Ready:           false,
				FailedComponent: ReadinessComponentSchema,
				Duration:        time.Millisecond,
				CheckedAt:       now,
			},
		},
	})

	var output bytes.Buffer
	if err := exporter.Write(context.Background(), &output); err != nil {
		t.Fatal(err)
	}
	body := output.String()
	for _, expected := range []string{
		"albion_market_api_readiness_ready 0",
		`albion_market_api_readiness_checks_total{result="error"} 1`,
		`albion_market_api_readiness_failures_total{component="schema"} 1`,
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("metrics output does not contain %q:\n%s", expected, body)
		}
	}
}
