\pset pager off
\pset border 2
\pset null '(null)'
\timing on

\echo '=== PostgreSQL index review: environment ==='
select
  current_database() as database_name,
  current_user as database_user,
  version() as postgres_version,
  current_setting('server_version_num') as server_version_num;

select
  datname as database_name,
  stats_reset
from pg_stat_database
where datname = current_database();

\echo '=== PostgreSQL index review: table statistics ==='
select
  relname as table_name,
  n_live_tup as estimated_live_rows,
  n_dead_tup as estimated_dead_rows,
  seq_scan,
  idx_scan,
  last_analyze,
  last_autoanalyze
from pg_stat_user_tables
where schemaname = 'public'
  and relname in (
    'current_market_prices',
    'market_history_buckets',
    'market_ingest_raw',
    'market_history_ingest_raw',
    'market_ingest_requests',
    'market_history_ingest_requests'
  )
order by relname;

\echo '=== PostgreSQL index review: index statistics ==='
select
  relname as table_name,
  indexrelname as index_name,
  idx_scan,
  idx_tup_read,
  idx_tup_fetch,
  pg_size_pretty(pg_relation_size(indexrelid)) as index_size
from pg_stat_user_indexes
where schemaname = 'public'
  and relname in (
    'current_market_prices',
    'market_history_buckets',
    'market_ingest_raw',
    'market_history_ingest_raw',
    'market_ingest_requests',
    'market_history_ingest_requests'
  )
order by relname, indexrelname;

select format(
  'INDEX_STAT|%s|%s|%s|%s|%s',
  indexrelname,
  idx_scan,
  idx_tup_read,
  idx_tup_fetch,
  pg_relation_size(indexrelid)
)
from pg_stat_user_indexes
where schemaname = 'public'
  and indexrelname in (
    'current_market_prices_pkey',
    'current_market_prices_item_loc_idx',
    'market_history_buckets_pkey',
    'market_history_buckets_bucket_at_idx',
    'market_ingest_raw_request_id_idx',
    'market_ingest_raw_received_at_id_idx',
    'market_history_ingest_raw_request_id_idx',
    'market_history_ingest_raw_received_at_id_idx',
    'market_ingest_requests_created_at_idx',
    'market_history_ingest_requests_created_at_idx'
  )
order by indexrelname;

-- Pick representative values from the real tables. Fallback values keep the
-- plans executable when a table is empty without modifying any data.
with chosen_server as (
  select coalesce((
    select server
    from public.current_market_prices
    group by server
    order by count(*) desc, server
    limit 1
  ), 1::smallint) as server
), locations as (
  select distinct p.location_id
  from public.current_market_prices p
  cross join chosen_server s
  where p.server = s.server
  order by p.location_id
  limit :sample_locations
), entries as (
  select distinct p.item_key, p.quality
  from public.current_market_prices p
  cross join chosen_server s
  where p.server = s.server
  order by p.item_key, p.quality
  limit :sample_entries
)
select
  (select server from chosen_server) as price_server,
  coalesce(
    (select array_agg(location_id order by location_id)::text from locations),
    '{1}'
  ) as price_location_ids,
  coalesce(
    (select array_agg(item_key order by item_key, quality)::text from entries),
    '{__INDEX_REVIEW_MISSING__}'
  ) as price_item_keys,
  coalesce(
    (select array_agg(quality order by item_key, quality)::text from entries),
    '{1}'
  ) as price_qualities
\gset

with chosen_server as (
  select coalesce((
    select server
    from public.market_history_buckets
    group by server
    order by count(*) desc, server
    limit 1
  ), 1::smallint) as server
), locations as (
  select distinct h.location_id
  from public.market_history_buckets h
  cross join chosen_server s
  where h.server = s.server
  order by h.location_id
  limit :sample_locations
), entries as (
  select distinct h.item_key, h.quality
  from public.market_history_buckets h
  cross join chosen_server s
  where h.server = s.server
  order by h.item_key, h.quality
  limit :sample_entries
), bounds as (
  select
    coalesce(min(bucket_at), now() - interval '28 days') as range_start,
    coalesce(max(bucket_at) + interval '1 microsecond', now()) as range_end
  from public.market_history_buckets h
  cross join chosen_server s
  where h.server = s.server
)
select
  (select server from chosen_server) as history_server,
  coalesce(
    (select array_agg(location_id order by location_id)::text from locations),
    '{1}'
  ) as history_location_ids,
  coalesce(
    (select array_agg(item_key order by item_key, quality)::text from entries),
    '{__INDEX_REVIEW_MISSING__}'
  ) as history_item_keys,
  coalesce(
    (select array_agg(quality order by item_key, quality)::text from entries),
    '{1}'
  ) as history_qualities,
  (select range_start::text from bounds) as history_range_start,
  (select range_end::text from bounds) as history_range_end
\gset

select coalesce(
  (select request_id::text from public.market_ingest_raw order by received_at desc, id desc limit 1),
  '00000000-0000-0000-0000-000000000000'
) as price_request_id
\gset

select coalesce(
  (select request_id::text from public.market_history_ingest_raw order by received_at desc, id desc limit 1),
  '00000000-0000-0000-0000-000000000000'
) as history_request_id
\gset

