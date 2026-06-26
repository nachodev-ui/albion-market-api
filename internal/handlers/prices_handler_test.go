package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

type fakePricesService struct {
	includeDisabled bool
	markets         domain.MarketCatalogResponse
	request         domain.PriceQueryRequest
	response        domain.PriceQueryResponse
	err             error
}

func (f *fakePricesService) Markets(includeDisabled bool) domain.MarketCatalogResponse {
	f.includeDisabled = includeDisabled
	return f.markets
}

func (f *fakePricesService) QueryCurrentPrices(_ context.Context, request domain.PriceQueryRequest) (domain.PriceQueryResponse, error) {
	f.request = request
	return f.response, f.err
}

func TestListMarketsReturnsPublicCatalogWithoutLocationIDs(t *testing.T) {
	t.Parallel()

	fake := &fakePricesService{markets: domain.MarketCatalogResponse{
		SchemaVersion: 1,
		Count:         1,
		Data: []domain.MarketDefinition{{
			Key: "brecilien", Name: "Brecilien", Type: domain.MarketTypeRegular, Enabled: true,
		}},
	}}
	handler := NewPricesHandler(fake)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/markets?includeDisabled=true", nil)
	response := httptest.NewRecorder()

	handler.ListMarkets(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if !fake.includeDisabled {
		t.Fatal("includeDisabled was not forwarded")
	}
	body := response.Body.String()
	if strings.Contains(body, "locationId") || strings.Contains(body, "location_id") {
		t.Fatalf("public catalog leaked an internal location ID: %s", body)
	}
	if !strings.Contains(body, `"schemaVersion":1`) || !strings.Contains(body, `"key":"brecilien"`) {
		t.Fatalf("unexpected body: %s", body)
	}
}

func TestQueryCurrentPricesAcceptsFrontendContract(t *testing.T) {
	t.Parallel()

	requestedAt := time.Date(2026, time.June, 26, 4, 0, 0, 0, time.UTC)
	fake := &fakePricesService{response: domain.PriceQueryResponse{
		RequestedAt: requestedAt,
		Count:       1,
		Data: []domain.CurrentPrice{{
			Server: domain.ServerWest, MarketKey: "martlock", LocationID: 3008,
			ItemKey: "T4_BAG", Quality: 1, UpdatedAt: requestedAt,
		}},
	}}
	handler := NewPricesHandler(fake)
	body := `{
		"server":"west",
		"marketKeys":["martlock"],
		"entries":[{"itemIdentifier":"T4_BAG","quality":1}]
	}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/prices/query", strings.NewReader(body))
	response := httptest.NewRecorder()

	handler.QueryCurrentPrices(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if len(fake.request.MarketKeys) != 1 || fake.request.MarketKeys[0] != "martlock" {
		t.Fatalf("request marketKeys = %#v", fake.request.MarketKeys)
	}
	if len(fake.request.Entries) != 1 || fake.request.Entries[0].ItemKey != "T4_BAG" {
		t.Fatalf("request entries = %#v", fake.request.Entries)
	}
	if strings.Contains(response.Body.String(), "3008") || strings.Contains(response.Body.String(), "location") {
		t.Fatalf("price response leaked internal location ID: %s", response.Body.String())
	}
}

func TestGetCurrentPricesBuildsReceiverCompatibleQuery(t *testing.T) {
	t.Parallel()

	fake := &fakePricesService{response: domain.PriceQueryResponse{Data: []domain.CurrentPrice{}}}
	handler := NewPricesHandler(fake)
	request := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/prices?server=europe&itemIds=T4_BAG,T5_BAG&marketKey=fort_sterling&quality=3",
		nil,
	)
	response := httptest.NewRecorder()

	handler.GetCurrentPrices(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	if fake.request.Server != domain.ServerEurope {
		t.Fatalf("server = %q", fake.request.Server)
	}
	if len(fake.request.MarketKeys) != 1 || fake.request.MarketKeys[0] != "fort_sterling" {
		t.Fatalf("marketKeys = %#v", fake.request.MarketKeys)
	}
	if len(fake.request.Entries) != 2 || fake.request.Entries[1].ItemKey != "T5_BAG" || fake.request.Entries[1].Quality != 3 {
		t.Fatalf("entries = %#v", fake.request.Entries)
	}
}

func TestQueryCurrentPricesReturnsValidationAndSafeInternalErrors(t *testing.T) {
	t.Parallel()

	t.Run("validation", func(t *testing.T) {
		fake := &fakePricesService{err: errors.Join(service.ErrInvalidPriceQuery, errors.New("marketKeys is required"))}
		handler := NewPricesHandler(fake)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/prices/query", strings.NewReader(`{"server":"west","marketKeys":[],"entries":[]}`))
		response := httptest.NewRecorder()

		handler.QueryCurrentPrices(response, request)

		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
		}
	})

	t.Run("internal", func(t *testing.T) {
		fake := &fakePricesService{err: errors.New("password authentication failed")}
		handler := NewPricesHandler(fake)
		request := httptest.NewRequest(http.MethodPost, "/api/v1/prices/query", strings.NewReader(`{"server":"west","marketKeys":["martlock"],"entries":[{"itemIdentifier":"T4_BAG","quality":1}]}`))
		response := httptest.NewRecorder()

		handler.QueryCurrentPrices(response, request)

		if response.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "password") {
			t.Fatalf("internal error leaked: %s", response.Body.String())
		}
	})
}

func TestQueryCurrentPricesRejectsUnknownJSONFields(t *testing.T) {
	t.Parallel()

	handler := NewPricesHandler(&fakePricesService{})
	request := httptest.NewRequest(http.MethodPost, "/api/v1/prices/query", strings.NewReader(`{
		"server":"west",
		"marketKeys":["martlock"],
		"entries":[{"itemIdentifier":"T4_BAG","quality":1}],
		"location_ids":[3008]
	}`))
	response := httptest.NewRecorder()

	handler.QueryCurrentPrices(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d; body=%s", response.Code, response.Body.String())
	}
	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["error"] != "invalid json" {
		t.Fatalf("payload = %#v", payload)
	}
}
