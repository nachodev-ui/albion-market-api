package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/domain"
)

// QueryCurrentPricesWithTrust enriches the hot current-price model with
// seven-day historical evidence. All metrics are derived at read time from the
// existing history buckets, so no duplicated trust state or schema migration is
// required.
func (r *PgxMarketRepository) QueryCurrentPricesWithTrust(
	ctx context.Context,
	lookup domain.CurrentPriceLookup,
) (prices []domain.CurrentPrice, err error) {
	started := time.Now()
	defer func() { r.observeDatabase("query_current_prices_with_trust", started, err) }()

	serverID, err := mapServer(lookup.Server)
	if err != nil {
		return nil, err
	}
	if len(lookup.LocationIDs) == 0 || len(lookup.Entries) == 0 {
		return []domain.CurrentPrice{}, nil
	}

	itemKeys := make([]string, 0, len(lookup.Entries))
	qualities := make([]int16, 0, len(lookup.Entries))
	for _, entry := range lookup.Entries {
		itemKeys = append(itemKeys, entry.ItemKey)
		qualities = append(qualities, entry.Quality)
	}

	const query = `
		with requested_entries(item_key, quality) as (
			select * from unnest($3::text[], $4::smallint[])
		), history_stats as (
			select
				h.server,
				h.location_id,
				h.item_key,
				h.quality,
				count(*)::bigint as observations_7d,
				coalesce(sum(h.item_count), 0)::bigint as volume_7d,
				round(
					percentile_cont(0.5) within group (order by h.average_unit_price)
						filter (where h.average_unit_price is not null)
				)::bigint as median_price_7d
			from market_history_buckets h
			join requested_entries e using (item_key, quality)
			where h.server = $1
			  and h.location_id = any($2)
			  and h.bucket_at >= now() - interval '7 days'
			group by h.server, h.location_id, h.item_key, h.quality
		)
		select
			c.server,
			c.location_id,
			c.item_key,
			c.quality,
			c.sell_price_min,
			c.sell_price_min_at,
			c.buy_price_max,
			c.buy_price_max_at,
			c.updated_at,
			coalesce(h.observations_7d, 0),
			coalesce(h.volume_7d, 0),
			h.median_price_7d
		from current_market_prices c
		join requested_entries e using (item_key, quality)
		left join history_stats h
		  on h.server = c.server
		 and h.location_id = c.location_id
		 and h.item_key = c.item_key
		 and h.quality = c.quality
		where c.server = $1
		  and c.location_id = any($2)
		order by c.location_id, c.item_key, c.quality
	`

	rows, err := r.db.Query(ctx, query, serverID, lookup.LocationIDs, itemKeys, qualities)
	if err != nil {
		return nil, fmt.Errorf("query current prices with trust: %w", err)
	}
	defer rows.Close()

	prices = make([]domain.CurrentPrice, 0)
	for rows.Next() {
		var price domain.CurrentPrice
		var serverValue int16
		if err := rows.Scan(
			&serverValue,
			&price.LocationID,
			&price.ItemKey,
			&price.Quality,
			&price.SellPriceMin,
			&price.SellPriceMinAt,
			&price.BuyPriceMax,
			&price.BuyPriceMaxAt,
			&price.UpdatedAt,
			&price.HistoryObservations7D,
			&price.HistoryVolume7D,
			&price.MedianPrice7D,
		); err != nil {
			return nil, fmt.Errorf("scan current price with trust: %w", err)
		}
		price.Server = unmapServer(serverValue)
		prices = append(prices, price)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows current prices with trust: %w", err)
	}
	return prices, nil
}
