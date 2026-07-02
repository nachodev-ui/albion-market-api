select jsonb_build_object(
  'database_name', current_database(),
  'database_user', current_user,
  'server_version', current_setting('server_version'),
  'server_version_num', current_setting('server_version_num')::integer,
  'captured_at_utc', to_char(
    clock_timestamp() at time zone 'UTC',
    'YYYY-MM-DD"T"HH24:MI:SS.US"Z"'
  ),
  'schema', jsonb_build_object(
    'required_table_count', (
      select count(*)
      from pg_class c
      join pg_namespace n on n.oid = c.relnamespace
      where n.nspname = 'public'
        and c.relkind in ('r', 'p')
        and c.relname = any(array[
          'current_market_prices',
          'market_history_buckets',
          'market_history_ingest_raw',
          'market_history_ingest_requests',
          'market_ingest_raw',
          'market_ingest_requests'
        ])
    ),
    'primary_key_count', (
      select count(*)
      from pg_constraint con
      join pg_class c on c.oid = con.conrelid
      join pg_namespace n on n.oid = c.relnamespace
      where n.nspname = 'public'
        and con.contype = 'p'
        and c.relname = any(array[
          'current_market_prices',
          'market_history_buckets',
          'market_history_ingest_raw',
          'market_history_ingest_requests',
          'market_ingest_raw',
          'market_ingest_requests'
        ])
    ),
    'foreign_key_count', (
      select count(*)
      from pg_constraint con
      join pg_class c on c.oid = con.conrelid
      join pg_namespace n on n.oid = c.relnamespace
      where n.nspname = 'public'
        and con.contype = 'f'
        and c.relname = any(array[
          'current_market_prices',
          'market_history_buckets',
          'market_history_ingest_raw',
          'market_history_ingest_requests',
          'market_ingest_raw',
          'market_ingest_requests'
        ])
    ),
    'index_count', (
      select count(*)
      from pg_indexes
      where schemaname = 'public'
        and tablename = any(array[
          'current_market_prices',
          'market_history_buckets',
          'market_history_ingest_raw',
          'market_history_ingest_requests',
          'market_ingest_raw',
          'market_ingest_requests'
        ])
    )
  ),
  'table_counts', jsonb_build_object(
    'current_market_prices', (select count(*) from public.current_market_prices),
    'market_history_buckets', (select count(*) from public.market_history_buckets),
    'market_history_ingest_raw', (select count(*) from public.market_history_ingest_raw),
    'market_history_ingest_requests', (select count(*) from public.market_history_ingest_requests),
    'market_ingest_raw', (select count(*) from public.market_ingest_raw),
    'market_ingest_requests', (select count(*) from public.market_ingest_requests)
  ),
  'query_checks', jsonb_build_object(
    'current_price_lookup_rows', (
      with sample as (
        select server, location_id, item_key, quality
        from public.current_market_prices
        order by server, location_id, item_key, quality
        limit 1
      )
      select count(*)
      from public.current_market_prices p
      join sample s
        on p.server = s.server
       and p.location_id = s.location_id
       and p.item_key = s.item_key
       and p.quality = s.quality
    ),
    'history_lookup_rows', (
      with sample as (
        select server, location_id, item_key, quality, min(bucket_at) as range_start
        from public.market_history_buckets
        group by server, location_id, item_key, quality
        order by server, location_id, item_key, quality
        limit 1
      )
      select count(*)
      from public.market_history_buckets h
      join sample s
        on h.server = s.server
       and h.location_id = s.location_id
       and h.item_key = s.item_key
       and h.quality = s.quality
       and h.bucket_at >= s.range_start
       and h.bucket_at < s.range_start + interval '367 days'
    ),
    'market_requests_completed', (
      select count(*) from public.market_ingest_requests where status = 'completed'
    ),
    'market_requests_processing', (
      select count(*) from public.market_ingest_requests where status = 'processing'
    ),
    'history_requests_completed', (
      select count(*) from public.market_history_ingest_requests where status = 'completed'
    ),
    'history_requests_processing', (
      select count(*) from public.market_history_ingest_requests where status = 'processing'
    ),
    'market_raw_sequence_valid', (
      select coalesce(
        (select last_value from public.market_ingest_raw_id_seq) >=
        (select coalesce(max(id), 0) from public.market_ingest_raw),
        false
      )
    ),
    'history_raw_sequence_valid', (
      select coalesce(
        (select last_value from public.market_history_ingest_raw_id_seq) >=
        (select coalesce(max(id), 0) from public.market_history_ingest_raw),
        false
      )
    )
  )
)::text;
