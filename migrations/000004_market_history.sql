-- Idempotent request ledger for normalized history batches sent by the local
-- receiver. It is intentionally separate from price ingest so each pipeline
-- can be retried and observed independently.
create table if not exists market_history_ingest_requests (
  request_id uuid primary key,
  request_sha256 bytea not null,
  server smallint not null check (server between 1 and 3),
  accepted_entries integer not null check (accepted_entries >= 0),
  accepted_buckets integer not null check (accepted_buckets >= 0),
  history_rows_touched bigint not null default 0 check (history_rows_touched >= 0),
  status text not null check (status in ('processing', 'completed')),
  created_at timestamptz not null default now(),
  completed_at timestamptz
);

create index if not exists market_history_ingest_requests_created_at_idx
  on market_history_ingest_requests (created_at desc);

-- Durable append-only audit trail. A normalized capture with N history
-- buckets produces N rows here. Runtime reads never depend on this table.
create table if not exists market_history_ingest_raw (
  id bigserial primary key,
  request_id uuid not null,
  received_at timestamptz not null default now(),
  server smallint not null check (server between 1 and 3),
  observed_at timestamptz not null,
  location_id smallint not null check (location_id > 0),
  item_key text not null check (length(item_key) > 0),
  quality smallint not null check (quality between 1 and 5),
  bucket_at timestamptz not null,
  item_count bigint not null check (item_count >= 0),
  average_unit_price bigint check (average_unit_price > 0),
  constraint market_history_ingest_raw_request_fk
    foreign key (request_id)
    references market_history_ingest_requests (request_id)
    on delete restrict
);

create index if not exists market_history_ingest_raw_request_id_idx
  on market_history_ingest_raw (request_id);

-- Small hot read model. Newer captures replace older values for the same
-- logical time bucket; the primary key prevents duplicate history points.
create table if not exists market_history_buckets (
  server smallint not null check (server between 1 and 3),
  location_id smallint not null check (location_id > 0),
  item_key text not null check (length(item_key) > 0),
  quality smallint not null check (quality between 1 and 5),
  bucket_at timestamptz not null,
  item_count bigint not null check (item_count >= 0),
  average_unit_price bigint check (average_unit_price > 0),
  observed_at timestamptz not null,
  updated_at timestamptz not null default now(),
  primary key (server, location_id, item_key, quality, bucket_at)
);
