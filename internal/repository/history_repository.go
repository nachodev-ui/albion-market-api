package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
)

var errHistoryCopySourceNotPositioned = errors.New("raw history copy source is not positioned on a row")

var (
	marketHistoryIngestRawTable   = pgx.Identifier{"market_history_ingest_raw"}
	marketHistoryIngestRawColumns = []string{
		"request_id",
		"server",
		"observed_at",
		"location_id",
		"item_key",
		"quality",
		"bucket_at",
		"item_count",
		"average_unit_price",
	}
)

func (r *PgxMarketRepository) IngestHistory(ctx context.Context, req domain.IngestHistoryRequest) (domain.IngestHistoryResult, error) {
	serverID, err := mapServer(req.Server)
	if err != nil {
		return domain.IngestHistoryResult{}, err
	}

	requestHash, err := canonicalHistoryRequestHash(req)
	if err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("hash history ingest request: %w", err)
	}

	var requestUUID pgtype.UUID
	if err := requestUUID.Scan(req.RequestID); err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("invalid request_id: %w", err)
	}

	acceptedBuckets := countHistoryBuckets(req.Entries)

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("begin history tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const registerRequest = `
		insert into market_history_ingest_requests (
			request_id,
			request_sha256,
			server,
			accepted_entries,
			accepted_buckets,
			status
		)
		values ($1, $2, $3, $4, $5, 'processing')
		on conflict (request_id) do nothing
	`

	registerTag, err := tx.Exec(
		ctx,
		registerRequest,
		requestUUID,
		requestHash,
		serverID,
		len(req.Entries),
		acceptedBuckets,
	)
	if err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("register history ingest request: %w", err)
	}

	if registerTag.RowsAffected() == 0 {
		result, err := r.existingHistoryIngestResult(ctx, tx, requestUUID, requestHash)
		if err != nil {
			return domain.IngestHistoryResult{}, err
		}
		return result, nil
	}

	copiedRows, err := tx.CopyFrom(
		ctx,
		marketHistoryIngestRawTable,
		marketHistoryIngestRawColumns,
		newRawHistoryCopySource(requestUUID, serverID, req.Entries),
	)
	if err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("copy raw market history: %w", err)
	}
	if copiedRows != int64(acceptedBuckets) {
		return domain.IngestHistoryResult{}, fmt.Errorf(
			"copy raw market history: copied %d rows, expected %d",
			copiedRows,
			acceptedBuckets,
		)
	}

	// The raw table remains the durable audit trail. For each logical bucket we
	// retain the values from the newest capture (observed_at). Equal timestamps
	// may still correct values, so a later request can repair a source bucket
	// without creating a duplicate row in the hot read model.
	const upsertHistory = `
		with request_rows as materialized (
			select
				r.id,
				r.server,
				r.location_id,
				r.item_key,
				r.quality,
				r.bucket_at,
				r.item_count,
				r.average_unit_price,
				r.observed_at
			from market_history_ingest_raw r
			where r.request_id = $1
		), latest_buckets as (
			select distinct on (
				r.server,
				r.location_id,
				r.item_key,
				r.quality,
				r.bucket_at
			)
				r.server,
				r.location_id,
				r.item_key,
				r.quality,
				r.bucket_at,
				r.item_count,
				r.average_unit_price,
				r.observed_at
			from request_rows r
			order by
				r.server,
				r.location_id,
				r.item_key,
				r.quality,
				r.bucket_at,
				r.observed_at desc,
				r.id desc
		)
		insert into market_history_buckets (
			server,
			location_id,
			item_key,
			quality,
			bucket_at,
			item_count,
			average_unit_price,
			observed_at,
			updated_at
		)
		select
			server,
			location_id,
			item_key,
			quality,
			bucket_at,
			item_count,
			average_unit_price,
			observed_at,
			now()
		from latest_buckets
		on conflict (server, location_id, item_key, quality, bucket_at)
		do update
		set
			item_count = excluded.item_count,
			average_unit_price = excluded.average_unit_price,
			observed_at = excluded.observed_at,
			updated_at = now()
		where
			excluded.observed_at > market_history_buckets.observed_at
			or (
				excluded.observed_at = market_history_buckets.observed_at
				and (
					excluded.item_count is distinct from market_history_buckets.item_count
					or excluded.average_unit_price is distinct from market_history_buckets.average_unit_price
				)
			)
	`

	tag, err := tx.Exec(ctx, upsertHistory, requestUUID)
	if err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("upsert market history: %w", err)
	}

	const completeRequest = `
		update market_history_ingest_requests
		set
			status = 'completed',
			history_rows_touched = $2,
			completed_at = now()
		where request_id = $1
	`
	if _, err := tx.Exec(ctx, completeRequest, requestUUID, tag.RowsAffected()); err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("complete history ingest request: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("commit history tx: %w", err)
	}

	return domain.IngestHistoryResult{
		AcceptedEntries:    len(req.Entries),
		AcceptedBuckets:    acceptedBuckets,
		HistoryRowsTouched: tag.RowsAffected(),
		Duplicate:          false,
	}, nil
}

func (r *PgxMarketRepository) existingHistoryIngestResult(
	ctx context.Context,
	tx pgx.Tx,
	requestID pgtype.UUID,
	expectedHash []byte,
) (domain.IngestHistoryResult, error) {
	const queryExisting = `
		select
			accepted_entries,
			accepted_buckets,
			history_rows_touched,
			status,
			request_sha256
		from market_history_ingest_requests
		where request_id = $1
	`

	var acceptedEntries int
	var acceptedBuckets int
	var historyRowsTouched int64
	var status string
	var actualHash []byte

	if err := tx.QueryRow(ctx, queryExisting, requestID).Scan(
		&acceptedEntries,
		&acceptedBuckets,
		&historyRowsTouched,
		&status,
		&actualHash,
	); err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("query existing history ingest request: %w", err)
	}

	if !equalHashes(actualHash, expectedHash) {
		return domain.IngestHistoryResult{}, ErrRequestPayloadConflict
	}

	switch status {
	case "completed":
		return domain.IngestHistoryResult{
			AcceptedEntries:    acceptedEntries,
			AcceptedBuckets:    acceptedBuckets,
			HistoryRowsTouched: historyRowsTouched,
			Duplicate:          true,
		}, nil
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

// rawHistoryCopySource walks nested history entries without allocating a
// flattened [][]any buffer. pgx encodes each returned row before requesting
// the next one, allowing the values array to be reused safely.
type rawHistoryCopySource struct {
	entries       []domain.HistoryIngest
	entryIndex    int
	bucketIndex   int
	currentEntry  int
	currentBucket int
	positioned    bool
	values        [9]any
}

func newRawHistoryCopySource(
	requestID pgtype.UUID,
	serverID int16,
	entries []domain.HistoryIngest,
) *rawHistoryCopySource {
	source := &rawHistoryCopySource{entries: entries}
	source.values[0] = requestID
	source.values[1] = serverID
	return source
}

func (s *rawHistoryCopySource) Next() bool {
	for s.entryIndex < len(s.entries) {
		entry := s.entries[s.entryIndex]
		if s.bucketIndex < len(entry.History) {
			s.currentEntry = s.entryIndex
			s.currentBucket = s.bucketIndex
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

func (s *rawHistoryCopySource) Err() error {
	return nil
}

func (r *PgxMarketRepository) QueryMarketHistory(
	ctx context.Context,
	lookup domain.MarketHistoryLookup,
) ([]domain.MarketHistorySeries, error) {
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
		select
			server,
			location_id,
			item_key,
			quality,
			bucket_at,
			item_count,
			average_unit_price
		from market_history_buckets
		where server = $1
		  and location_id = any($2)
		  and (item_key, quality) in (
			select * from unnest($3::text[], $4::smallint[])
		  )
		  and bucket_at >= $5
		  and bucket_at < $6
		order by location_id, item_key, quality, bucket_at
	`

	rows, err := r.db.Query(
		ctx,
		query,
		serverID,
		lookup.LocationIDs,
		itemKeys,
		qualities,
		lookup.RangeStart,
		lookup.RangeEndExclusive,
	)
	if err != nil {
		return nil, fmt.Errorf("query market history: %w", err)
	}
	defer rows.Close()

	histories := make([]domain.MarketHistorySeries, 0)
	for rows.Next() {
		var serverValue int16
		var locationID int16
		var itemKey string
		var quality int16
		var point domain.MarketHistoryPoint

		if err := rows.Scan(
			&serverValue,
			&locationID,
			&itemKey,
			&quality,
			&point.Timestamp,
			&point.ItemCount,
			&point.AverageUnitPrice,
		); err != nil {
			return nil, fmt.Errorf("scan market history: %w", err)
		}

		seriesIndex := len(histories) - 1
		if seriesIndex < 0 ||
			histories[seriesIndex].LocationID != locationID ||
			histories[seriesIndex].ItemKey != itemKey ||
			histories[seriesIndex].Quality != quality {
			histories = append(histories, domain.MarketHistorySeries{
				Server:     unmapServer(serverValue),
				LocationID: locationID,
				ItemKey:    itemKey,
				Quality:    quality,
				History:    make([]domain.MarketHistoryPoint, 0, 28),
			})
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
