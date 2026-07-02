create table if not exists market_ingest_raw (
  id bigserial primary key,
  request_id uuid not null,
  received_at timestamptz not null default now(),
  server smallint not null,
  observed_at timestamptz not null,
  location_id smallint not null,
  item_key text not null,
  quality smallint not null,
  sell_price_min bigint,
  sell_price_min_at timestamptz,
  buy_price_max bigint,
  buy_price_max_at timestamptz
);

create index if not exists market_ingest_raw_request_id_idx
  on market_ingest_raw (request_id);

create index if not exists market_ingest_raw_lookup_idx
  on market_ingest_raw (server, location_id, item_key, quality, observed_at desc);

create table if not exists current_market_prices (
  server smallint not null,
  location_id smallint not null,
  item_key text not null,
  quality smallint not null,
  sell_price_min bigint,
  sell_price_min_at timestamptz,
  buy_price_max bigint,
  buy_price_max_at timestamptz,
  updated_at timestamptz not null default now(),
  primary key (server, location_id, item_key, quality)
);

create index if not exists current_market_prices_item_loc_idx
  on current_market_prices (item_key, location_id, quality, server);