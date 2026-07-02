\pset pager off
\pset border 2
\pset null '(null)'
\timing on

begin transaction read only;

\echo '=== Identidad y configuración ==='
select
  current_database() as database_name,
  current_user as database_user,
  version() as postgres_version,
  current_setting('server_version_num') as server_version_num,
  current_setting('block_size') as block_size_bytes,
  current_setting('autovacuum') as autovacuum,
  current_setting('track_counts') as track_counts,
  current_setting('default_transaction_isolation') as default_isolation;

select
  datname as database_name,
  stats_reset
from pg_stat_database
where datname = current_database();

\echo '=== Tamaño y estadísticas de tablas ==='
select
  s.relname as table_name,
  pg_size_pretty(pg_total_relation_size(s.relid)) as total_size,
  pg_size_pretty(pg_relation_size(s.relid)) as heap_size,
  pg_size_pretty(pg_indexes_size(s.relid)) as indexes_size,
  s.n_live_tup as estimated_live_rows,
  s.n_dead_tup as estimated_dead_rows,
  s.seq_scan,
  s.idx_scan,
  s.n_tup_ins,
  s.n_tup_upd,
  s.n_tup_del,
  s.last_analyze,
  s.last_autoanalyze,
  s.last_vacuum,
  s.last_autovacuum,
  case
    when s.n_live_tup > 0
      then round(pg_total_relation_size(s.relid)::numeric / s.n_live_tup, 2)
    else null
  end as approx_total_bytes_per_live_row
from pg_stat_user_tables s
where s.relname in (
  'market_ingest_raw',
  'market_ingest_requests',
  'current_market_prices',
  'market_history_ingest_raw',
  'market_history_ingest_requests',
  'market_history_buckets'
)
order by pg_total_relation_size(s.relid) desc;

\echo '=== Opciones específicas de almacenamiento/autovacuum ==='
select
  c.oid::regclass as table_name,
  c.reloptions
from pg_class c
join pg_namespace n on n.oid = c.relnamespace
where n.nspname = 'public'
  and c.relname in (
    'market_ingest_raw',
    'market_ingest_requests',
    'current_market_prices',
    'market_history_ingest_raw',
    'market_history_ingest_requests',
    'market_history_buckets'
  )
order by c.relname;

\echo '=== Columnas ==='
select
  c.table_name,
  c.ordinal_position,
  c.column_name,
  c.data_type,
  c.udt_name,
  c.is_nullable,
  c.column_default
from information_schema.columns c
where c.table_schema = 'public'
  and c.table_name in (
    'market_ingest_raw',
    'market_ingest_requests',
    'current_market_prices',
    'market_history_ingest_raw',
    'market_history_ingest_requests',
    'market_history_buckets'
  )
order by c.table_name, c.ordinal_position;

\echo '=== Restricciones ==='
select
  conrelid::regclass as table_name,
  conname as constraint_name,
  case contype
    when 'p' then 'PRIMARY KEY'
    when 'f' then 'FOREIGN KEY'
    when 'u' then 'UNIQUE'
    when 'c' then 'CHECK'
    when 'x' then 'EXCLUSION'
    else contype::text
  end as constraint_type,
  pg_get_constraintdef(oid, true) as definition
from pg_constraint
where connamespace = 'public'::regnamespace
  and conrelid::regclass::text in (
    'market_ingest_raw',
    'market_ingest_requests',
    'current_market_prices',
    'market_history_ingest_raw',
    'market_history_ingest_requests',
    'market_history_buckets'
  )
order by conrelid::regclass::text, constraint_type, constraint_name;

\echo '=== Índices, tamaños y uso acumulado ==='
select
  ui.relname as table_name,
  ui.indexrelname as index_name,
  pg_size_pretty(pg_relation_size(ui.indexrelid)) as index_size,
  ui.idx_scan,
  ui.idx_tup_read,
  ui.idx_tup_fetch,
  pg_get_indexdef(ui.indexrelid) as definition
from pg_stat_user_indexes ui
where ui.relname in (
  'market_ingest_raw',
  'market_ingest_requests',
  'current_market_prices',
  'market_history_ingest_raw',
  'market_history_ingest_requests',
  'market_history_buckets'
)
order by ui.relname, ui.indexrelname;

