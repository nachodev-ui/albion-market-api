package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
)

type priceQueryRepository struct {
	lookup domain.CurrentPriceLookup
	prices []domain.CurrentPrice
	err    error
}

func (r *priceQueryRepository) Ping(context.Context) error { return nil }

func (r *priceQueryRepository) IngestPrices(context.Context, domain.IngestPricesRequest) (domain.IngestPricesResult, error) {
	return domain.IngestPricesResult{}, nil
}

func (r *priceQueryRepository) IngestHistory(context.Context, domain.IngestHistoryRequest) (domain.IngestHistoryResult, error) {
	return domain.IngestHistoryResult{}, nil
}

func (r *priceQueryRepository) QueryMarketHistory(context.Context, domain.MarketHistoryLookup) ([]domain.MarketHistorySeries, error) {
	return []domain.MarketHistorySeries{}, nil
}

func (r *priceQueryRepository) QueryCurrentPrices(_ context.Context, lookup domain.CurrentPriceLookup) ([]domain.CurrentPrice, error) {
	r.lookup = lookup
	return append([]domain.CurrentPrice(nil), r.prices...), r.err
}

func TestQueryCurrentPricesResolvesMarketKeysAndHidesLocations(t *testing.T) {
	t.Parallel()

	updatedAt := time.Date(2026, time.June, 26, 3, 0, 0, 0, time.UTC)
	repo := &priceQueryRepository{prices: []domain.CurrentPrice{
		{Server: domain.ServerWest, LocationID: 4002, ItemKey: "T4_BAG", Quality: 1, UpdatedAt: updatedAt},
		{Server: domain.ServerWest, LocationID: 5003, ItemKey: "T4_BAG", Quality: 1, UpdatedAt: updatedAt},
	}}
	service := NewMarketService(repo)
	requestedAt := time.Date(2026, time.June, 26, 4, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return requestedAt }

	response, err := service.QueryCurrentPrices(context.Background(), domain.PriceQueryRequest{
		Server:     domain.ServerWest,
		MarketKeys: []string{" Brecilien ", "fort_sterling", "brecilien"},
		Entries: []domain.PriceQueryEntry{
			{ItemKey: " T4_BAG ", Quality: 1},
			{ItemKey: "T4_BAG", Quality: 1},
		},
	})
	if err != nil {
		t.Fatalf("QueryCurrentPrices: %v", err)
	}

	wantLocations := []int16{5003, 4002}
	if !reflect.DeepEqual(repo.lookup.LocationIDs, wantLocations) {
		t.Fatalf("lookup locations = %#v, want %#v", repo.lookup.LocationIDs, wantLocations)
	}
	if len(repo.lookup.Entries) != 1 || repo.lookup.Entries[0].ItemKey != "T4_BAG" {
		t.Fatalf("lookup entries = %#v", repo.lookup.Entries)
	}
	if response.RequestedAt != requestedAt || response.Count != 2 {
		t.Fatalf("response metadata = %#v", response)
	}
	if response.Data[0].MarketKey != "brecilien" || response.Data[1].MarketKey != "fort_sterling" {
		t.Fatalf("market order/data = %#v", response.Data)
	}
}

func TestQueryCurrentPricesRejectsUnknownDisabledAndInvalidQuality(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  domain.PriceQueryRequest
	}{
		{
			name: "unknown market",
			req: domain.PriceQueryRequest{
				Server: domain.ServerWest, MarketKeys: []string{"unknown"},
				Entries: []domain.PriceQueryEntry{{ItemKey: "T4_BAG", Quality: 1}},
			},
		},
		{
			name: "disabled market",
			req: domain.PriceQueryRequest{
				Server: domain.ServerWest, MarketKeys: []string{"black_market"},
				Entries: []domain.PriceQueryEntry{{ItemKey: "T4_BAG", Quality: 1}},
			},
		},
		{
			name: "invalid quality",
			req: domain.PriceQueryRequest{
				Server: domain.ServerWest, MarketKeys: []string{"martlock"},
				Entries: []domain.PriceQueryEntry{{ItemKey: "T4_BAG", Quality: 6}},
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			service := NewMarketService(&priceQueryRepository{})
			_, err := service.QueryCurrentPrices(context.Background(), test.req)
			if !errors.Is(err, ErrInvalidPriceQuery) {
				t.Fatalf("error = %v, want ErrInvalidPriceQuery", err)
			}
		})
	}
}

func TestQueryCurrentPricesPropagatesRepositoryFailure(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("database unavailable")
	service := NewMarketService(&priceQueryRepository{err: wantErr})
	_, err := service.QueryCurrentPrices(context.Background(), domain.PriceQueryRequest{
		Server:     domain.ServerWest,
		MarketKeys: []string{"martlock"},
		Entries:    []domain.PriceQueryEntry{{ItemKey: "T4_BAG", Quality: 1}},
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

func TestQueryCurrentPricesReturnsEmptyArrayInsteadOfNil(t *testing.T) {
	t.Parallel()

	service := NewMarketService(&priceQueryRepository{prices: nil})
	response, err := service.QueryCurrentPrices(context.Background(), domain.PriceQueryRequest{
		Server:     domain.ServerWest,
		MarketKeys: []string{"martlock"},
		Entries:    []domain.PriceQueryEntry{{ItemKey: "T4_BAG", Quality: 1}},
	})
	if err != nil {
		t.Fatalf("QueryCurrentPrices: %v", err)
	}
	if response.Data == nil || len(response.Data) != 0 || response.Count != 0 {
		t.Fatalf("response = %#v, want non-nil empty data", response)
	}
}
