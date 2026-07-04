package service

import (
	"context"
	"encoding/hex"
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
	ErrInvalidHistoryIngestRequest = errors.New("invalid history ingest request")
	ErrInvalidHistoryQuery         = errors.New("invalid history query")
)

const (
	maxHistoryIngestEntries = 2000
	maxHistoryIngestBuckets = 100000
	maxHistoryQueryMarkets  = 32
	maxHistoryQueryEntries  = 2000
	maxHistoryQueryDays     = 366
	utcDateLayout           = "2006-01-02"
)

func (s *MarketService) IngestHistory(
	ctx context.Context,
	req domain.IngestHistoryRequest,
) (domain.IngestHistoryResponse, error) {
	normalized, err := normalizeHistoryIngestRequest(req)
	if err != nil {
		return domain.IngestHistoryResponse{}, err
	}

	result, err := s.repo.IngestHistory(ctx, normalized)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrRequestAlreadyProcessing):
			return domain.IngestHistoryResponse{}, ErrIngestRequestAlreadyProcessing
		case errors.Is(err, repository.ErrRequestPayloadConflict):
			return domain.IngestHistoryResponse{}, ErrIngestRequestPayloadConflict
		default:
			return domain.IngestHistoryResponse{}, err
		}
	}

	return domain.IngestHistoryResponse{
		RequestID:          normalized.RequestID,
		AcceptedEntries:    result.AcceptedEntries,
		AcceptedBuckets:    result.AcceptedBuckets,
		HistoryRowsTouched: result.HistoryRowsTouched,
		Duplicate:          result.Duplicate,
		PersistenceTiming:  result.PersistenceTiming,
	}, nil
}

func normalizeHistoryIngestRequest(
	req domain.IngestHistoryRequest,
) (domain.IngestHistoryRequest, error) {
	req.RequestID = strings.TrimSpace(req.RequestID)
	if req.RequestID == "" {
		return domain.IngestHistoryRequest{}, fmt.Errorf(
			"%w: request_id is required",
			ErrInvalidHistoryIngestRequest,
		)
	}
	if !isUUID(req.RequestID) {
		return domain.IngestHistoryRequest{}, fmt.Errorf(
			"%w: request_id must be a UUID",
			ErrInvalidHistoryIngestRequest,
		)
	}
	if err := validateServer(req.Server); err != nil {
		return domain.IngestHistoryRequest{}, fmt.Errorf(
			"%w: %v",
			ErrInvalidHistoryIngestRequest,
			err,
		)
	}
	if len(req.Entries) == 0 {
		return domain.IngestHistoryRequest{}, fmt.Errorf(
			"%w: entries is required",
			ErrInvalidHistoryIngestRequest,
		)
	}
	if len(req.Entries) > maxHistoryIngestEntries {
		return domain.IngestHistoryRequest{}, fmt.Errorf(
			"%w: entries cannot exceed %d items",
			ErrInvalidHistoryIngestRequest,
			maxHistoryIngestEntries,
		)
	}

	normalizedEntries := make([]domain.HistoryIngest, len(req.Entries))
	totalBuckets := 0
	for entryIndex, entry := range req.Entries {
		entry.ItemKey = strings.TrimSpace(entry.ItemKey)
		entry.ObservedAt = entry.ObservedAt.UTC()

		if entry.ObservedAt.IsZero() {
			return domain.IngestHistoryRequest{}, fmt.Errorf(
				"%w: entries[%d].observed_at is required",
				ErrInvalidHistoryIngestRequest,
				entryIndex,
			)
		}
		if entry.LocationID <= 0 {
			return domain.IngestHistoryRequest{}, fmt.Errorf(
				"%w: entries[%d].location_id must be greater than 0",
				ErrInvalidHistoryIngestRequest,
				entryIndex,
			)
		}
		if entry.ItemKey == "" {
			return domain.IngestHistoryRequest{}, fmt.Errorf(
				"%w: entries[%d].item_key is required",
				ErrInvalidHistoryIngestRequest,
				entryIndex,
			)
		}
		if entry.Quality < 1 || entry.Quality > 5 {
			return domain.IngestHistoryRequest{}, fmt.Errorf(
				"%w: entries[%d].quality must be between 1 and 5",
				ErrInvalidHistoryIngestRequest,
				entryIndex,
			)
		}
		if len(entry.History) == 0 {
			return domain.IngestHistoryRequest{}, fmt.Errorf(
				"%w: entries[%d].history is required",
				ErrInvalidHistoryIngestRequest,
				entryIndex,
			)
		}

		totalBuckets += len(entry.History)
		if totalBuckets > maxHistoryIngestBuckets {
			return domain.IngestHistoryRequest{}, fmt.Errorf(
				"%w: total history buckets cannot exceed %d",
				ErrInvalidHistoryIngestRequest,
				maxHistoryIngestBuckets,
			)
		}

		normalizedHistory := make([]domain.HistoryBucketIngest, len(entry.History))
		for bucketIndex, bucket := range entry.History {
			bucket.Timestamp = bucket.Timestamp.UTC()
			if bucket.Timestamp.IsZero() {
				return domain.IngestHistoryRequest{}, fmt.Errorf(
					"%w: entries[%d].history[%d].timestamp is required",
					ErrInvalidHistoryIngestRequest,
					entryIndex,
					bucketIndex,
				)
			}
			if bucket.ItemCount < 0 {
				return domain.IngestHistoryRequest{}, fmt.Errorf(
					"%w: entries[%d].history[%d].item_count cannot be negative",
					ErrInvalidHistoryIngestRequest,
					entryIndex,
					bucketIndex,
				)
			}
			if bucket.AverageUnitPrice != nil {
				switch {
				case *bucket.AverageUnitPrice < 0:
					return domain.IngestHistoryRequest{}, fmt.Errorf(
						"%w: entries[%d].history[%d].average_unit_price cannot be negative",
						ErrInvalidHistoryIngestRequest,
						entryIndex,
						bucketIndex,
					)
				case *bucket.AverageUnitPrice == 0:
					// AODP uses zero when a bucket has volume metadata but no
					// meaningful average price. Persist that as NULL so callers can
					// distinguish missing price data from an actual silver value.
					bucket.AverageUnitPrice = nil
				}
			}
			normalizedHistory[bucketIndex] = bucket
		}

		entry.History = normalizedHistory
		normalizedEntries[entryIndex] = entry
	}

	req.Entries = normalizedEntries
	return req, nil
}

