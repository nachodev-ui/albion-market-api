package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

const upsertMarketHistorySQL = `
	with request_rows as materialized (
		select r.id, r.server, r.location_id, r.item_key, r.quality, r.bucket_at,
		       r.item_count, r.average_unit_price, r.observed_at
		from market_history_ingest_raw r where r.request_id = $1
	), latest_buckets as (
		select distinct on (r.server, r.location_id, r.item_key, r.quality, r.bucket_at)
		       r.server, r.location_id, r.item_key, r.quality, r.bucket_at,
		       r.item_count, r.average_unit_price, r.observed_at
		from request_rows r
		order by r.server, r.location_id, r.item_key, r.quality, r.bucket_at,
		         r.observed_at desc, r.id desc
	)
	insert into market_history_buckets (
		server, location_id, item_key, quality, bucket_at, item_count,
		average_unit_price, observed_at, updated_at
	)
	select server, location_id, item_key, quality, bucket_at, item_count,
	       average_unit_price, observed_at, now()
	from latest_buckets
	on conflict (server, location_id, item_key, quality, bucket_at)
	do update set
		item_count = excluded.item_count,
		average_unit_price = excluded.average_unit_price,
		observed_at = excluded.observed_at,
		updated_at = now()
	where excluded.observed_at > market_history_buckets.observed_at
	   or (excluded.observed_at = market_history_buckets.observed_at and (
	       excluded.item_count is distinct from market_history_buckets.item_count
	       or excluded.average_unit_price is distinct from market_history_buckets.average_unit_price))
`

func (r *PgxMarketRepository) IngestHistory(ctx context.Context, req domain.IngestHistoryRequest) (result domain.IngestHistoryResult, err error) {
	started := time.Now()
	defer func() { r.observeDatabase("ingest_history", started, err) }()
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
	transactionStarted := time.Now()
	defer func() { r.observeDatabase("transaction_history", transactionStarted, err) }()
	defer func() { observability.RecordIngestTransaction(ctx, time.Since(transactionStarted)) }()
	defer func() { _ = tx.Rollback(ctx) }()

	registerTag, err := tx.Exec(ctx, registerHistoryIngestRequestSQL, requestUUID, requestHash, serverID, len(req.Entries), acceptedBuckets)
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

	copyStarted := time.Now()
	copiedRows, err := tx.CopyFrom(ctx, marketHistoryIngestRawTable, marketHistoryIngestRawColumns, newRawHistoryCopySource(requestUUID, serverID, req.Entries))
	if err == nil && copiedRows != int64(acceptedBuckets) {
		err = fmt.Errorf("copied %d rows, expected %d", copiedRows, acceptedBuckets)
	}
	r.observeDatabase("copy_raw_history", copyStarted, err)
	if err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("copy raw market history: %w", err)
	}

	upsertStarted := time.Now()
	tag, err := tx.Exec(ctx, upsertMarketHistorySQL, requestUUID)
	r.observeDatabase("upsert_market_history", upsertStarted, err)
	if err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("upsert market history: %w", err)
	}
	if _, err := tx.Exec(ctx, completeHistoryIngestRequestSQL, requestUUID, tag.RowsAffected()); err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("complete history ingest request: %w", err)
	}

	commitStarted := time.Now()
	err = tx.Commit(ctx)
	observability.RecordIngestCommit(ctx, time.Since(commitStarted))
	if err != nil {
		return domain.IngestHistoryResult{}, fmt.Errorf("commit history tx: %w", err)
	}
	return domain.IngestHistoryResult{
		AcceptedEntries:    len(req.Entries),
		AcceptedBuckets:    acceptedBuckets,
		HistoryRowsTouched: tag.RowsAffected(),
	}, nil
}
