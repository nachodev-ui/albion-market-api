package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/handlers"
	"github.com/nachodev-ui/albion-market-api/internal/observability"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

type routerRepository struct{}

func (routerRepository) Ping(context.Context) error { return nil }
func (routerRepository) IngestPrices(context.Context, domain.IngestPricesRequest) (domain.IngestPricesResult, error) {
	return domain.IngestPricesResult{}, nil
}
func (routerRepository) QueryCurrentPrices(context.Context, domain.PriceQueryRequest) ([]domain.CurrentPrice, error) {
	return []domain.CurrentPrice{}, nil
}

type routerDatabaseMonitor struct{}

func (routerDatabaseMonitor) Snapshot(context.Context) observability.DatabaseSnapshot {
	return observability.DatabaseSnapshot{Healthy: true}
}

func TestRouterExposesStatusEndpoint(t *testing.T) {
	t.Parallel()

	marketService := service.NewMarketService(routerRepository{})
	metrics := observability.NewIngestMetrics()

	router := NewRouter(
		handlers.NewHealthHandler(marketService),
		handlers.NewIngestHandler(marketService, []string{"secret"}, 1<<20, metrics, nil),
		handlers.NewPricesHandler(marketService),
		handlers.NewStatusHandler("albion-market-api", "test", time.Now(), routerDatabaseMonitor{}, metrics),
	)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/status", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
}
