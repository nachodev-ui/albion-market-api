alter table subscriptions
    add column if not exists provider_variant_id text,
    add column if not exists provider_status text,
    add column if not exists provider_updated_at timestamptz;

alter table billing_webhook_events
    add column if not exists attempt_count integer not null default 0,
    add column if not exists last_attempt_at timestamptz;

alter table subscriptions
    drop constraint if exists subscriptions_provider_variant_length,
    drop constraint if exists subscriptions_provider_status_length;

alter table billing_webhook_events
    drop constraint if exists billing_webhook_events_attempt_count_nonnegative;

alter table subscriptions
    add constraint subscriptions_provider_variant_length
    check (provider_variant_id is null or length(provider_variant_id) between 1 and 80),
    add constraint subscriptions_provider_status_length
    check (provider_status is null or length(provider_status) between 1 and 80);

alter table billing_webhook_events
    add constraint billing_webhook_events_attempt_count_nonnegative
    check (attempt_count >= 0);

create index if not exists subscriptions_provider_customer_idx
    on subscriptions (provider, provider_customer_id)
    where provider_customer_id is not null;

create index if not exists billing_webhook_events_provider_attempt_idx
    on billing_webhook_events (provider, status, last_attempt_at desc);

update app_schema_state
set version = greatest(version, 9), updated_at = now()
where singleton = true;
