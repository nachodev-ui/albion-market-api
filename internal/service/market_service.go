package service

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/marketcatalog"
	"github.com/nachodev-ui/albion-market-api/internal/repository"
)

var (
	ErrIngestRequestAlreadyProcessing = errors.New("request_id is already processing")
	ErrIngestRequestPayloadConflict   = errors.New("request_id was already used with a different payload")
	ErrInvalidIngestRequest           = errors.New("invalid ingest request")
	ErrInvalidPriceQuery              = errors.New("invalid price query")
)

const (
	maxPriceQueryMarkets = 32
	maxPriceQueryEntries = 2000
)

type MarketService struct {
	repo    repository.MarketRepository
	catalog *marketcatalog.Catalog
	now     func() time.Time
}

func NewMarketService(repo repository.MarketRepository, catalogs ...*marketcatalog.Catalog) *MarketService {
	catalog := marketcatalog.NewDefault()
	if len(catalogs) > 0 && catalogs[0] != nil {
		catalog = catalogs[0]
	}
	return &MarketService{
		repo:    repo,
		catalog: catalog,
		now:     time.Now,
	}
}

func (s *MarketService) Ping(ctx context.Context) error {
	return s.repo.Ping(ctx)
}

func (s *MarketService) IngestPrices(ctx context.Context, req domain.IngestPricesRequest) (domain.IngestPricesResponse, error) {
	if req.RequestID == "" {
		return domain.IngestPricesResponse{}, fmt.Errorf("%w: request_id is required", ErrInvalidIngestRequest)
	}
	if req.Server == "" {
		return domain.IngestPricesResponse{}, fmt.Errorf("%w: server is required", ErrInvalidIngestRequest)
	}
	if len(req.Entries) == 0 {
		return domain.IngestPricesResponse{}, fmt.Errorf("%w: entries is required", ErrInvalidIngestRequest)
	}
	if err := validateServer(req.Server); err != nil {
		return domain.IngestPricesResponse{}, fmt.Errorf("%w: %v", ErrInvalidIngestRequest, err)
	}

	result, err := s.repo.IngestPrices(ctx, req)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrRequestAlreadyProcessing):
			return domain.IngestPricesResponse{}, ErrIngestRequestAlreadyProcessing
		case errors.Is(err, repository.ErrRequestPayloadConflict):
			return domain.IngestPricesResponse{}, ErrIngestRequestPayloadConflict
		default:
			return domain.IngestPricesResponse{}, err
		}
	}

	return domain.IngestPricesResponse{
		RequestID:          req.RequestID,
		Accepted:           result.Accepted,
		CurrentRowsTouched: result.CurrentRowsTouched,
		Duplicate:          result.Duplicate,
		PersistenceTiming:  result.PersistenceTiming,
	}, nil
}

func (s *MarketService) Markets(includeDisabled bool) domain.MarketCatalogResponse {
	markets := s.catalog.List(includeDisabled)
	return domain.MarketCatalogResponse{
		SchemaVersion: marketcatalog.SchemaVersion,
		Count:         len(markets),
		Data:          markets,
	}
}

type normalizedPriceQuery struct {
	lookup         domain.CurrentPriceLookup
	marketOrder    map[string]int
	keysByLocation map[int16]string
}

func (s *MarketService) QueryCurrentPrices(ctx context.Context, req domain.PriceQueryRequest) (domain.PriceQueryResponse, error) {
	normalized, err := s.normalizePriceQuery(req)
	if err != nil {
		return domain.PriceQueryResponse{}, err
	}

	prices, err := s.repo.QueryCurrentPrices(ctx, normalized.lookup)
	if err != nil {
		return domain.PriceQueryResponse{}, err
	}
	if prices == nil {
		prices = make([]domain.CurrentPrice, 0)
	}

	for index := range prices {
		marketKey, ok := normalized.keysByLocation[prices[index].LocationID]
		if !ok {
			return domain.PriceQueryResponse{}, fmt.Errorf(
				"repository returned unmapped market location %d",
				prices[index].LocationID,
			)
		}
		prices[index].MarketKey = marketKey
	}

	sort.SliceStable(prices, func(i, j int) bool {
		leftRank := normalized.marketOrder[prices[i].MarketKey]
		rightRank := normalized.marketOrder[prices[j].MarketKey]
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if prices[i].ItemKey != prices[j].ItemKey {
			return prices[i].ItemKey < prices[j].ItemKey
		}
		return prices[i].Quality < prices[j].Quality
	})

	return domain.PriceQueryResponse{
		RequestedAt: s.now().UTC(),
		Count:       len(prices),
		Data:        prices,
	}, nil
}

