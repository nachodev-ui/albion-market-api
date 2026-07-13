create table if not exists plans (
    code text primary key,
    display_name text not null,
    active boolean not null default true,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint plans_code_format check (code ~ '^[a-z][a-z0-9_]{1,31}$')
);

create table if not exists app_users (
    id uuid primary key default gen_random_uuid(),
    auth_subject text not null unique,
    email text,
    display_name text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    last_login_at timestamptz
);

create table if not exists subscriptions (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references app_users(id) on delete cascade,
    provider text not null,
    provider_customer_id text,
    provider_subscription_id text,
    plan_code text not null references plans(code),
    status text not null check (status in ('trialing','active','past_due','canceled','expired')),
    current_period_start timestamptz,
    current_period_end timestamptz,
    cancel_at_period_end boolean not null default false,
    access_until timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create unique index if not exists subscriptions_provider_subscription_unique
    on subscriptions (provider, provider_subscription_id)
    where provider_subscription_id is not null;
create index if not exists subscriptions_user_access_idx
    on subscriptions (user_id, status, access_until desc, updated_at desc);

create table if not exists plan_entitlements (
    plan_code text not null references plans(code) on delete cascade,
    entitlement_key text not null,
    entitlement_value jsonb not null,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    primary key (plan_code, entitlement_key)
);

create table if not exists user_entitlement_overrides (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references app_users(id) on delete cascade,
    entitlement_key text not null,
    entitlement_value jsonb not null,
    reason text,
    expires_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (user_id, entitlement_key)
);

create index if not exists user_entitlement_overrides_active_idx
    on user_entitlement_overrides (user_id, entitlement_key, expires_at);

create table if not exists billing_webhook_events (
    id uuid primary key default gen_random_uuid(),
    provider text not null,
    provider_event_id text not null,
    event_type text not null,
    payload_hash bytea not null,
    processed_at timestamptz,
    status text not null default 'pending' check (status in ('pending','processed','failed','ignored')),
    error_message text,
    created_at timestamptz not null default now(),
    unique (provider, provider_event_id)
);

create index if not exists billing_webhook_events_status_idx
    on billing_webhook_events (status, created_at);

insert into plans (code, display_name, active)
values ('free','Free',true), ('pro','Pro',true), ('guild','Guild',false)
on conflict (code) do update
set display_name=excluded.display_name, active=excluded.active, updated_at=now();

insert into plan_entitlements (plan_code, entitlement_key, entitlement_value)
values
('free','history.max_days','7'::jsonb),
('free','optimizer.liquidity','false'::jsonb),
('free','optimizer.batch_limit','5'::jsonb),
('free','saved_configurations.max','3'::jsonb),
('free','exports.csv','false'::jsonb),
('free','alerts.market.max','0'::jsonb),
('pro','history.max_days','28'::jsonb),
('pro','optimizer.liquidity','true'::jsonb),
('pro','optimizer.batch_limit','500'::jsonb),
('pro','saved_configurations.max','100'::jsonb),
('pro','exports.csv','true'::jsonb),
('pro','alerts.market.max','10'::jsonb)
on conflict (plan_code, entitlement_key) do update
set entitlement_value=excluded.entitlement_value, updated_at=now();

update app_schema_state
set version=greatest(version,7), updated_at=now()
where singleton=true;
