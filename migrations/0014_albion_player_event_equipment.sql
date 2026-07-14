alter table albion_player_events
    add column if not exists player_equipment jsonb not null default '{}'::jsonb,
    add column if not exists opponent_equipment jsonb not null default '{}'::jsonb;

update albion_player_events
set player_equipment = jsonb_build_object('mainHand', weapon_type)
where weapon_type is not null
  and player_equipment = '{}'::jsonb;

update app_schema_state
set version = greatest(version, 14), updated_at = now()
where singleton = true;