func (s *MarketService) normalizePriceQuery(req domain.PriceQueryRequest) (normalizedPriceQuery, error) {
	if err := validateServer(req.Server); err != nil {
		return normalizedPriceQuery{}, fmt.Errorf("%w: %v", ErrInvalidPriceQuery, err)
	}
	if len(req.MarketKeys) == 0 {
		return normalizedPriceQuery{}, fmt.Errorf("%w: marketKeys is required", ErrInvalidPriceQuery)
	}
	if len(req.MarketKeys) > maxPriceQueryMarkets {
		return normalizedPriceQuery{}, fmt.Errorf(
			"%w: marketKeys cannot exceed %d entries",
			ErrInvalidPriceQuery,
			maxPriceQueryMarkets,
		)
	}
	if len(req.Entries) == 0 {
		return normalizedPriceQuery{}, fmt.Errorf("%w: entries is required", ErrInvalidPriceQuery)
	}
	if len(req.Entries) > maxPriceQueryEntries {
		return normalizedPriceQuery{}, fmt.Errorf(
			"%w: entries cannot exceed %d items",
			ErrInvalidPriceQuery,
			maxPriceQueryEntries,
		)
	}

	locationIDs, keysByLocation, err := s.catalog.ResolveEnabled(req.MarketKeys)
	if err != nil {
		return normalizedPriceQuery{}, fmt.Errorf("%w: %v", ErrInvalidPriceQuery, err)
	}

	normalizedKeys := make([]string, 0, len(locationIDs))
	marketOrder := make(map[string]int, len(locationIDs))
	seenMarkets := make(map[string]struct{}, len(locationIDs))
	for _, rawKey := range req.MarketKeys {
		key := marketcatalog.NormalizeKey(rawKey)
		if _, exists := seenMarkets[key]; exists {
			continue
		}
		seenMarkets[key] = struct{}{}
		marketOrder[key] = len(normalizedKeys)
		normalizedKeys = append(normalizedKeys, key)
	}

	normalizedEntries := make([]domain.PriceQueryEntry, 0, len(req.Entries))
	seenEntries := make(map[string]struct{}, len(req.Entries))
	for index, entry := range req.Entries {
		itemKey := strings.TrimSpace(entry.ItemKey)
		if itemKey == "" {
			return normalizedPriceQuery{}, fmt.Errorf(
				"%w: entries[%d].itemIdentifier is required",
				ErrInvalidPriceQuery,
				index,
			)
		}
		if entry.Quality < 1 || entry.Quality > 5 {
			return normalizedPriceQuery{}, fmt.Errorf(
				"%w: entries[%d].quality must be between 1 and 5",
				ErrInvalidPriceQuery,
				index,
			)
		}

		dedupeKey := fmt.Sprintf("%s\x00%d", itemKey, entry.Quality)
		if _, exists := seenEntries[dedupeKey]; exists {
			continue
		}
		seenEntries[dedupeKey] = struct{}{}
		normalizedEntries = append(normalizedEntries, domain.PriceQueryEntry{
			ItemKey: itemKey,
			Quality: entry.Quality,
		})
	}

	return normalizedPriceQuery{
		lookup: domain.CurrentPriceLookup{
			Server:      req.Server,
			LocationIDs: locationIDs,
			Entries:     normalizedEntries,
		},
		marketOrder:    marketOrder,
		keysByLocation: keysByLocation,
	}, nil
}

func validateServer(server domain.Server) error {
	switch server {
	case domain.ServerWest, domain.ServerEast, domain.ServerEurope:
		return nil
	case "":
		return errors.New("server is required")
	default:
		return fmt.Errorf("unsupported server %q", server)
	}
}
