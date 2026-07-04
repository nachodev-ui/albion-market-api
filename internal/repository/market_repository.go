package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

type MarketRepository interface {
	IngestPrices(context.Context, domain.IngestPricesRequest) (domain.IngestPricesResult, error)
	IngestHistory(context.Context, domain.IngestHistoryRequest) (domain.IngestHistoryResult, error)
	QueryCurrentPrices(context.Context, domain.CurrentPriceLookup) ([]domain.CurrentPrice, error)
	QueryMarketHistory(context.Context, domain.MarketHistoryLookup) ([]domain.MarketHistorySeries, error)
	Ping(context.Context) error
}

type PgxMarketRepository struct {
	db      *pgxpool.Pool
	metrics *observability.DatabaseMetrics
}

func NewMarketRepository(db *pgxpool.Pool, metrics ...*observability.DatabaseMetrics) *PgxMarketRepository {
	var databaseMetrics *observability.DatabaseMetrics
	if len(metrics) > 0 {
		databaseMetrics = metrics[0]
	}
	return &PgxMarketRepository{db: db, metrics: databaseMetrics}
}

func (r *PgxMarketRepository) Ping(ctx context.Context) (err error) {
	started := time.Now()
	defer func() { r.observeDatabase("ping", started, err) }()
	return r.db.Ping(ctx)
}

func (r *PgxMarketRepository) observeDatabase(operation string, started time.Time, err error) {
	if r.metrics != nil {
		r.metrics.Observe(operation, time.Since(started), err)
	}
}

func (r *PgxMarketRepository) IngestPrices(ctx context.Context, req domain.IngestPricesRequest) (result domain.IngestPricesResult, err error) {
	started := time.Now()
	defer func() { r.observeDatabase("ingest_prices", started, err) }()
	serverID, err := mapServer(req.Server)
	if err != nil {
		return domain.IngestPricesResult{}, err
	}
	requestHash, err := canonicalRequestHash(req)
	if err != nil {
		return domain.IngestPricesResult{}, fmt.Errorf("hash ingest request: %w", err)
	}
	var requestUUID pgtype.UUID
	if err := requestUUID.Scan(req.RequestID); err != nil {
		return domain.IngestPricesResult{}, fmt.Errorf("invalid request_id: %w", err)
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return domain.IngestPricesResult{}, fmt.Errorf("begin tx: %w", err)
	}
	transactionStarted := time.Now()
	defer func() { r.observeDatabase("transaction_prices", transactionStarted, err) }()
	defer func() { observability.RecordIngestTransaction(ctx, time.Since(transactionStarted)) }()
	defer func() { _ = tx.Rollback(ctx) }()

	registerTag, err := tx.Exec(ctx, registerPriceIngestRequestSQL, requestUUID, requestHash, serverID, len(req.Entries))
	if err != nil {
		return domain.IngestPricesResult{}, fmt.Errorf("register ingest request: %w", err)
	}
	if registerTag.RowsAffected() == 0 {
		result, err := r.existingIngestResult(ctx, tx, requestUUID, requestHash)
		if err != nil { return domain.IngestPricesResult{}, err }
		return result, nil
	}

	copyStarted := time.Now()
	copiedRows, err := tx.CopyFrom(ctx, marketIngestRawTable, marketIngestRawColumns, newRawPriceCopySource(requestUUID, serverID, req.Entries))
	if err == nil && copiedRows != int64(len(req.Entries)) { err = fmt.Errorf("copied %d rows, expected %d", copiedRows, len(req.Entries)) }
	r.observeDatabase("copy_raw_prices", copyStarted, err)
	if err != nil { return domain.IngestPricesResult{}, fmt.Errorf("copy raw market prices: %w", err) }

	upsertStarted := time.Now()
	tag, err := tx.Exec(ctx, upsertCurrentPricesSQL, requestUUID)
	r.observeDatabase("upsert_current_prices", upsertStarted, err)
	if err != nil { return domain.IngestPricesResult{}, fmt.Errorf("upsert current prices: %w", err) }
	if _, err := tx.Exec(ctx, completePriceIngestRequestSQL, requestUUID, tag.RowsAffected()); err != nil {
		return domain.IngestPricesResult{}, fmt.Errorf("complete ingest request: %w", err)
	}

	commitStarted := time.Now()
	err = tx.Commit(ctx)
	observability.RecordIngestCommit(ctx, time.Since(commitStarted))
	if err != nil { return domain.IngestPricesResult{}, fmt.Errorf("commit tx: %w", err) }
	return domain.IngestPricesResult{Accepted: len(req.Entries), CurrentRowsTouched: tag.RowsAffected()}, nil
}

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
	if s.index >= len(s.entries) { return false }
	s.index++
	return true
}

