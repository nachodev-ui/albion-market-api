create table if not exists albion_pvp_events (
    server text not null,
    event_id bigint not null,
    occurred_at timestamptz not null,
    killer_id text not null,
    killer_name text not null,
    killer_guild_id text,
    killer_guild_name text,
    killer_alliance_id text,
    killer_alliance_name text,
    killer_item_power double precision not null default 0,
    killer_weapon_type text,
    killer_equipment jsonb not null default '{}'::jsonb,
    victim_id text not null,
    victim_name text not null,
    victim_guild_id text,
    victim_guild_name text,
    victim_alliance_id text,
    victim_alliance_name text,
    victim_item_power double precision not null default 0,
    victim_weapon_type text,
    victim_equipment jsonb not null default '{}'::jsonb,
    total_victim_kill_fame bigint not null default 0,
    participant_count integer not null default 0,
    group_member_count integer not null default 0,
    source text not null default 'gameinfo',
    first_seen_at timestamptz not null default now(),
    updated_at timestamptz not null default now(),
    primary key (server, event_id),
    constraint albion_pvp_events_server_check check (server in ('americas', 'europe', 'asia')),
    constraint albion_pvp_events_source_check check (source in ('gameinfo', 'murderledger', 'legacy')),
    constraint albion_pvp_events_counts_check check (participant_count >= 0 and group_member_count >= 0),
    constraint albion_pvp_events_player_ids_check check (char_length(killer_id) between 1 and 128 and char_length(victim_id) between 1 and 128)
);

create index if not exists albion_pvp_events_killer_recent_idx
    on albion_pvp_events (server, killer_id, occurred_at desc, event_id desc);

create index if not exists albion_pvp_events_victim_recent_idx
    on albion_pvp_events (server, victim_id, occurred_at desc, event_id desc);

create index if not exists albion_pvp_events_occurred_idx
    on albion_pvp_events (server, occurred_at desc, event_id desc);

create index if not exists albion_pvp_events_killer_weapon_idx
    on albion_pvp_events (server, killer_id, killer_weapon_type, occurred_at desc)
    where killer_weapon_type is not null;

create index if not exists albion_pvp_events_victim_weapon_idx
    on albion_pvp_events (server, victim_id, victim_weapon_type, occurred_at desc)
    where victim_weapon_type is not null;

create table if not exists albion_pvp_ingest_state (
    server text primary key,
    last_attempt_at timestamptz,
    last_success_at timestamptz,
    latest_event_at timestamptz,
    latest_event_id bigint,
    active_source text,
    consecutive_failures integer not null default 0,
    circuit_open_until timestamptz,
    last_error text,
    updated_at timestamptz not null default now(),
    constraint albion_pvp_ingest_state_server_check check (server in ('americas', 'europe', 'asia')),
    constraint albion_pvp_ingest_state_source_check check (active_source is null or active_source in ('gameinfo', 'murderledger')),
    constraint albion_pvp_ingest_state_failures_check check (consecutive_failures >= 0)
);

insert into albion_pvp_ingest_state (server)
values ('americas'), ('europe'), ('asia')
on conflict (server) do nothing;

-- Preserve the profile-local history already collected before the global event store.
insert into albion_pvp_events (
    server, event_id, occurred_at,
    killer_id, killer_name, killer_guild_id, killer_guild_name,
    killer_alliance_id, killer_alliance_name, killer_item_power,
    killer_weapon_type, killer_equipment,
    victim_id, victim_name, victim_guild_id, victim_guild_name,
    victim_alliance_id, victim_alliance_name, victim_item_power,
    victim_weapon_type, victim_equipment,
    total_victim_kill_fame, participant_count, group_member_count, source
)
select
    p.server,
    e.event_id,
    e.occurred_at,
    case when e.result = 'kill' then p.player_id else coalesce(e.opponent_id, 'unknown:' || e.event_id::text) end,
    case when e.result = 'kill' then p.player_name else e.opponent_name end,
    case when e.result = 'kill' then p.guild_id else null end,
    case when e.result = 'kill' then p.guild_name else e.opponent_guild end,
    case when e.result = 'kill' then p.alliance_id else null end,
    case when e.result = 'kill' then p.alliance_name else null end,
    case when e.result = 'kill' then e.player_item_power else e.opponent_item_power end,
    case when e.result = 'kill' then e.weapon_type else e.opponent_equipment->>'mainHand' end,
    case when e.result = 'kill' then e.player_equipment else e.opponent_equipment end,
    case when e.result = 'death' then p.player_id else coalesce(e.opponent_id, 'unknown:' || e.event_id::text) end,
    case when e.result = 'death' then p.player_name else e.opponent_name end,
    case when e.result = 'death' then p.guild_id else null end,
    case when e.result = 'death' then p.guild_name else e.opponent_guild end,
    case when e.result = 'death' then p.alliance_id else null end,
    case when e.result = 'death' then p.alliance_name else null end,
    case when e.result = 'death' then e.player_item_power else e.opponent_item_power end,
    case when e.result = 'death' then e.weapon_type else e.opponent_equipment->>'mainHand' end,
    case when e.result = 'death' then e.player_equipment else e.opponent_equipment end,
    e.kill_fame,
    e.participant_count,
    e.group_member_count,
    'legacy'
from albion_player_events e
join albion_player_profiles p on p.id = e.profile_id
where e.event_id > 0
on conflict (server, event_id) do nothing;

update app_schema_state
set version = greatest(version, 19), updated_at = now()
where singleton = true;
