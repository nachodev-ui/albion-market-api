insert into plan_entitlements (plan_code, entitlement_key, entitlement_value)
values
    ('free', 'black_market.analytics', 'false'::jsonb),
    ('pro', 'black_market.analytics', 'true'::jsonb)
on conflict (plan_code, entitlement_key)
do update set entitlement_value = excluded.entitlement_value;

update app_schema_state
set version = greatest(version, 15), updated_at = now()
where singleton = true;
