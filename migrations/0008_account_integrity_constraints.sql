alter table billing_webhook_events
    drop constraint if exists billing_webhook_events_status_check;

alter table subscriptions
    drop constraint if exists subscriptions_status_check;

alter table app_users
    add constraint app_users_auth_subject_length
    check (length(auth_subject) between 3 and 255),
    add constraint app_users_display_name_length
    check (display_name is null or length(display_name) <= 160),
    add constraint app_users_email_length
    check (email is null or length(email) <= 320);

alter table billing_webhook_events
    add constraint billing_webhook_events_error_length
    check (error_message is null or length(error_message) <= 2000),
    add constraint billing_webhook_events_event_id_length
    check (length(provider_event_id) between 1 and 255),
    add constraint billing_webhook_events_provider_length
    check (length(provider) between 1 and 40),
    add constraint billing_webhook_events_status_allowed
    check (status in ('pending', 'processed', 'failed', 'ignored')),
    add constraint billing_webhook_events_type_length
    check (length(event_type) between 1 and 160);

alter table plan_entitlements
    add constraint plan_entitlements_key_format
    check (entitlement_key ~ '^[a-z][a-z0-9_.]{2,127}$');

alter table plans
    add constraint plans_display_name_length
    check (length(display_name) between 1 and 80);

alter table subscriptions
    add constraint subscriptions_period_order
    check (
        current_period_start is null
        or current_period_end is null
        or current_period_end >= current_period_start
    ),
    add constraint subscriptions_provider_length
    check (length(provider) between 1 and 40),
    add constraint subscriptions_status_allowed
    check (status in ('trialing', 'active', 'past_due', 'canceled', 'expired'));

alter table user_entitlement_overrides
    add constraint user_entitlement_overrides_key_format
    check (entitlement_key ~ '^[a-z][a-z0-9_.]{2,127}$'),
    add constraint user_entitlement_overrides_reason_length
    check (reason is null or length(reason) <= 500);

update app_schema_state
set version = greatest(version, 8), updated_at = now()
where singleton = true;
