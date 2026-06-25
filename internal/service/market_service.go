package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/repository"
)

var (
	ErrIngestRequestAlreadyProcessing = errors.New("request_id is already processing")
	ErrIngestRequestPayloadConflict   = errors.New("request_id was already used with a different payload")
)

type MarketService struct {
	repo repository.MarketRepository
}

func NewMarketService(repo repository.MarketRepository) *MarketService {
	return &MarketService{repo: repo}
}

func (s *MarketService) Ping(ctx context.Context) error {
	return s.repo.Ping(ctx)
}

func (s *MarketService) IngestPrices(ctx context.Context, req domain.IngestPricesRequest) (domain.IngestPricesResponse, error) {
	if req.RequestID == "" {
		return domain.IngestPricesResponse{}, fmt.Errorf("request_id is required")
	}
	if req.Server == "" {
		return domain.IngestPricesResponse{}, fmt.Errorf("server is required")
	}
	if len(req.Entries) == 0 {
		return domain.IngestPricesResponse{}, fmt.Errorf("entries is required")
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
	}, nil
}

func (s *MarketService) QueryCurrentPrices(ctx context.Context, req domain.PriceQueryRequest) (domain.PriceQueryResponse, error) {
	if req.Server == "" {
		return domain.PriceQueryResponse{}, fmt.Errorf("server is required")
	}

	prices, err := s.repo.QueryCurrentPrices(ctx, req)
	if err != nil {
		return domain.PriceQueryResponse{}, err
	}

	return domain.PriceQueryResponse{Prices: prices}, nil
}
