begin;

alter table subscriptions
    add column if not exists provider_order_id text;

alter table subscriptions
    drop constraint if exists subscriptions_provider_order_length;

alter table subscriptions
    add constraint subscriptions_provider_order_length
    check (provider_order_id is null or length(provider_order_id) between 1 and 128);

create index if not exists subscriptions_provider_order_idx
    on subscriptions (provider, provider_order_id)
    where provider_order_id is not null;

alter table billing_webhook_events
    add column if not exists raw_payload jsonb not null default '{}'::jsonb,
    add column if not exists object_type text,
    add column if not exists object_id text,
    add column if not exists delivery_count integer not null default 1,
    add column if not exists last_received_at timestamptz not null default now(),
    add column if not exists next_attempt_at timestamptz,
    add column if not exists locked_at timestamptz,
    add column if not exists locked_by text,
    add column if not exists dead_letter_at timestamptz;

alter table billing_webhook_events
    drop constraint if exists billing_webhook_events_status_check,
    drop constraint if exists billing_webhook_events_delivery_count_positive,
    drop constraint if exists billing_webhook_events_raw_payload_object,
    drop constraint if exists billing_webhook_events_object_type_length,
    drop constraint if exists billing_webhook_events_object_id_length,
    drop constraint if exists billing_webhook_events_locked_by_length;

alter table billing_webhook_events
    add constraint billing_webhook_events_status_check
    check (status in ('pending','processing','processed','failed','ignored','dead_letter')),
    add constraint billing_webhook_events_delivery_count_positive
    check (delivery_count >= 1),
    add constraint billing_webhook_events_raw_payload_object
    check (jsonb_typeof(raw_payload) = 'object'),
    add constraint billing_webhook_events_object_type_length
    check (object_type is null or length(object_type) between 1 and 80),
    add constraint billing_webhook_events_object_id_length
    check (object_id is null or length(object_id) between 1 and 128),
    add constraint billing_webhook_events_locked_by_length
    check (locked_by is null or length(locked_by) between 1 and 160);

update billing_webhook_events
set next_attempt_at = coalesce(next_attempt_at, created_at),
    last_received_at = coalesce(last_received_at, created_at),
    delivery_count = greatest(delivery_count, 1)
where status in ('pending', 'failed');

create index if not exists billing_webhook_events_queue_idx
    on billing_webhook_events (provider, status, next_attempt_at, created_at)
    where status in ('pending', 'failed', 'processing');

create index if not exists billing_webhook_events_dead_letter_idx
    on billing_webhook_events (provider, dead_letter_at desc)
    where status = 'dead_letter';

create table if not exists billing_orders (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references app_users(id) on delete cascade,
    provider text not null,
    provider_order_id text not null,
    provider_customer_id text,
    provider_variant_id text,
    provider_status text not null,
    provider_updated_at timestamptz,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (provider, provider_order_id),
    constraint billing_orders_provider_length check (length(provider) between 1 and 40),
    constraint billing_orders_order_id_length check (length(provider_order_id) between 1 and 128),
    constraint billing_orders_customer_id_length check (provider_customer_id is null or length(provider_customer_id) between 1 and 128),
    constraint billing_orders_variant_id_length check (provider_variant_id is null or length(provider_variant_id) between 1 and 80),
    constraint billing_orders_status_length check (length(provider_status) between 1 and 80)
);

create index if not exists billing_orders_user_idx
    on billing_orders (user_id, updated_at desc);

create table if not exists billing_notification_outbox (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references app_users(id) on delete cascade,
    provider text not null,
    provider_event_id text not null,
    notification_type text not null,
    payload jsonb not null default '{}'::jsonb,
    status text not null default 'pending',
    attempt_count integer not null default 0,
    next_attempt_at timestamptz,
    locked_at timestamptz,
    locked_by text,
    sent_at timestamptz,
    error_code text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    unique (provider, provider_event_id, notification_type),
    constraint billing_notification_outbox_status_check
        check (status in ('pending','sending','sent','failed','dead_letter')),
    constraint billing_notification_outbox_attempt_nonnegative check (attempt_count >= 0),
    constraint billing_notification_outbox_payload_object check (jsonb_typeof(payload) = 'object'),
    constraint billing_notification_outbox_type_length check (length(notification_type) between 1 and 80),
    constraint billing_notification_outbox_error_length check (error_code is null or length(error_code) between 1 and 160)
);

create index if not exists billing_notification_outbox_queue_idx
    on billing_notification_outbox (status, next_attempt_at, created_at)
    where status in ('pending', 'failed', 'sending');

update app_schema_state
set version = greatest(version, 18), updated_at = now()
where singleton = true;

commit;