\echo '=== Índices exactamente equivalentes por definición interna ==='
with index_signatures as (
  select
    i.indrelid,
    c.relname as table_name,
    ic.relname as index_name,
    i.indisunique,
    i.indisprimary,
    i.indkey::text as indkey,
    i.indclass::text as indclass,
    i.indcollation::text as indcollation,
    i.indoption::text as indoption,
    coalesce(pg_get_expr(i.indexprs, i.indrelid), '') as expressions,
    coalesce(pg_get_expr(i.indpred, i.indrelid), '') as predicate
  from pg_index i
  join pg_class c on c.oid = i.indrelid
  join pg_class ic on ic.oid = i.indexrelid
  join pg_namespace n on n.oid = c.relnamespace
  where n.nspname = 'public'
    and c.relname in (
      'market_ingest_raw',
      'market_ingest_requests',
      'current_market_prices',
      'market_history_ingest_raw',
      'market_history_ingest_requests',
      'market_history_buckets'
    )
)
select
  table_name,
  string_agg(index_name, ', ' order by index_name) as equivalent_indexes
from index_signatures
group by
  indrelid,
  table_name,
  indisunique,
  indisprimary,
  indkey,
  indclass,
  indcollation,
  indoption,
  expressions,
  predicate
having count(*) > 1
order by table_name;

\echo '=== Particionamiento actual ==='
select
  child.oid::regclass as partition_name,
  parent.oid::regclass as parent_table,
  pg_get_expr(child.relpartbound, child.oid) as partition_bound
from pg_inherits inh
join pg_class parent on parent.oid = inh.inhparent
join pg_class child on child.oid = inh.inhrelid
join pg_namespace n on n.oid = parent.relnamespace
where n.nspname = 'public'
  and parent.relname in (
    'market_ingest_raw',
    'market_history_ingest_raw',
    'market_ingest_requests',
    'market_history_ingest_requests',
    'market_history_buckets'
  )
order by parent.relname, child.relname;

\echo '=== Estado de requests de idempotencia (agregado) ==='
select to_regclass('public.market_ingest_requests') is not null as has_price_requests \gset
select to_regclass('public.market_history_ingest_requests') is not null as has_history_requests \gset

\if :has_price_requests
select 'market_ingest_requests' as table_name, status, count(*) as rows
from market_ingest_requests
group by status
order by status;
\else
\echo 'Falta public.market_ingest_requests.'
\endif

\if :has_history_requests
select 'market_history_ingest_requests' as table_name, status, count(*) as rows
from market_history_ingest_requests
group by status
order by status;
\else
\echo 'Falta public.market_history_ingest_requests.'
\endif

\if :include_exact_counts
\echo '=== Conteos exactos solicitados: pueden escanear tablas completas ==='
select 'market_ingest_raw' as table_name, count(*) as exact_rows from market_ingest_raw
union all
select 'market_ingest_requests', count(*) from market_ingest_requests
union all
select 'current_market_prices', count(*) from current_market_prices
union all
select 'market_history_ingest_raw', count(*) from market_history_ingest_raw
union all
select 'market_history_ingest_requests', count(*) from market_history_ingest_requests
union all
select 'market_history_buckets', count(*) from market_history_buckets
order by table_name;

\echo '=== Cobertura temporal exacta solicitada ==='
select
  'market_ingest_raw' as table_name,
  min(received_at) as oldest_timestamp,
  max(received_at) as newest_timestamp
from market_ingest_raw
union all
select
  'market_ingest_requests',
  min(created_at),
  max(created_at)
from market_ingest_requests
union all
select
  'current_market_prices',
  min(updated_at),
  max(updated_at)
from current_market_prices
union all
select
  'market_history_ingest_raw',
  min(received_at),
  max(received_at)
from market_history_ingest_raw
union all
select
  'market_history_ingest_requests',
  min(created_at),
  max(created_at)
from market_history_ingest_requests
union all
select
  'market_history_buckets',
  min(bucket_at),
  max(bucket_at)
from market_history_buckets
order by table_name;
\else
\echo 'Conteos y rangos exactos omitidos. Use -IncludeExactCounts solo fuera de horario crítico.'
\endif

commit;
