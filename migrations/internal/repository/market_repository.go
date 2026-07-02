package repository

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
)

var (
	ErrRequestAlreadyProcessing = errors.New("request_id is already processing")
	ErrRequestPayloadConflict   = errors.New("request_id was already used with a different payload")
	errCopySourceNotPositioned  = errors.New("raw price copy source is not positioned on a row")
)

var (
	marketIngestRawTable   = pgx.Identifier{"market_ingest_raw"}
	marketIngestRawColumns = []string{
		"request_id",
		"server",
		"observed_at",
		"location_id",
		"item_key",
		"quality",
		"sell_price_min",
		"sell_price_min_at",
		"buy_price_max",
		"buy_price_max_at",
	}
)

type MarketRepository interface {
	IngestPrices(ctx context.Context, req domain.IngestPricesRequest) (domain.IngestPricesResult, error)
	IngestHistory(ctx context.Context, req domain.IngestHistoryRequest) (domain.IngestHistoryResult, error)
	QueryCurrentPrices(ctx context.Context, lookup domain.CurrentPriceLookup) ([]domain.CurrentPrice, error)
	QueryMarketHistory(ctx context.Context, lookup domain.MarketHistoryLookup) ([]domain.MarketHistorySeries, error)
	Ping(ctx context.Context) error
}

type PgxMarketRepository struct {
	db *pgxpool.Pool
}

func NewMarketRepository(db *pgxpool.Pool) *PgxMarketRepository {
	return &PgxMarketRepository{db: db}
}

func (r *PgxMarketRepository) Ping(ctx context.Context) error {
	return r.db.Ping(ctx)
}

