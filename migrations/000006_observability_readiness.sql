-- Readiness uses a single explicit schema marker in addition to checking the
-- relations required by the API. Future schema migrations must update this row.
create table if not exists app_schema_state (
  singleton boolean primary key default true check (singleton),
  version integer not null check (version > 0),
  updated_at timestamptz not null default now()
);

insert into app_schema_state (singleton, version, updated_at)
values (true, 6, now())
on conflict (singleton)
do update
set
  version = greatest(app_schema_state.version, excluded.version),
  updated_at = now();
