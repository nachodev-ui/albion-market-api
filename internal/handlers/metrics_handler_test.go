package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

type metricsHandlerDatabaseMonitor struct{}

func (metricsHandlerDatabaseMonitor) Snapshot(context.Context) observability.DatabaseSnapshot {
	return observability.DatabaseSnapshot{Healthy: true}
}

func TestMetricsHandlerExposesPrometheusText(t *testing.T) {
	t.Parallel()

	handler := NewMetricsHandler(observability.NewPrometheusExporter(observability.PrometheusExporterOptions{
		Service:      "albion-market-api",
		Environment:  "test",
		Version:      "test",
		Revision:     "test",
		StartedAt:    time.Now(),
		HTTP:         observability.NewHTTPMetrics(),
		Database:     observability.NewDatabaseMetrics(),
		DatabasePool: metricsHandlerDatabaseMonitor{},
	}))
	response := httptest.NewRecorder()
	handler.Metrics(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); !strings.Contains(contentType, "text/plain") {
		t.Fatalf("Content-Type = %q", contentType)
	}
	if !strings.Contains(response.Body.String(), "albion_market_api_build_info") {
		t.Fatalf("metrics body = %q", response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q", response.Header().Get("Cache-Control"))
	}
}

func TestMetricsHandlerRejectsNonGET(t *testing.T) {
	t.Parallel()

	handler := NewMetricsHandler(nil)
	response := httptest.NewRecorder()
	handler.Metrics(response, httptest.NewRequest(http.MethodPost, "/metrics", nil))

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("Allow = %q", response.Header().Get("Allow"))
	}
}
