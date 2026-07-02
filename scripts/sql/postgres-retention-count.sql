\pset tuples_only on
\pset format unaligned
\pset pager off

begin transaction read only;

select :'target' = 'market_history_ingest_raw' as is_history_raw \gset
select :'target' = 'market_ingest_raw' as is_price_raw \gset
select :'target' = 'market_history_ingest_requests' as is_history_requests \gset
select :'target' = 'market_ingest_requests' as is_price_requests \gset
select :'target' = 'market_history_buckets' as is_history_buckets \gset

\if :is_history_raw
select count(*)
from public.market_history_ingest_raw r
where r.received_at < :'cutoff'::timestamptz
  and not exists (
    select 1
    from public.market_history_ingest_requests q
    where q.request_id = r.request_id
      and q.status = 'processing'
  );
\elif :is_price_raw
select count(*)
from public.market_ingest_raw r
where r.received_at < :'cutoff'::timestamptz
  and not exists (
    select 1
    from public.market_ingest_requests q
    where q.request_id = r.request_id
      and q.status = 'processing'
  );
\elif :is_history_requests
select count(*)
from public.market_history_ingest_requests q
where q.created_at < :'cutoff'::timestamptz
  and q.status = 'completed'
  and not exists (
    select 1
    from public.market_history_ingest_raw r
    where r.request_id = q.request_id
      and (
        :'simulate_raw_cleanup'::boolean = false
        or r.received_at >= :'raw_cutoff'::timestamptz
        or exists (
          select 1
          from public.market_history_ingest_requests protected
          where protected.request_id = r.request_id
            and protected.status = 'processing'
        )
      )
  );
\elif :is_price_requests
select count(*)
from public.market_ingest_requests q
where q.created_at < :'cutoff'::timestamptz
  and q.status = 'completed'
  and not exists (
    select 1
    from public.market_ingest_raw r
    where r.request_id = q.request_id
      and (
        :'simulate_raw_cleanup'::boolean = false
        or r.received_at >= :'raw_cutoff'::timestamptz
        or exists (
          select 1
          from public.market_ingest_requests protected
          where protected.request_id = r.request_id
            and protected.status = 'processing'
        )
      )
  );
\elif :is_history_buckets
select count(*)
from public.market_history_buckets h
where h.bucket_at < :'cutoff'::timestamptz;
\else
\echo 'Unsupported retention target.'
\quit 3
\endif

commit;