func (r *PgxMarketRepository) IngestPrices(ctx context.Context, req domain.IngestPricesRequest) (domain.IngestPricesResult, error) {
	// Do validation and CPU work before opening the transaction so a pool
	// connection is not held while the request hash is calculated.
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
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	const registerRequest = `
		insert into market_ingest_requests (
			request_id,
			request_sha256,
			server,
			accepted_count,
			status
		)
		values ($1, $2, $3, $4, 'processing')
		on conflict (request_id) do nothing
	`

	registerTag, err := tx.Exec(ctx, registerRequest, requestUUID, requestHash, serverID, len(req.Entries))
	if err != nil {
		return domain.IngestPricesResult{}, fmt.Errorf("register ingest request: %w", err)
	}

	if registerTag.RowsAffected() == 0 {
		result, err := r.existingIngestResult(ctx, tx, requestUUID, requestHash)
		if err != nil {
			return domain.IngestPricesResult{}, err
		}
		return result, nil
	}

	copiedRows, err := tx.CopyFrom(
		ctx,
		marketIngestRawTable,
		marketIngestRawColumns,
		newRawPriceCopySource(requestUUID, serverID, req.Entries),
	)
	if err != nil {
		return domain.IngestPricesResult{}, fmt.Errorf("copy raw market prices: %w", err)
	}
	if copiedRows != int64(len(req.Entries)) {
		return domain.IngestPricesResult{}, fmt.Errorf(
			"copy raw market prices: copied %d rows, expected %d",
			copiedRows,
			len(req.Entries),
		)
	}

	// market_ingest_raw remains the durable audit trail. The request rows are
	// reduced set-wise before touching current_market_prices, which is kept as
	// the small, hot read model.
	const upsertCurrent = `
		with request_rows as materialized (
			select
				r.id,
				r.server,
				r.location_id,
				r.item_key,
				r.quality,
				r.observed_at,
				r.sell_price_min,
				r.sell_price_min_at,
				r.buy_price_max,
				r.buy_price_max_at
			from market_ingest_raw r
			where r.request_id = $1
		), request_keys as (
			select
				r.server,
				r.location_id,
				r.item_key,
				r.quality
			from request_rows r
			group by r.server, r.location_id, r.item_key, r.quality
		), latest_sell as (
			select distinct on (r.server, r.location_id, r.item_key, r.quality)
				r.server,
				r.location_id,
				r.item_key,
				r.quality,
				r.sell_price_min,
				r.sell_price_min_at
			from request_rows r
			where r.sell_price_min_at is not null
			order by
				r.server,
				r.location_id,
				r.item_key,
				r.quality,
				r.sell_price_min_at desc,
				r.observed_at desc,
				r.id desc
		), latest_buy as (
			select distinct on (r.server, r.location_id, r.item_key, r.quality)
				r.server,
				r.location_id,
				r.item_key,
				r.quality,
				r.buy_price_max,
				r.buy_price_max_at
			from request_rows r
			where r.buy_price_max_at is not null
			order by
				r.server,
				r.location_id,
				r.item_key,
				r.quality,
				r.buy_price_max_at desc,
				r.observed_at desc,
				r.id desc
		)
		insert into current_market_prices (
			server,
			location_id,
			item_key,
			quality,
			sell_price_min,
			sell_price_min_at,
			buy_price_max,
			buy_price_max_at,
			updated_at
		)
		select
			k.server,
			k.location_id,
			k.item_key,
			k.quality,
			s.sell_price_min,
			s.sell_price_min_at,
			b.buy_price_max,
			b.buy_price_max_at,
			now()
		from request_keys k
		left join latest_sell s using (server, location_id, item_key, quality)
		left join latest_buy b using (server, location_id, item_key, quality)
		on conflict (server, location_id, item_key, quality)
		do update
		set
			sell_price_min = case
				when excluded.sell_price_min_at is not null
					and (
						current_market_prices.sell_price_min_at is null
						or excluded.sell_price_min_at >= current_market_prices.sell_price_min_at
					)
				then excluded.sell_price_min
				else current_market_prices.sell_price_min
			end,
			sell_price_min_at = case
				when excluded.sell_price_min_at is not null
					and (
						current_market_prices.sell_price_min_at is null
						or excluded.sell_price_min_at >= current_market_prices.sell_price_min_at
					)
				then excluded.sell_price_min_at
				else current_market_prices.sell_price_min_at
			end,
			buy_price_max = case
				when excluded.buy_price_max_at is not null
					and (
						current_market_prices.buy_price_max_at is null
						or excluded.buy_price_max_at >= current_market_prices.buy_price_max_at
					)
				then excluded.buy_price_max
				else current_market_prices.buy_price_max
			end,
			buy_price_max_at = case
				when excluded.buy_price_max_at is not null
					and (
						current_market_prices.buy_price_max_at is null
						or excluded.buy_price_max_at >= current_market_prices.buy_price_max_at
					)
				then excluded.buy_price_max_at
				else current_market_prices.buy_price_max_at
			end,
			updated_at = now()
		where
			(
				excluded.sell_price_min_at is not null
				and (
					current_market_prices.sell_price_min_at is null
					or excluded.sell_price_min_at > current_market_prices.sell_price_min_at
					or (
						excluded.sell_price_min_at = current_market_prices.sell_price_min_at
						and excluded.sell_price_min is distinct from current_market_prices.sell_price_min
					)
				)
			)
			or (
				excluded.buy_price_max_at is not null
				and (
					current_market_prices.buy_price_max_at is null
					or excluded.buy_price_max_at > current_market_prices.buy_price_max_at
					or (
						excluded.buy_price_max_at = current_market_prices.buy_price_max_at
						and excluded.buy_price_max is distinct from current_market_prices.buy_price_max
					)
				)
			)
	`

	tag, err := tx.Exec(ctx, upsertCurrent, requestUUID)
	if err != nil {
		return domain.IngestPricesResult{}, fmt.Errorf("upsert current prices: %w", err)
	}

	const completeRequest = `
		update market_ingest_requests
		set
			status = 'completed',
			current_rows_touched = $2,
			completed_at = now()
		where request_id = $1
	`
	if _, err := tx.Exec(ctx, completeRequest, requestUUID, tag.RowsAffected()); err != nil {
		return domain.IngestPricesResult{}, fmt.Errorf("complete ingest request: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.IngestPricesResult{}, fmt.Errorf("commit tx: %w", err)
	}

	return domain.IngestPricesResult{
		Accepted:           len(req.Entries),
		CurrentRowsTouched: tag.RowsAffected(),
		Duplicate:          false,
	}, nil
}

// rawPriceCopySource feeds COPY row-by-row without first building a [][]any
// buffer. The values array is reused because pgx encodes each row before
// requesting the next one.
type rawPriceCopySource struct {
	entries []domain.PriceIngest
	index   int
	values  [10]any
}

func newRawPriceCopySource(requestID pgtype.UUID, serverID int16, entries []domain.PriceIngest) *rawPriceCopySource {
	source := &rawPriceCopySource{entries: entries}
	source.values[0] = requestID
	source.values[1] = serverID
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

func (r *PgxMarketRepository) existingIngestResult(ctx context.Context, tx pgx.Tx, requestID pgtype.UUID, expectedHash []byte) (domain.IngestPricesResult, error) {
	const queryExisting = `
		select accepted_count, current_rows_touched, status, request_sha256
		from market_ingest_requests
		where request_id = $1
	`

	var acceptedCount int
	var currentRowsTouched int64
	var status string
	var actualHash []byte

	if err := tx.QueryRow(ctx, queryExisting, requestID).Scan(&acceptedCount, &currentRowsTouched, &status, &actualHash); err != nil {
		return domain.IngestPricesResult{}, fmt.Errorf("query existing ingest request: %w", err)
	}

	if !equalHashes(actualHash, expectedHash) {
		return domain.IngestPricesResult{}, ErrRequestPayloadConflict
	}

	switch status {
	case "completed":
		return domain.IngestPricesResult{
			Accepted:           acceptedCount,
			CurrentRowsTouched: currentRowsTouched,
			Duplicate:          true,
		}, nil
	case "processing":
		return domain.IngestPricesResult{}, ErrRequestAlreadyProcessing
	default:
		return domain.IngestPricesResult{}, fmt.Errorf("unsupported ingest request status %q", status)
	}
}

func canonicalRequestHash(req domain.IngestPricesRequest) ([]byte, error) {
	encoded, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

func equalHashes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (r *PgxMarketRepository) QueryCurrentPrices(ctx context.Context, lookup domain.CurrentPriceLookup) ([]domain.CurrentPrice, error) {
	serverID, err := mapServer(lookup.Server)
	if err != nil {
		return nil, err
	}

	if len(lookup.LocationIDs) == 0 || len(lookup.Entries) == 0 {
		return []domain.CurrentPrice{}, nil
	}

	itemKeys := make([]string, 0, len(lookup.Entries))
	qualities := make([]int16, 0, len(lookup.Entries))
	for _, e := range lookup.Entries {
		itemKeys = append(itemKeys, e.ItemKey)
		qualities = append(qualities, e.Quality)
	}

	const q = `
		select
			server,
			location_id,
			item_key,
			quality,
			sell_price_min,
			sell_price_min_at,
			buy_price_max,
			buy_price_max_at,
			updated_at
		from current_market_prices
		where server = $1
		  and location_id = any($2)
		  and (item_key, quality) in (
			select * from unnest($3::text[], $4::smallint[])
		  )
		order by location_id, item_key, quality
	`

	rows, err := r.db.Query(ctx, q, serverID, lookup.LocationIDs, itemKeys, qualities)
	if err != nil {
		return nil, fmt.Errorf("query current prices: %w", err)
	}
	defer rows.Close()

	var prices []domain.CurrentPrice
	for rows.Next() {
		var p domain.CurrentPrice
		var serverValue int16

		if err := rows.Scan(
			&serverValue,
			&p.LocationID,
			&p.ItemKey,
			&p.Quality,
			&p.SellPriceMin,
			&p.SellPriceMinAt,
			&p.BuyPriceMax,
			&p.BuyPriceMaxAt,
			&p.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan current price: %w", err)
		}

		p.Server = unmapServer(serverValue)
		prices = append(prices, p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows current prices: %w", err)
	}

	return prices, nil
}

func mapServer(s domain.Server) (int16, error) {
	switch s {
	case domain.ServerWest:
		return 1, nil
	case domain.ServerEast:
		return 2, nil
	case domain.ServerEurope:
		return 3, nil
	default:
		return 0, fmt.Errorf("invalid server: %s", s)
	}
}

func unmapServer(v int16) domain.Server {
	switch v {
	case 1:
		return domain.ServerWest
	case 2:
		return domain.ServerEast
	case 3:
		return domain.ServerEurope
	default:
		return domain.ServerWest
	}
}
