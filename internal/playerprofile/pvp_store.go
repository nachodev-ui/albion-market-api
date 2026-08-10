package playerprofile

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type IngestState struct {
	Server              Server
	LastAttemptAt       *time.Time
	LastSuccessAt       *time.Time
	LatestEventAt       *time.Time
	LatestEventID       *int64
	ActiveSource        *string
	ConsecutiveFailures int
	CircuitOpenUntil    *time.Time
	LastError           *string
}

func (r *PostgresRepository) UpsertPvPEvents(ctx context.Context, events []PvPEventRecord) (int64, error) {
	if len(events) == 0 {
		return 0, nil
	}
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, fmt.Errorf("begin PvP event upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const statement = `
		insert into albion_pvp_events (
			server, event_id, occurred_at,
			killer_id, killer_name, killer_guild_id, killer_guild_name,
			killer_alliance_id, killer_alliance_name, killer_item_power,
			killer_weapon_type, killer_equipment,
			victim_id, victim_name, victim_guild_id, victim_guild_name,
			victim_alliance_id, victim_alliance_name, victim_item_power,
			victim_weapon_type, victim_equipment,
			total_victim_kill_fame, participant_count, group_member_count, source
		) values (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb,
			$13,$14,$15,$16,$17,$18,$19,$20,$21::jsonb,$22,$23,$24,$25
		)
		on conflict (server, event_id) do update set
			occurred_at = excluded.occurred_at,
			killer_id = excluded.killer_id,
			killer_name = excluded.killer_name,
			killer_guild_id = excluded.killer_guild_id,
			killer_guild_name = excluded.killer_guild_name,
			killer_alliance_id = excluded.killer_alliance_id,
			killer_alliance_name = excluded.killer_alliance_name,
			killer_item_power = excluded.killer_item_power,
			killer_weapon_type = excluded.killer_weapon_type,
			killer_equipment = excluded.killer_equipment,
			victim_id = excluded.victim_id,
			victim_name = excluded.victim_name,
			victim_guild_id = excluded.victim_guild_id,
			victim_guild_name = excluded.victim_guild_name,
			victim_alliance_id = excluded.victim_alliance_id,
			victim_alliance_name = excluded.victim_alliance_name,
			victim_item_power = excluded.victim_item_power,
			victim_weapon_type = excluded.victim_weapon_type,
			victim_equipment = excluded.victim_equipment,
			total_victim_kill_fame = excluded.total_victim_kill_fame,
			participant_count = excluded.participant_count,
			group_member_count = excluded.group_member_count,
			source = excluded.source,
			updated_at = now()
		where albion_pvp_events.source <> 'gameinfo' or excluded.source = 'gameinfo'
	`

	batch := &pgx.Batch{}
	for _, event := range events {
		killerEquipment, err := json.Marshal(event.KillerEquipment)
		if err != nil {
			return 0, fmt.Errorf("encode killer equipment: %w", err)
		}
		victimEquipment, err := json.Marshal(event.VictimEquipment)
		if err != nil {
			return 0, fmt.Errorf("encode victim equipment: %w", err)
		}
		batch.Queue(statement,
			event.Server, event.EventID, event.OccurredAt.UTC(),
			event.KillerID, event.KillerName, event.KillerGuildID, event.KillerGuildName,
			event.KillerAllianceID, event.KillerAllianceName, event.KillerItemPower,
			event.KillerWeaponType, killerEquipment,
			event.VictimID, event.VictimName, event.VictimGuildID, event.VictimGuildName,
			event.VictimAllianceID, event.VictimAllianceName, event.VictimItemPower,
			event.VictimWeaponType, victimEquipment,
			event.TotalVictimKillFame, event.ParticipantCount, event.GroupMemberCount,
			event.Source,
		)
	}

	results := tx.SendBatch(ctx, batch)
	var affected int64
	for range events {
		tag, execErr := results.Exec()
		if execErr != nil {
			_ = results.Close()
			return 0, fmt.Errorf("upsert PvP event: %w", execErr)
		}
		affected += tag.RowsAffected()
	}
	if err := results.Close(); err != nil {
		return 0, fmt.Errorf("close PvP event batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit PvP event upsert: %w", err)
	}
	return affected, nil
}

func (r *PostgresRepository) HasPvPEvent(ctx context.Context, server Server, eventID int64) (bool, error) {
	var exists bool
	if err := r.db.QueryRow(ctx, `
		select exists(select 1 from albion_pvp_events where server = $1 and event_id = $2)
	`, server, eventID).Scan(&exists); err != nil {
		return false, fmt.Errorf("check PvP event: %w", err)
	}
	return exists, nil
}

func (r *PostgresRepository) GetIngestState(ctx context.Context, server Server) (IngestState, error) {
	var state IngestState
	state.Server = server
	if err := r.db.QueryRow(ctx, `
		select last_attempt_at, last_success_at, latest_event_at, latest_event_id,
			active_source, consecutive_failures, circuit_open_until, last_error
		from albion_pvp_ingest_state
		where server = $1
	`, server).Scan(
		&state.LastAttemptAt, &state.LastSuccessAt, &state.LatestEventAt, &state.LatestEventID,
		&state.ActiveSource, &state.ConsecutiveFailures, &state.CircuitOpenUntil, &state.LastError,
	); err != nil {
		return IngestState{}, fmt.Errorf("get PvP ingest state: %w", err)
	}
	return state, nil
}

func (r *PostgresRepository) MarkIngestSuccess(ctx context.Context, server Server, source string, attemptedAt time.Time, latestAt *time.Time, latestID *int64) error {
	_, err := r.db.Exec(ctx, `
		insert into albion_pvp_ingest_state (
			server, last_attempt_at, last_success_at, latest_event_at, latest_event_id,
			active_source, consecutive_failures, circuit_open_until, last_error, updated_at
		) values ($1,$2,$2,$3,$4,$5,0,null,null,now())
		on conflict (server) do update set
			last_attempt_at = excluded.last_attempt_at,
			last_success_at = excluded.last_success_at,
			latest_event_at = coalesce(excluded.latest_event_at, albion_pvp_ingest_state.latest_event_at),
			latest_event_id = coalesce(excluded.latest_event_id, albion_pvp_ingest_state.latest_event_id),
			active_source = excluded.active_source,
			consecutive_failures = 0,
			circuit_open_until = null,
			last_error = null,
			updated_at = now()
	`, server, attemptedAt.UTC(), latestAt, latestID, source)
	if err != nil {
		return fmt.Errorf("mark PvP ingest success: %w", err)
	}
	return nil
}

func (r *PostgresRepository) MarkIngestFailure(ctx context.Context, server Server, attemptedAt time.Time, failures int, circuitOpenUntil *time.Time, message string) error {
	message = strings.TrimSpace(message)
	if len(message) > 500 {
		message = message[:500]
	}
	_, err := r.db.Exec(ctx, `
		insert into albion_pvp_ingest_state (
			server, last_attempt_at, consecutive_failures, circuit_open_until, last_error, updated_at
		) values ($1,$2,$3,$4,$5,now())
		on conflict (server) do update set
			last_attempt_at = excluded.last_attempt_at,
			consecutive_failures = excluded.consecutive_failures,
			circuit_open_until = excluded.circuit_open_until,
			last_error = excluded.last_error,
			updated_at = now()
	`, server, attemptedAt.UTC(), failures, circuitOpenUntil, message)
	if err != nil {
		return fmt.Errorf("mark PvP ingest failure: %w", err)
	}
	return nil
}

func (r *PostgresRepository) ListLinkedPlayers(ctx context.Context, server Server, limit int) ([]LinkedPlayer, error) {
	if limit < 1 {
		limit = 1
	}
	rows, err := r.db.Query(ctx, `
		select user_id::text, server, player_id
		from albion_player_profiles
		where server = $1
		order by updated_at desc
		limit $2
	`, server, limit)
	if err != nil {
		return nil, fmt.Errorf("list linked Albion players: %w", err)
	}
	defer rows.Close()
	players := make([]LinkedPlayer, 0, limit)
	for rows.Next() {
		var player LinkedPlayer
		if err := rows.Scan(&player.UserID, &player.Server, &player.PlayerID); err != nil {
			return nil, fmt.Errorf("scan linked Albion player: %w", err)
		}
		players = append(players, player)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate linked Albion players: %w", err)
	}
	return players, nil
}

func (r *PostgresRepository) ListIdentityRefreshCandidates(ctx context.Context, staleBefore time.Time, limit int) ([]LinkedPlayer, error) {
	if limit < 1 {
		limit = 1
	}
	rows, err := r.db.Query(ctx, `
		select user_id::text, server, player_id
		from albion_player_profiles
		where last_refreshed_at is null or last_refreshed_at < $1
		order by last_refreshed_at asc nulls first
		limit $2
	`, staleBefore.UTC(), limit)
	if err != nil {
		return nil, fmt.Errorf("list identity refresh candidates: %w", err)
	}
	defer rows.Close()
	players := make([]LinkedPlayer, 0, limit)
	for rows.Next() {
		var player LinkedPlayer
		if err := rows.Scan(&player.UserID, &player.Server, &player.PlayerID); err != nil {
			return nil, fmt.Errorf("scan identity refresh candidate: %w", err)
		}
		players = append(players, player)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate identity refresh candidates: %w", err)
	}
	return players, nil
}

func (r *PostgresRepository) UpdateIdentity(ctx context.Context, userID string, player Player, refreshedAt time.Time) error {
	result, err := r.db.Exec(ctx, `
		update albion_player_profiles
		set player_name = $2,
			guild_id = $3,
			guild_name = $4,
			alliance_id = $5,
			alliance_name = $6,
			avatar = $7,
			avatar_ring = $8,
			kill_fame = $9,
			death_fame = $10,
			fame_ratio = $11,
			last_refreshed_at = $12,
			last_refresh_attempt_at = $12,
			last_refresh_status = 'ok',
			last_refresh_error = null,
			updated_at = now()
		where user_id = $1::uuid and server = $13 and player_id = $14
	`, userID, player.PlayerName, player.GuildID, player.GuildName,
		player.AllianceID, player.AllianceName, player.Avatar, player.AvatarRing,
		player.KillFame, player.DeathFame, player.FameRatio, refreshedAt.UTC(),
		player.Server, player.PlayerID)
	if err != nil {
		return fmt.Errorf("update Albion identity: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotLinked
	}
	return nil
}