type normalizedHistoryQuery struct {
	lookup         domain.MarketHistoryLookup
	marketOrder    map[string]int
	keysByLocation map[int16]string
	rangeStart     string
	rangeEnd       string
}

func (s *MarketService) QueryMarketHistory(
	ctx context.Context,
	req domain.HistoryQueryRequest,
) (domain.HistoryQueryResponse, error) {
	normalized, err := s.normalizeHistoryQuery(req)
	if err != nil {
		return domain.HistoryQueryResponse{}, err
	}

	histories, err := s.repo.QueryMarketHistory(ctx, normalized.lookup)
	if err != nil {
		return domain.HistoryQueryResponse{}, err
	}
	if histories == nil {
		histories = make([]domain.MarketHistorySeries, 0)
	}

	bucketCount := 0
	for index := range histories {
		marketKey, ok := normalized.keysByLocation[histories[index].LocationID]
		if !ok {
			return domain.HistoryQueryResponse{}, fmt.Errorf(
				"repository returned unmapped market location %d",
				histories[index].LocationID,
			)
		}
		histories[index].MarketKey = marketKey
		if histories[index].History == nil {
			histories[index].History = make([]domain.MarketHistoryPoint, 0)
		}
		for pointIndex := range histories[index].History {
			histories[index].History[pointIndex].Timestamp = histories[index].History[pointIndex].Timestamp.UTC()
		}
		bucketCount += len(histories[index].History)
	}

	sort.SliceStable(histories, func(i, j int) bool {
		leftRank := normalized.marketOrder[histories[i].MarketKey]
		rightRank := normalized.marketOrder[histories[j].MarketKey]
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		if histories[i].ItemKey != histories[j].ItemKey {
			return histories[i].ItemKey < histories[j].ItemKey
		}
		return histories[i].Quality < histories[j].Quality
	})

	return domain.HistoryQueryResponse{
		RequestedAt: s.now().UTC(),
		RangeStart:  normalized.rangeStart,
		RangeEnd:    normalized.rangeEnd,
		Count:       len(histories),
		BucketCount: bucketCount,
		Data:        histories,
	}, nil
}