func (s *rawPriceCopySource) Values() ([]any, error) {
	if s.index == 0 || s.index > len(s.entries) { return nil, errCopySourceNotPositioned }
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

func (s *rawPriceCopySource) Err() error { return nil }

func (r *PgxMarketRepository) existingIngestResult(ctx context.Context, tx pgx.Tx, requestID pgtype.UUID, expectedHash []byte) (domain.IngestPricesResult, error) {
	const query = `select accepted_count, current_rows_touched, status, request_sha256 from market_ingest_requests where request_id = $1`
	var acceptedCount int
	var currentRowsTouched int64
	var status string
	var actualHash []byte
	if err := tx.QueryRow(ctx, query, requestID).Scan(&acceptedCount, &currentRowsTouched, &status, &actualHash); err != nil {
		return domain.IngestPricesResult{}, fmt.Errorf("query existing ingest request: %w", err)
	}
	if !equalHashes(actualHash, expectedHash) { return domain.IngestPricesResult{}, ErrRequestPayloadConflict }
	switch status {
	case "completed":
		return domain.IngestPricesResult{Accepted: acceptedCount, CurrentRowsTouched: currentRowsTouched, Duplicate: true}, nil
	case "processing":
		return domain.IngestPricesResult{}, ErrRequestAlreadyProcessing
	default:
		return domain.IngestPricesResult{}, fmt.Errorf("unsupported ingest request status %q", status)
	}
}

func canonicalRequestHash(req domain.IngestPricesRequest) ([]byte, error) {
	encoded, err := json.Marshal(req)
	if err != nil { return nil, err }
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

func equalHashes(a, b []byte) bool {
	if len(a) != len(b) { return false }
	for i := range a { if a[i] != b[i] { return false } }
	return true
}

func (r *PgxMarketRepository) QueryCurrentPrices(ctx context.Context, lookup domain.CurrentPriceLookup) (prices []domain.CurrentPrice, err error) {
	started := time.Now()
	defer func() { r.observeDatabase("query_current_prices", started, err) }()
	serverID, err := mapServer(lookup.Server)
	if err != nil { return nil, err }
	if len(lookup.LocationIDs) == 0 || len(lookup.Entries) == 0 { return []domain.CurrentPrice{}, nil }
	itemKeys := make([]string, 0, len(lookup.Entries))
	qualities := make([]int16, 0, len(lookup.Entries))
	for _, entry := range lookup.Entries { itemKeys = append(itemKeys, entry.ItemKey); qualities = append(qualities, entry.Quality) }
	const query = `
		select server, location_id, item_key, quality, sell_price_min, sell_price_min_at,
		       buy_price_max, buy_price_max_at, updated_at
		from current_market_prices
		where server = $1 and location_id = any($2)
		  and (item_key, quality) in (select * from unnest($3::text[], $4::smallint[]))
		order by location_id, item_key, quality`
	rows, err := r.db.Query(ctx, query, serverID, lookup.LocationIDs, itemKeys, qualities)
	if err != nil { return nil, fmt.Errorf("query current prices: %w", err) }
	defer rows.Close()
	prices = make([]domain.CurrentPrice, 0)
	for rows.Next() {
		var price domain.CurrentPrice
		var serverValue int16
		if err := rows.Scan(&serverValue, &price.LocationID, &price.ItemKey, &price.Quality, &price.SellPriceMin, &price.SellPriceMinAt, &price.BuyPriceMax, &price.BuyPriceMaxAt, &price.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan current price: %w", err)
		}
		price.Server = unmapServer(serverValue)
		prices = append(prices, price)
	}
	if err := rows.Err(); err != nil { return nil, fmt.Errorf("rows current prices: %w", err) }
	return prices, nil
}

func mapServer(server domain.Server) (int16, error) {
	switch server {
	case domain.ServerWest: return 1, nil
	case domain.ServerEast: return 2, nil
	case domain.ServerEurope: return 3, nil
	default: return 0, fmt.Errorf("invalid server: %s", server)
	}
}

func unmapServer(value int16) domain.Server {
	switch value {
	case 1: return domain.ServerWest
	case 2: return domain.ServerEast
	case 3: return domain.ServerEurope
	default: return domain.ServerWest
	}
}
