create table if not exists market_ingest_requests (
  request_id uuid primary key,
  request_sha256 bytea not null,
  server smallint not null,
  accepted_count integer not null,
  current_rows_touched bigint not null default 0,
  status text not null check (status in ('processing', 'completed')),
  created_at timestamptz not null default now(),
  completed_at timestamptz
);

create index if not exists market_ingest_requests_created_at_idx
  on market_ingest_requests (created_at desc);