func (s *MarketService) normalizeHistoryQuery(
	req domain.HistoryQueryRequest,
) (normalizedHistoryQuery, error) {
	if err := validateServer(req.Server); err != nil {
		return normalizedHistoryQuery{}, fmt.Errorf("%w: %v", ErrInvalidHistoryQuery, err)
	}
	if len(req.MarketKeys) == 0 {
		return normalizedHistoryQuery{}, fmt.Errorf(
			"%w: marketKeys is required",
			ErrInvalidHistoryQuery,
		)
	}
	if len(req.MarketKeys) > maxHistoryQueryMarkets {
		return normalizedHistoryQuery{}, fmt.Errorf(
			"%w: marketKeys cannot exceed %d entries",
			ErrInvalidHistoryQuery,
			maxHistoryQueryMarkets,
		)
	}
	if len(req.Entries) == 0 {
		return normalizedHistoryQuery{}, fmt.Errorf(
			"%w: entries is required",
			ErrInvalidHistoryQuery,
		)
	}
	if len(req.Entries) > maxHistoryQueryEntries {
		return normalizedHistoryQuery{}, fmt.Errorf(
			"%w: entries cannot exceed %d items",
			ErrInvalidHistoryQuery,
			maxHistoryQueryEntries,
		)
	}

	locationIDs, keysByLocation, err := s.catalog.ResolveEnabled(req.MarketKeys)
	if err != nil {
		return normalizedHistoryQuery{}, fmt.Errorf("%w: %v", ErrInvalidHistoryQuery, err)
	}

	marketOrder := make(map[string]int, len(locationIDs))
	seenMarkets := make(map[string]struct{}, len(locationIDs))
	for _, rawKey := range req.MarketKeys {
		key := marketcatalog.NormalizeKey(rawKey)
		if _, exists := seenMarkets[key]; exists {
			continue
		}
		seenMarkets[key] = struct{}{}
		marketOrder[key] = len(marketOrder)
	}

	normalizedEntries := make([]domain.HistoryQueryEntry, 0, len(req.Entries))
	seenEntries := make(map[string]struct{}, len(req.Entries))
	for index, entry := range req.Entries {
		itemKey := strings.TrimSpace(entry.ItemKey)
		if itemKey == "" {
			return normalizedHistoryQuery{}, fmt.Errorf(
				"%w: entries[%d].itemIdentifier is required",
				ErrInvalidHistoryQuery,
				index,
			)
		}
		if entry.Quality < 1 || entry.Quality > 5 {
			return normalizedHistoryQuery{}, fmt.Errorf(
				"%w: entries[%d].quality must be between 1 and 5",
				ErrInvalidHistoryQuery,
				index,
			)
		}

		dedupeKey := fmt.Sprintf("%s\x00%d", itemKey, entry.Quality)
		if _, exists := seenEntries[dedupeKey]; exists {
			continue
		}
		seenEntries[dedupeKey] = struct{}{}
		normalizedEntries = append(normalizedEntries, domain.HistoryQueryEntry{
			ItemKey: itemKey,
			Quality: entry.Quality,
		})
	}

	rangeStart, rangeEndExclusive, rangeStartString, rangeEndString, err := parseHistoryRange(
		req.RangeStart,
		req.RangeEnd,
	)
	if err != nil {
		return normalizedHistoryQuery{}, err
	}

	return normalizedHistoryQuery{
		lookup: domain.MarketHistoryLookup{
			Server:            req.Server,
			LocationIDs:       locationIDs,
			Entries:           normalizedEntries,
			RangeStart:        rangeStart,
			RangeEndExclusive: rangeEndExclusive,
		},
		marketOrder:    marketOrder,
		keysByLocation: keysByLocation,
		rangeStart:     rangeStartString,
		rangeEnd:       rangeEndString,
	}, nil
}

func parseHistoryRange(
	rawStart string,
	rawEnd string,
) (time.Time, time.Time, string, string, error) {
	startString := strings.TrimSpace(rawStart)
	endString := strings.TrimSpace(rawEnd)
	if startString == "" {
		return time.Time{}, time.Time{}, "", "", fmt.Errorf(
			"%w: rangeStart is required",
			ErrInvalidHistoryQuery,
		)
	}
	if endString == "" {
		return time.Time{}, time.Time{}, "", "", fmt.Errorf(
			"%w: rangeEnd is required",
			ErrInvalidHistoryQuery,
		)
	}

	start, err := time.Parse(utcDateLayout, startString)
	if err != nil {
		return time.Time{}, time.Time{}, "", "", fmt.Errorf(
			"%w: rangeStart must use YYYY-MM-DD",
			ErrInvalidHistoryQuery,
		)
	}
	end, err := time.Parse(utcDateLayout, endString)
	if err != nil {
		return time.Time{}, time.Time{}, "", "", fmt.Errorf(
			"%w: rangeEnd must use YYYY-MM-DD",
			ErrInvalidHistoryQuery,
		)
	}
	if end.Before(start) {
		return time.Time{}, time.Time{}, "", "", fmt.Errorf(
			"%w: rangeEnd cannot be before rangeStart",
			ErrInvalidHistoryQuery,
		)
	}

	days := int(end.Sub(start)/(24*time.Hour)) + 1
	if days > maxHistoryQueryDays {
		return time.Time{}, time.Time{}, "", "", fmt.Errorf(
			"%w: date range cannot exceed %d days",
			ErrInvalidHistoryQuery,
			maxHistoryQueryDays,
		)
	}

	return start.UTC(), end.AddDate(0, 0, 1).UTC(), startString, endString, nil
}

func isUUID(value string) bool {
	compact := value
	switch len(value) {
	case 36:
		if value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
			return false
		}
		compact = value[:8] + value[9:13] + value[14:18] + value[19:23] + value[24:]
	case 32:
	default:
		return false
	}

	decoded := make([]byte, 16)
	_, err := hex.Decode(decoded, []byte(compact))
	return err == nil
}
