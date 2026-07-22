create table if not exists player_economic_profiles (
    user_id uuid primary key references app_users(id) on delete cascade,
    server text not null,
    premium_active boolean not null default false,
    daily_focus_balance integer not null default 0,
    home_city text not null,
    guild_has_island boolean not null default false,
    sales_tax_rate numeric(6,3) not null default 0,
    transport_cost bigint not null default 0,
    specializations jsonb not null default '[]'::jsonb,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint player_economic_profiles_server check (
        server in ('americas', 'europe', 'asia')
    ),
    constraint player_economic_profiles_home_city check (
        home_city in (
            'bridgewatch', 'caerleon', 'fort-sterling', 'lymhurst',
            'martlock', 'thetford', 'brecilien'
        )
    ),
    constraint player_economic_profiles_focus_range check (
        daily_focus_balance between 0 and 100000
    ),
    constraint player_economic_profiles_sales_tax_range check (
        sales_tax_rate between 0 and 100
    ),
    constraint player_economic_profiles_transport_cost_range check (
        transport_cost between 0 and 1000000000000
    ),
    constraint player_economic_profiles_specializations_array check (
        jsonb_typeof(specializations) = 'array'
    )
);

update app_schema_state
set version = greatest(version, 17), updated_at = now()
where singleton = true;
