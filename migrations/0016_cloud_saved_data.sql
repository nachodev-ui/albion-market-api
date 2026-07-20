create table if not exists saved_presets (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references app_users(id) on delete cascade,
    name text not null,
    payload jsonb not null,
    is_default boolean not null default false,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint saved_presets_name_length check (char_length(btrim(name)) between 1 and 80),
    constraint saved_presets_payload_object check (jsonb_typeof(payload) = 'object')
);

create unique index if not exists saved_presets_user_name_unique
    on saved_presets (user_id, lower(btrim(name)));

create unique index if not exists saved_presets_one_default_per_user
    on saved_presets (user_id)
    where is_default;

create index if not exists saved_presets_user_updated_idx
    on saved_presets (user_id, is_default desc, updated_at desc);

create table if not exists saved_calculations (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references app_users(id) on delete cascade,
    name text,
    kind text not null default 'craft',
    snapshot jsonb not null,
    created_at timestamptz not null default now(),
    constraint saved_calculations_name_length check (
        name is null or char_length(btrim(name)) between 1 and 120
    ),
    constraint saved_calculations_kind_format check (kind ~ '^[a-z][a-z0-9_-]{0,31}$'),
    constraint saved_calculations_snapshot_object check (jsonb_typeof(snapshot) = 'object')
);

create index if not exists saved_calculations_user_created_idx
    on saved_calculations (user_id, created_at desc);

insert into plan_entitlements (plan_code, entitlement_key, entitlement_value)
values
    ('free', 'saved_calculations.max', '20'::jsonb),
    ('pro', 'saved_calculations.max', '250'::jsonb),
    ('guild', 'saved_calculations.max', '1000'::jsonb)
on conflict (plan_code, entitlement_key)
do update set
    entitlement_value = excluded.entitlement_value,
    updated_at = now();

update app_schema_state
set version = greatest(version, 16), updated_at = now()
where singleton = true;
