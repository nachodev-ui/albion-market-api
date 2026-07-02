package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

type healthRepository struct {
	pingErr error
}

func (r healthRepository) Ping(context.Context) error { return r.pingErr }
func (healthRepository) IngestPrices(context.Context, domain.IngestPricesRequest) (domain.IngestPricesResult, error) {
	return domain.IngestPricesResult{}, nil
}
func (healthRepository) IngestHistory(context.Context, domain.IngestHistoryRequest) (domain.IngestHistoryResult, error) {
	return domain.IngestHistoryResult{}, nil
}
func (healthRepository) QueryCurrentPrices(context.Context, domain.CurrentPriceLookup) ([]domain.CurrentPrice, error) {
	return nil, nil
}
func (healthRepository) QueryMarketHistory(context.Context, domain.MarketHistoryLookup) ([]domain.MarketHistorySeries, error) {
	return nil, nil
}

func TestHealthHandlerDoesNotExposeDatabaseErrors(t *testing.T) {
	t.Parallel()

	marketService := service.NewMarketService(healthRepository{pingErr: errors.New("dial tcp database.internal:5432: connection refused")})
	handler := NewHealthHandler(marketService)
	response := httptest.NewRecorder()

	handler.Healthz(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusServiceUnavailable)
	}
	body := response.Body.String()
	if strings.Contains(body, "database.internal") || !strings.Contains(body, "service unavailable") {
		t.Fatalf("body = %q, want generic unavailable response", body)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
}

func TestHealthHandlerSetsAllowHeader(t *testing.T) {
	t.Parallel()

	marketService := service.NewMarketService(healthRepository{})
	handler := NewHealthHandler(marketService)
	response := httptest.NewRecorder()

	handler.Healthz(response, httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
	if got := response.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("Allow = %q, want GET", got)
	}
}
