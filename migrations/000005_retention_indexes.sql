-- Support bounded retention scans without changing the current table layout.
-- These indexes keep DELETE batches ordered by their retention timestamp and
-- avoid repeatedly scanning the full raw/history tables.
create index if not exists market_history_ingest_raw_received_at_id_idx
  on public.market_history_ingest_raw (received_at, id);

create index if not exists market_ingest_raw_received_at_id_idx
  on public.market_ingest_raw (received_at, id);

create index if not exists market_history_buckets_bucket_at_idx
  on public.market_history_buckets (bucket_at);
