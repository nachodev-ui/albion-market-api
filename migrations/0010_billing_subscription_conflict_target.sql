create unique index if not exists subscriptions_provider_subscription_conflict_unique
    on subscriptions (provider, provider_subscription_id);

update app_schema_state
set version = greatest(version, 10), updated_at = now()
where singleton = true;
