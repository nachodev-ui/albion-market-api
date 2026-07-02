-- market_ingest_raw is the durable append-only audit trail. Runtime price
-- reads are served by current_market_prices, so this wide lookup index only
-- adds write amplification to every ingest row and is not used by the API.
drop index if exists market_ingest_raw_lookup_idx;
