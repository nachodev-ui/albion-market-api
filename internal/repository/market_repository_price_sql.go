package repository

const registerPriceIngestRequestSQL = `
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

const upsertCurrentPricesSQL = `
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

const completePriceIngestRequestSQL = `
	update market_ingest_requests
	set
		status = 'completed',
		current_rows_touched = $2,
		completed_at = now()
	where request_id = $1
`