\echo '=== PostgreSQL index review: representative parameters ==='
select
  :price_server::smallint as price_server,
  :'price_location_ids'::smallint[] as price_location_ids,
  cardinality(:'price_item_keys'::text[]) as price_entry_count,
  :history_server::smallint as history_server,
  :'history_location_ids'::smallint[] as history_location_ids,
  cardinality(:'history_item_keys'::text[]) as history_entry_count,
  :'history_range_start'::timestamptz as history_range_start,
  :'history_range_end'::timestamptz as history_range_end,
  :'price_request_id'::uuid as price_request_id,
  :'history_request_id'::uuid as history_request_id;

begin transaction read only;
set local statement_timeout = '120s';

\echo '=== PLAN:current_market_prices:default ==='
explain (analyze, buffers, settings, summary, format text)
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
from public.current_market_prices
where server = :price_server::smallint
  and location_id = any(:'price_location_ids'::smallint[])
  and (item_key, quality) in (
    select * from unnest(
      :'price_item_keys'::text[],
      :'price_qualities'::smallint[]
    )
  )
order by location_id, item_key, quality;

\echo '=== PLAN:market_history_buckets:default ==='
explain (analyze, buffers, settings, summary, format text)
select
  server,
  location_id,
  item_key,
  quality,
  bucket_at,
  item_count,
  average_unit_price
from public.market_history_buckets
where server = :history_server::smallint
  and location_id = any(:'history_location_ids'::smallint[])
  and (item_key, quality) in (
    select * from unnest(
      :'history_item_keys'::text[],
      :'history_qualities'::smallint[]
    )
  )
  and bucket_at >= :'history_range_start'::timestamptz
  and bucket_at < :'history_range_end'::timestamptz
order by location_id, item_key, quality, bucket_at;

\echo '=== PLAN:market_ingest_raw:request_id ==='
explain (analyze, buffers, settings, summary, format text)
select
  id,
  server,
  location_id,
  item_key,
  quality,
  observed_at,
  sell_price_min,
  sell_price_min_at,
  buy_price_max,
  buy_price_max_at
from public.market_ingest_raw
where request_id = :'price_request_id'::uuid;

\echo '=== PLAN:market_history_ingest_raw:request_id ==='
explain (analyze, buffers, settings, summary, format text)
select
  id,
  server,
  location_id,
  item_key,
  quality,
  bucket_at,
  observed_at,
  item_count,
  average_unit_price
from public.market_history_ingest_raw
where request_id = :'history_request_id'::uuid;

\echo '=== PLAN:retention:market_history_ingest_raw ==='
explain (analyze, buffers, settings, summary, format text)
select r.id
from public.market_history_ingest_raw r
where r.received_at < now() - interval '30 days'
  and not exists (
    select 1
    from public.market_history_ingest_requests q
    where q.request_id = r.request_id
      and q.status = 'processing'
  )
order by r.received_at, r.id
limit :retention_batch_size;

\echo '=== PLAN:retention:market_ingest_raw ==='
explain (analyze, buffers, settings, summary, format text)
select r.id
from public.market_ingest_raw r
where r.received_at < now() - interval '30 days'
  and not exists (
    select 1
    from public.market_ingest_requests q
    where q.request_id = r.request_id
      and q.status = 'processing'
  )
order by r.received_at, r.id
limit :retention_batch_size;

\echo '=== PLAN:retention:market_history_ingest_requests ==='
explain (analyze, buffers, settings, summary, format text)
select q.request_id
from public.market_history_ingest_requests q
where q.created_at < now() - interval '90 days'
  and q.status = 'completed'
  and not exists (
    select 1
    from public.market_history_ingest_raw r
    where r.request_id = q.request_id
  )
order by q.created_at, q.request_id
limit :retention_batch_size;

\echo '=== PLAN:retention:market_ingest_requests ==='
explain (analyze, buffers, settings, summary, format text)
select q.request_id
from public.market_ingest_requests q
where q.created_at < now() - interval '90 days'
  and q.status = 'completed'
  and not exists (
    select 1
    from public.market_ingest_raw r
    where r.request_id = q.request_id
  )
order by q.created_at, q.request_id
limit :retention_batch_size;

\echo '=== PLAN:retention:market_history_buckets ==='
explain (analyze, buffers, settings, summary, format text)
select
  h.server,
  h.location_id,
  h.item_key,
  h.quality,
  h.bucket_at
from public.market_history_buckets h
where h.bucket_at < now() - interval '400 days'
order by
  h.bucket_at,
  h.server,
  h.location_id,
  h.item_key,
  h.quality
limit :retention_batch_size;

commit;

begin transaction read only;
set local statement_timeout = '120s';
set local enable_seqscan = off;

\echo '=== PLAN:current_market_prices:index_preferred ==='
explain (analyze, buffers, settings, summary, format text)
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
from public.current_market_prices
where server = :price_server::smallint
  and location_id = any(:'price_location_ids'::smallint[])
  and (item_key, quality) in (
    select * from unnest(
      :'price_item_keys'::text[],
      :'price_qualities'::smallint[]
    )
  )
order by location_id, item_key, quality;

commit;
