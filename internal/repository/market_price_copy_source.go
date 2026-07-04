package repository

import (
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
)

type rawPriceCopySource struct {
	entries []domain.PriceIngest
	index   int
	values  [10]any
}

func newRawPriceCopySource(requestID pgtype.UUID, serverID int16, entries []domain.PriceIngest) *rawPriceCopySource {
	source := &rawPriceCopySource{entries: entries}
	source.values[0], source.values[1] = requestID, serverID
	return source
}

func (s *rawPriceCopySource) Next() bool {
	if s.index >= len(s.entries) {
		return false
	}
	s.index++
	return true
}

func (s *rawPriceCopySource) Values() ([]any, error) {
	if s.index == 0 || s.index > len(s.entries) {
		return nil, errCopySourceNotPositioned
	}
	entry := s.entries[s.index-1]
	s.values[2] = entry.ObservedAt
	s.values[3] = entry.LocationID
	s.values[4] = entry.ItemKey
	s.values[5] = entry.Quality
	s.values[6] = entry.SellPriceMin
	s.values[7] = entry.SellPriceMinAt
	s.values[8] = entry.BuyPriceMax
	s.values[9] = entry.BuyPriceMaxAt
	return s.values[:], nil
}

func (s *rawPriceCopySource) Err() error {
	return nil
}
