\pset tuples_only on
\pset format unaligned
\pset pager off

with required_objects(object_name) as (
  values
    ('public.market_history_ingest_raw'),
    ('public.market_ingest_raw'),
    ('public.market_history_ingest_requests'),
    ('public.market_ingest_requests'),
    ('public.market_history_buckets'),
    ('public.market_history_ingest_raw_received_at_id_idx'),
    ('public.market_ingest_raw_received_at_id_idx'),
    ('public.market_history_buckets_bucket_at_idx')
)
select count(*)
from required_objects
where to_regclass(object_name) is null;
