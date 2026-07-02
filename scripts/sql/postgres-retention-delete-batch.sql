\pset tuples_only on
\pset format unaligned
\pset pager off

begin;
set local lock_timeout = :'lock_timeout';
set local statement_timeout = :'statement_timeout';

select :'target' = 'market_history_ingest_raw' as is_history_raw \gset
select :'target' = 'market_ingest_raw' as is_price_raw \gset
select :'target' = 'market_history_ingest_requests' as is_history_requests \gset
select :'target' = 'market_ingest_requests' as is_price_requests \gset
select :'target' = 'market_history_buckets' as is_history_buckets \gset

\if :is_history_raw
with candidates as materialized (
  select r.id
  from public.market_history_ingest_raw r
  where r.received_at < :'cutoff'::timestamptz
    and not exists (
      select 1
      from public.market_history_ingest_requests q
      where q.request_id = r.request_id
        and q.status = 'processing'
    )
  order by r.received_at, r.id
  limit :batch_size
  for update of r skip locked
), deleted as (
  delete from public.market_history_ingest_raw r
  using candidates c
  where r.id = c.id
  returning 1
)
select count(*) from deleted;
\elif :is_price_raw
with candidates as materialized (
  select r.id
  from public.market_ingest_raw r
  where r.received_at < :'cutoff'::timestamptz
    and not exists (
      select 1
      from public.market_ingest_requests q
      where q.request_id = r.request_id
        and q.status = 'processing'
    )
  order by r.received_at, r.id
  limit :batch_size
  for update of r skip locked
), deleted as (
  delete from public.market_ingest_raw r
  using candidates c
  where r.id = c.id
  returning 1
)
select count(*) from deleted;
\elif :is_history_requests
with candidates as materialized (
  select q.request_id
  from public.market_history_ingest_requests q
  where q.created_at < :'cutoff'::timestamptz
    and q.status = 'completed'
    and not exists (
      select 1
      from public.market_history_ingest_raw r
      where r.request_id = q.request_id
    )
  order by q.created_at, q.request_id
  limit :batch_size
  for update of q skip locked
), deleted as (
  delete from public.market_history_ingest_requests q
  using candidates c
  where q.request_id = c.request_id
    and q.status = 'completed'
    and q.created_at < :'cutoff'::timestamptz
    and not exists (
      select 1
      from public.market_history_ingest_raw r
      where r.request_id = q.request_id
    )
  returning 1
)
select count(*) from deleted;
\elif :is_price_requests
with candidates as materialized (
  select q.request_id
  from public.market_ingest_requests q
  where q.created_at < :'cutoff'::timestamptz
    and q.status = 'completed'
    and not exists (
      select 1
      from public.market_ingest_raw r
      where r.request_id = q.request_id
    )
  order by q.created_at, q.request_id
  limit :batch_size
  for update of q skip locked
), deleted as (
  delete from public.market_ingest_requests q
  using candidates c
  where q.request_id = c.request_id
    and q.status = 'completed'
    and q.created_at < :'cutoff'::timestamptz
    and not exists (
      select 1
      from public.market_ingest_raw r
      where r.request_id = q.request_id
    )
  returning 1
)
select count(*) from deleted;
\elif :is_history_buckets
with candidates as materialized (
  select
    h.server,
    h.location_id,
    h.item_key,
    h.quality,
    h.bucket_at
  from public.market_history_buckets h
  where h.bucket_at < :'cutoff'::timestamptz
  order by
    h.bucket_at,
    h.server,
    h.location_id,
    h.item_key,
    h.quality
  limit :batch_size
  for update of h skip locked
), deleted as (
  delete from public.market_history_buckets h
  using candidates c
  where h.server = c.server
    and h.location_id = c.location_id
    and h.item_key = c.item_key
    and h.quality = c.quality
    and h.bucket_at = c.bucket_at
  returning 1
)
select count(*) from deleted;
\else
\echo 'Unsupported retention target.'
\quit 3
\endif

commit;
