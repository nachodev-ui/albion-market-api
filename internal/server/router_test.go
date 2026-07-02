package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/handlers"
	"github.com/nachodev-ui/albion-market-api/internal/ingestauth"
	"github.com/nachodev-ui/albion-market-api/internal/observability"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

type routerRepository struct{}

func (routerRepository) Ping(context.Context) error { return nil }
func (routerRepository) IngestPrices(context.Context, domain.IngestPricesRequest) (domain.IngestPricesResult, error) {
	return domain.IngestPricesResult{}, nil
}
func (routerRepository) IngestHistory(context.Context, domain.IngestHistoryRequest) (domain.IngestHistoryResult, error) {
	return domain.IngestHistoryResult{}, nil
}
func (routerRepository) QueryMarketHistory(context.Context, domain.MarketHistoryLookup) ([]domain.MarketHistorySeries, error) {
	return []domain.MarketHistorySeries{}, nil
}
func (routerRepository) QueryCurrentPrices(context.Context, domain.CurrentPriceLookup) ([]domain.CurrentPrice, error) {
	return []domain.CurrentPrice{}, nil
}

type routerReadinessChecker struct{}

func (routerReadinessChecker) Check(context.Context) observability.ReadinessSnapshot {
	return observability.ReadinessSnapshot{Ready: true}
}

type routerDatabaseMonitor struct{}

func (routerDatabaseMonitor) Snapshot(context.Context) observability.DatabaseSnapshot {
	return observability.DatabaseSnapshot{Healthy: true}
}

func newTestRouter(t *testing.T) http.Handler {
	marketService := service.NewMarketService(routerRepository{})
	metrics := observability.NewIngestMetrics()
	historyMetrics := observability.NewHistoryIngestMetrics()

	startedAt := time.Now()
	httpMetrics := observability.NewHTTPMetrics()
	databaseMetrics := observability.NewDatabaseMetrics()
	metricsHandler := handlers.NewMetricsHandler(observability.NewPrometheusExporter(observability.PrometheusExporterOptions{
		Service:       "albion-market-api",
		Environment:   "test",
		Version:       "test",
		Revision:      "test",
		StartedAt:     startedAt,
		HTTP:          httpMetrics,
		Database:      databaseMetrics,
		DatabasePool:  routerDatabaseMonitor{},
		Ingest:        metrics,
		HistoryIngest: historyMetrics,
	}))

	return NewRouter(
		handlers.NewHealthHandler(routerReadinessChecker{}),
		handlers.NewIngestHandler(marketService, routerAuthenticator(t), 1<<20, metrics, nil, historyMetrics),
		handlers.NewPricesHandler(marketService, 1<<20),
		handlers.NewHistoryHandler(marketService, 1<<20),
		handlers.NewStatusHandler("albion-market-api", "test", startedAt, routerDatabaseMonitor{}, metrics, historyMetrics),
		metricsHandler,
		SecurityOptions{AllowedOrigins: []string{"http://localhost:5173"}},
		ObservabilityOptions{HTTPMetrics: httpMetrics},
	)
}

func TestRouterExposesStatusEndpoint(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	newTestRouter(t).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
}

func TestRouterExposesFrontendReadEndpoints(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{
			name:   "market catalog",
			method: http.MethodGet,
			path:   "/api/v1/markets",
		},
		{
			name:   "simple prices",
			method: http.MethodGet,
			path:   "/api/v1/prices?server=west&itemIds=T4_BAG&marketKey=martlock&quality=1",
		},
		{
			name:   "batch prices",
			method: http.MethodPost,
			path:   "/api/v1/prices/query",
			body:   `{"server":"west","marketKeys":["martlock"],"entries":[{"itemIdentifier":"T4_BAG","quality":1}]}`,
		},
		{
			name:   "simple history",
			method: http.MethodGet,
			path:   "/api/v1/history?server=west&itemId=T4_BAG&marketKey=martlock&quality=1&period=4-weeks&limit=1",
		},
		{
			name:   "batch history",
			method: http.MethodPost,
			path:   "/api/v1/history/query",
			body:   `{"server":"west","marketKeys":["martlock"],"entries":[{"itemIdentifier":"T4_BAG","quality":1}],"rangeStart":"2026-06-01","rangeEnd":"2026-06-25"}`,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			response := httptest.NewRecorder()
			newTestRouter(t).ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("status code = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
			}
		})
	}
}

func TestRouterAllowsFrontendPreflight(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodOptions, "/api/v1/prices/query", nil)
	request.Header.Set("Origin", "http://localhost:5173")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response := httptest.NewRecorder()
	newTestRouter(t).ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusNoContent)
	}
	if response.Header().Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("missing CORS header: %#v", response.Header())
	}
}

func TestRouterExposesAuthenticatedHistoryIngestEndpoint(t *testing.T) {
	t.Parallel()

	body := `{
		"request_id":"00112233-4455-6677-8899-aabbccddeeff",
		"server":"west",
		"entries":[{
			"observed_at":"2026-06-26T12:00:00Z",
			"location_id":4002,
			"item_key":"T4_BAG",
			"quality":1,
			"history":[{
				"timestamp":"2026-06-25T00:00:00Z",
				"item_count":12,
				"average_unit_price":4500
			}]
		}]
	}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest/history", strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer secret")
	response := httptest.NewRecorder()
	newTestRouter(t).ServeHTTP(response, request)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status code = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
}

func routerAuthenticator(t *testing.T) *ingestauth.Authenticator {
	t.Helper()
	authenticator, err := ingestauth.New(
		[]ingestauth.Credential{{ID: "current", Token: "secret"}},
		ingestauth.Options{},
	)
	if err != nil {
		t.Fatal(err)
	}
	return authenticator
}

func TestRouterExposesOperationalEndpoints(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/healthz", "/readyz", "/metrics"} {
		t.Run(path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, path, nil)
			response := httptest.NewRecorder()
			newTestRouter(t).ServeHTTP(response, request)

			if response.Code != http.StatusOK {
				t.Fatalf("%s status code = %d, want %d; body=%s", path, response.Code, http.StatusOK, response.Body.String())
			}
			if response.Header().Get("X-Request-ID") == "" {
				t.Fatalf("%s response is missing X-Request-ID", path)
			}
		})
	}
}

func TestRouterPreservesValidRequestID(t *testing.T) {
	t.Parallel()

	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	request.Header.Set("X-Request-ID", "trace-123")
	response := httptest.NewRecorder()
	newTestRouter(t).ServeHTTP(response, request)

	if got := response.Header().Get("X-Request-ID"); got != "trace-123" {
		t.Fatalf("X-Request-ID = %q, want trace-123", got)
	}
}
