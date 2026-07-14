create table if not exists albion_player_profiles (
    id uuid primary key default gen_random_uuid(),
    user_id uuid not null references app_users(id) on delete cascade,
    server text not null,
    player_id text not null,
    player_name text not null,
    guild_id text,
    guild_name text,
    alliance_id text,
    alliance_name text,
    avatar text,
    avatar_ring text,
    verification_status text not null default 'unverified',
    kill_fame bigint not null default 0,
    death_fame bigint not null default 0,
    fame_ratio double precision not null default 0,
    linked_at timestamptz not null default now(),
    last_refreshed_at timestamptz,
    last_refresh_attempt_at timestamptz,
    last_refresh_status text not null default 'pending',
    last_refresh_error text,
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    constraint albion_player_profiles_user_unique unique (user_id),
    constraint albion_player_profiles_server_check check (server in ('americas', 'europe', 'asia')),
    constraint albion_player_profiles_verification_check check (verification_status in ('unverified', 'verified')),
    constraint albion_player_profiles_refresh_status_check check (last_refresh_status in ('pending', 'ok', 'error')),
    constraint albion_player_profiles_player_id_length check (char_length(player_id) between 1 and 128),
    constraint albion_player_profiles_player_name_length check (char_length(player_name) between 1 and 64)
);

create index if not exists albion_player_profiles_server_player_idx
    on albion_player_profiles (server, player_id);

create index if not exists albion_player_profiles_updated_idx
    on albion_player_profiles (updated_at desc);

create table if not exists albion_player_events (
    id bigint generated always as identity primary key,
    profile_id uuid not null references albion_player_profiles(id) on delete cascade,
    event_id bigint not null,
    occurred_at timestamptz not null,
    result text not null,
    opponent_id text,
    opponent_name text not null,
    opponent_guild text,
    kill_fame bigint not null default 0,
    player_item_power double precision not null default 0,
    opponent_item_power double precision not null default 0,
    weapon_type text,
    participant_count integer not null default 0,
    group_member_count integer not null default 0,
    created_at timestamptz not null default now(),
    constraint albion_player_events_result_check check (result in ('kill', 'death')),
    constraint albion_player_events_unique unique (profile_id, event_id, result),
    constraint albion_player_events_counts_check check (participant_count >= 0 and group_member_count >= 0)
);

create index if not exists albion_player_events_profile_occurred_idx
    on albion_player_events (profile_id, occurred_at desc);

update app_schema_state
set version = greatest(version, 13), updated_at = now()
where singleton = true;
