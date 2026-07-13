alter table billing_webhook_events
    drop constraint if exists billing_webhook_events_status_check;

alter table subscriptions
    drop constraint if exists subscriptions_status_check;

do $$
begin
    if not exists (
        select 1 from pg_constraint
        where conrelid = 'app_users'::regclass
          and conname = 'app_users_auth_subject_length'
    ) then
        alter table app_users
            add constraint app_users_auth_subject_length
            check (length(auth_subject) between 3 and 255);
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'app_users'::regclass
          and conname = 'app_users_display_name_length'
    ) then
        alter table app_users
            add constraint app_users_display_name_length
            check (display_name is null or length(display_name) <= 160);
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'app_users'::regclass
          and conname = 'app_users_email_length'
    ) then
        alter table app_users
            add constraint app_users_email_length
            check (email is null or length(email) <= 320);
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'billing_webhook_events'::regclass
          and conname = 'billing_webhook_events_error_length'
    ) then
        alter table billing_webhook_events
            add constraint billing_webhook_events_error_length
            check (error_message is null or length(error_message) <= 2000);
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'billing_webhook_events'::regclass
          and conname = 'billing_webhook_events_event_id_length'
    ) then
        alter table billing_webhook_events
            add constraint billing_webhook_events_event_id_length
            check (length(provider_event_id) between 1 and 255);
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'billing_webhook_events'::regclass
          and conname = 'billing_webhook_events_provider_length'
    ) then
        alter table billing_webhook_events
            add constraint billing_webhook_events_provider_length
            check (length(provider) between 1 and 40);
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'billing_webhook_events'::regclass
          and conname = 'billing_webhook_events_status_allowed'
    ) then
        alter table billing_webhook_events
            add constraint billing_webhook_events_status_allowed
            check (status in ('pending', 'processed', 'failed', 'ignored'));
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'billing_webhook_events'::regclass
          and conname = 'billing_webhook_events_type_length'
    ) then
        alter table billing_webhook_events
            add constraint billing_webhook_events_type_length
            check (length(event_type) between 1 and 160);
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'plan_entitlements'::regclass
          and conname = 'plan_entitlements_key_format'
    ) then
        alter table plan_entitlements
            add constraint plan_entitlements_key_format
            check (entitlement_key ~ '^[a-z][a-z0-9_.]{2,127}$');
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'plans'::regclass
          and conname = 'plans_display_name_length'
    ) then
        alter table plans
            add constraint plans_display_name_length
            check (length(display_name) between 1 and 80);
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'subscriptions'::regclass
          and conname = 'subscriptions_period_order'
    ) then
        alter table subscriptions
            add constraint subscriptions_period_order
            check (
                current_period_start is null
                or current_period_end is null
                or current_period_end >= current_period_start
            );
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'subscriptions'::regclass
          and conname = 'subscriptions_provider_length'
    ) then
        alter table subscriptions
            add constraint subscriptions_provider_length
            check (length(provider) between 1 and 40);
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'subscriptions'::regclass
          and conname = 'subscriptions_status_allowed'
    ) then
        alter table subscriptions
            add constraint subscriptions_status_allowed
            check (status in ('trialing', 'active', 'past_due', 'canceled', 'expired'));
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'user_entitlement_overrides'::regclass
          and conname = 'user_entitlement_overrides_key_format'
    ) then
        alter table user_entitlement_overrides
            add constraint user_entitlement_overrides_key_format
            check (entitlement_key ~ '^[a-z][a-z0-9_.]{2,127}$');
    end if;

    if not exists (
        select 1 from pg_constraint
        where conrelid = 'user_entitlement_overrides'::regclass
          and conname = 'user_entitlement_overrides_reason_length'
    ) then
        alter table user_entitlement_overrides
            add constraint user_entitlement_overrides_reason_length
            check (reason is null or length(reason) <= 500);
    end if;
end
$$;

update app_schema_state
set version = greatest(version, 8), updated_at = now()
where singleton = true;
