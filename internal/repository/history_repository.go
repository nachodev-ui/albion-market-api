package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
)

var errHistoryCopySourceNotPositioned = errors.New("raw history copy source is not positioned on a row")

var (
	marketHistoryIngestRawTable   = pgx.Identifier{"market_history_ingest_raw"}
	marketHistoryIngestRawColumns = []string{"request_id", "server", "observed_at", "location_id", "item_key", "quality", "bucket_at", "item_count", "average_unit_price"}
)

func (r *PgxMarketRepository) existingHistoryIngestResult(ctx context.Context, tx pgx.Tx, requestID pgtype.UUID, expectedHash []byte) (domain.IngestHistoryResult, error) {
	const query = `select accepted_entries, accepted_buckets, history_rows_touched, status, request_sha256 from market_history_ingest_requests where request_id = $1`
	var acceptedEntries, acceptedBuckets int
	var historyRowsTouched int64
	var status string
	var actualHash []byte
	if err := tx.QueryRow(ctx, query, requestID).Scan(&acceptedEntries, &acceptedBuckets, &historyRowsTouched, &status, &actualHash); err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("query existing history ingest request: %w", err)
	}
	if !equalHashes(actualHash, expectedHash) {
		return domain.IngestHistoryResult{}, ErrRequestPayloadConflict
	}
	switch status {
	case "completed":
		return domain.IngestHistoryResult{AcceptedEntries: acceptedEntries, AcceptedBuckets: acceptedBuckets, HistoryRowsTouched: historyRowsTouched, Duplicate: true}, nil
	case "processing":
		return domain.IngestHistoryResult{}, ErrRequestAlreadyProcessing
	default:
		return domain.IngestHistoryResult{}, fmt.Errorf("unsupported history ingest request status %q", status)
	}
}

func canonicalHistoryRequestHash(req domain.IngestHistoryRequest) ([]byte, error) {
	encoded, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

func countHistoryBuckets(entries []domain.HistoryIngest) int {
	total := 0
	for _, entry := range entries {
		total += len(entry.History)
	}
	return total
}

type rawHistoryCopySource struct {
	entries                             []domain.HistoryIngest
	entryIndex, bucketIndex             int
	currentEntry, currentBucket         int
	positioned                          bool
	values                              [9]any
}

func newRawHistoryCopySource(requestID pgtype.UUID, serverID int16, entries []domain.HistoryIngest) *rawHistoryCopySource {
	source := &rawHistoryCopySource{entries: entries}
	source.values[0], source.values[1] = requestID, serverID
	return source
}

func (s *rawHistoryCopySource) Next() bool {
	for s.entryIndex < len(s.entries) {
		entry := s.entries[s.entryIndex]
		if s.bucketIndex < len(entry.History) {
			s.currentEntry, s.currentBucket = s.entryIndex, s.bucketIndex
			s.bucketIndex++
			s.positioned = true
			return true
		}
		s.entryIndex++
		s.bucketIndex = 0
	}
	s.positioned = false
	return false
}

func (s *rawHistoryCopySource) Values() ([]any, error) {
	if !s.positioned || s.currentEntry >= len(s.entries) {
		return nil, errHistoryCopySourceNotPositioned
	}
	entry := s.entries[s.currentEntry]
	if s.currentBucket >= len(entry.History) {
		return nil, errHistoryCopySourceNotPositioned
	}
	bucket := entry.History[s.currentBucket]
	s.values[2] = entry.ObservedAt
	s.values[3] = entry.LocationID
	s.values[4] = entry.ItemKey
	s.values[5] = entry.Quality
	s.values[6] = bucket.Timestamp
	s.values[7] = bucket.ItemCount
	s.values[8] = bucket.AverageUnitPrice
	return s.values[:], nil
}

func (s *rawHistoryCopySource) Err() error { return nil }

func (r *PgxMarketRepository) QueryMarketHistory(ctx context.Context, lookup domain.MarketHistoryLookup) (histories []domain.MarketHistorySeries, err error) {
	started := time.Now()
	defer func() { r.observeDatabase("query_market_history", started, err) }()
	serverID, err := mapServer(lookup.Server)
	if err != nil {
		return nil, err
	}
	if len(lookup.LocationIDs) == 0 || len(lookup.Entries) == 0 {
		return []domain.MarketHistorySeries{}, nil
	}
	itemKeys := make([]string, 0, len(lookup.Entries))
	qualities := make([]int16, 0, len(lookup.Entries))
	for _, entry := range lookup.Entries {
		itemKeys = append(itemKeys, entry.ItemKey)
		qualities = append(qualities, entry.Quality)
	}
	const query = `
		select server, location_id, item_key, quality, bucket_at, item_count, average_unit_price
		from market_history_buckets
		where server = $1 and location_id = any($2)
		  and (item_key, quality) in (select * from unnest($3::text[], $4::smallint[]))
		  and bucket_at >= $5 and bucket_at < $6
		order by location_id, item_key, quality, bucket_at`
	rows, err := r.db.Query(ctx, query, serverID, lookup.LocationIDs, itemKeys, qualities, lookup.RangeStart, lookup.RangeEndExclusive)
	if err != nil {
		return nil, fmt.Errorf("query market history: %w", err)
	}
	defer rows.Close()
	histories = make([]domain.MarketHistorySeries, 0)
	for rows.Next() {
		var serverValue, locationID, quality int16
		var itemKey string
		var point domain.MarketHistoryPoint
		if err := rows.Scan(&serverValue, &locationID, &itemKey, &quality, &point.Timestamp, &point.ItemCount, &point.AverageUnitPrice); err != nil {
			return nil, fmt.Errorf("scan market history: %w", err)
		}
		seriesIndex := len(histories) - 1
		if seriesIndex < 0 || histories[seriesIndex].LocationID != locationID || histories[seriesIndex].ItemKey != itemKey || histories[seriesIndex].Quality != quality {
			histories = append(histories, domain.MarketHistorySeries{Server: unmapServer(serverValue), LocationID: locationID, ItemKey: itemKey, Quality: quality, History: make([]domain.MarketHistoryPoint, 0, 28)})
			seriesIndex = len(histories) - 1
		}
		point.Timestamp = point.Timestamp.UTC()
		histories[seriesIndex].History = append(histories[seriesIndex].History, point)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows market history: %w", err)
	}
	return histories, nil
}
