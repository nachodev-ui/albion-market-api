\pset pager off

begin;

insert into public.market_history_ingest_requests (
  request_id,
  request_sha256,
  server,
  accepted_entries,
  accepted_buckets,
  history_rows_touched,
  status,
  created_at,
  completed_at
)
values
  ('00000000-0000-0000-0000-000000000001', decode(repeat('01', 32), 'hex'), 1, 1, 1, 1, 'completed', :'reference_time'::timestamptz - interval '100 days', :'reference_time'::timestamptz - interval '100 days'),
  ('00000000-0000-0000-0000-000000000002', decode(repeat('02', 32), 'hex'), 1, 1, 1, 1, 'completed', :'reference_time'::timestamptz - interval '10 days',  :'reference_time'::timestamptz - interval '10 days'),
  ('00000000-0000-0000-0000-000000000003', decode(repeat('03', 32), 'hex'), 1, 1, 1, 0, 'processing', :'reference_time'::timestamptz - interval '100 days', null),
  ('00000000-0000-0000-0000-000000000004', decode(repeat('04', 32), 'hex'), 1, 1, 1, 1, 'completed', :'reference_time'::timestamptz - interval '100 days', :'reference_time'::timestamptz - interval '100 days');

insert into public.market_history_ingest_raw (
  request_id,
  received_at,
  server,
  observed_at,
  location_id,
  item_key,
  quality,
  bucket_at,
  item_count,
  average_unit_price
)
values
  ('00000000-0000-0000-0000-000000000001', :'reference_time'::timestamptz - interval '31 days', 1, :'reference_time'::timestamptz - interval '31 days', 1001, 'T4_HISTORY_OLD', 1, :'reference_time'::timestamptz - interval '31 days', 10, 100),
  ('00000000-0000-0000-0000-000000000002', :'reference_time'::timestamptz - interval '31 days', 1, :'reference_time'::timestamptz - interval '31 days', 1001, 'T4_HISTORY_RECENT_REQUEST', 1, :'reference_time'::timestamptz - interval '31 days', 10, 100),
  ('00000000-0000-0000-0000-000000000003', :'reference_time'::timestamptz - interval '31 days', 1, :'reference_time'::timestamptz - interval '31 days', 1001, 'T4_HISTORY_PROCESSING', 1, :'reference_time'::timestamptz - interval '31 days', 10, 100),
  ('00000000-0000-0000-0000-000000000004', :'reference_time'::timestamptz - interval '20 days', 1, :'reference_time'::timestamptz - interval '20 days', 1001, 'T4_HISTORY_RECENT_RAW', 1, :'reference_time'::timestamptz - interval '20 days', 10, 100);

insert into public.market_ingest_requests (
  request_id,
  request_sha256,
  server,
  accepted_count,
  current_rows_touched,
  status,
  created_at,
  completed_at
)
values
  ('10000000-0000-0000-0000-000000000001', decode(repeat('11', 32), 'hex'), 1, 1, 1, 'completed', :'reference_time'::timestamptz - interval '100 days', :'reference_time'::timestamptz - interval '100 days'),
  ('10000000-0000-0000-0000-000000000002', decode(repeat('12', 32), 'hex'), 1, 1, 1, 'completed', :'reference_time'::timestamptz - interval '10 days',  :'reference_time'::timestamptz - interval '10 days'),
  ('10000000-0000-0000-0000-000000000003', decode(repeat('13', 32), 'hex'), 1, 1, 0, 'processing', :'reference_time'::timestamptz - interval '100 days', null),
  ('10000000-0000-0000-0000-000000000004', decode(repeat('14', 32), 'hex'), 1, 1, 1, 'completed', :'reference_time'::timestamptz - interval '100 days', :'reference_time'::timestamptz - interval '100 days');

insert into public.market_ingest_raw (
  request_id,
  received_at,
  server,
  observed_at,
  location_id,
  item_key,
  quality,
  sell_price_min,
  sell_price_min_at,
  buy_price_max,
  buy_price_max_at
)
values
  ('10000000-0000-0000-0000-000000000001', :'reference_time'::timestamptz - interval '31 days', 1, :'reference_time'::timestamptz - interval '31 days', 1001, 'T4_PRICE_OLD', 1, 100, :'reference_time'::timestamptz - interval '31 days', 90, :'reference_time'::timestamptz - interval '31 days'),
  ('10000000-0000-0000-0000-000000000002', :'reference_time'::timestamptz - interval '31 days', 1, :'reference_time'::timestamptz - interval '31 days', 1001, 'T4_PRICE_RECENT_REQUEST', 1, 100, :'reference_time'::timestamptz - interval '31 days', 90, :'reference_time'::timestamptz - interval '31 days'),
  ('10000000-0000-0000-0000-000000000003', :'reference_time'::timestamptz - interval '31 days', 1, :'reference_time'::timestamptz - interval '31 days', 1001, 'T4_PRICE_PROCESSING', 1, 100, :'reference_time'::timestamptz - interval '31 days', 90, :'reference_time'::timestamptz - interval '31 days'),
  ('10000000-0000-0000-0000-000000000004', :'reference_time'::timestamptz - interval '20 days', 1, :'reference_time'::timestamptz - interval '20 days', 1001, 'T4_PRICE_RECENT_RAW', 1, 100, :'reference_time'::timestamptz - interval '20 days', 90, :'reference_time'::timestamptz - interval '20 days');

insert into public.market_history_buckets (
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
values
  (1, 1001, 'T4_BUCKET_OLD', 1, :'reference_time'::timestamptz - interval '401 days', 10, 100, :'reference_time'::timestamptz - interval '401 days', :'reference_time'::timestamptz - interval '401 days'),
  (1, 1001, 'T4_BUCKET_BOUNDARY', 1, :'reference_time'::timestamptz - interval '400 days', 10, 100, :'reference_time'::timestamptz - interval '400 days', :'reference_time'::timestamptz - interval '400 days'),
  (1, 1001, 'T4_BUCKET_RECENT', 1, :'reference_time'::timestamptz - interval '10 days', 10, 100, :'reference_time'::timestamptz - interval '10 days', :'reference_time'::timestamptz - interval '10 days');

insert into public.current_market_prices (
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
values (
  1,
  1001,
  'T4_CURRENT_PRICE',
  1,
  100,
  :'reference_time'::timestamptz,
  90,
  :'reference_time'::timestamptz,
  :'reference_time'::timestamptz
);

commit;
