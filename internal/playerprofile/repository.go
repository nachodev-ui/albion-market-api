package playerprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotLinked = errors.New("albion player profile is not linked")

type Snapshot struct {
	Profile Profile
	Events  []Event
}

type Repository interface {
	Get(context.Context, string) (Snapshot, error)
	Save(context.Context, string, Player, []Event, time.Time) (Snapshot, error)
	Delete(context.Context, string) error
	MarkRefreshFailure(context.Context, string, time.Time, string) error
}

type PostgresRepository struct{ db *pgxpool.Pool }

func NewPostgresRepository(db *pgxpool.Pool) (*PostgresRepository, error) {
	if db == nil {
		return nil, errors.New("database pool is required")
	}
	return &PostgresRepository{db: db}, nil
}

func (r *PostgresRepository) Get(ctx context.Context, userID string) (Snapshot, error) {
	const profileQuery = `
		select id::text, server, player_id, player_name, guild_id, guild_name,
			alliance_id, alliance_name, avatar, avatar_ring, verification_status,
			kill_fame, death_fame, fame_ratio, linked_at, last_refreshed_at,
			last_refresh_attempt_at, last_refresh_status, last_refresh_error
		from albion_player_profiles
		where user_id = $1::uuid
	`
	var profile Profile
	if err := r.db.QueryRow(ctx, profileQuery, userID).Scan(
		&profile.ID, &profile.Server, &profile.PlayerID, &profile.PlayerName,
		&profile.GuildID, &profile.GuildName, &profile.AllianceID, &profile.AllianceName,
		&profile.Avatar, &profile.AvatarRing, &profile.VerificationStatus,
		&profile.KillFame, &profile.DeathFame, &profile.FameRatio, &profile.LinkedAt,
		&profile.LastRefreshedAt, &profile.LastRefreshAttempt, &profile.LastRefreshStatus,
		&profile.LastRefreshError,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Snapshot{}, ErrNotLinked
		}
		return Snapshot{}, fmt.Errorf("get Albion profile: %w", err)
	}
	profile.IdentityRefreshedAt = profile.LastRefreshedAt

	const freshnessQuery = `
		select last_success_at, active_source
		from albion_pvp_ingest_state
		where server = $1
	`
	if err := r.db.QueryRow(ctx, freshnessQuery, profile.Server).Scan(
		&profile.ActivityRefreshedAt, &profile.ActivitySource,
	); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, fmt.Errorf("get Albion PvP freshness: %w", err)
	}

	const eventsQuery = `
		select
			event_id,
			occurred_at,
			case when killer_id = $2 then 'kill' else 'death' end,
			case when killer_id = $2 then victim_id else killer_id end,
			case when killer_id = $2 then victim_name else killer_name end,
			case when killer_id = $2 then victim_guild_name else killer_guild_name end,
			total_victim_kill_fame,
			case when killer_id = $2 then killer_item_power else victim_item_power end,
			case when killer_id = $2 then victim_item_power else killer_item_power end,
			case when killer_id = $2 then killer_weapon_type else victim_weapon_type end,
			case when killer_id = $2 then killer_equipment else victim_equipment end::text,
			case when killer_id = $2 then victim_equipment else killer_equipment end::text,
			participant_count,
			group_member_count
		from albion_pvp_events
		where server = $1
		  and (killer_id = $2 or victim_id = $2)
		order by occurred_at desc, event_id desc
		limit 50
	`
	rows, err := r.db.Query(ctx, eventsQuery, profile.Server, profile.PlayerID)
	if err != nil {
		return Snapshot{}, fmt.Errorf("get Albion PvP events: %w", err)
	}
	defer rows.Close()
	events := make([]Event, 0, 50)
	for rows.Next() {
		var event Event
		var playerEquipmentJSON string
		var opponentEquipmentJSON string
		if err := rows.Scan(
			&event.EventID, &event.OccurredAt, &event.Result, &event.OpponentID,
			&event.OpponentName, &event.OpponentGuild, &event.KillFame,
			&event.PlayerItemPower, &event.OpponentItemPower, &event.WeaponType,
			&playerEquipmentJSON, &opponentEquipmentJSON,
			&event.ParticipantCount, &event.GroupMemberCount,
		); err != nil {
			return Snapshot{}, fmt.Errorf("scan Albion PvP event: %w", err)
		}
		if err := json.Unmarshal([]byte(playerEquipmentJSON), &event.PlayerEquipment); err != nil {
			return Snapshot{}, fmt.Errorf("decode player equipment: %w", err)
		}
		if err := json.Unmarshal([]byte(opponentEquipmentJSON), &event.OpponentEquipment); err != nil {
			return Snapshot{}, fmt.Errorf("decode opponent equipment: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("iterate Albion PvP events: %w", err)
	}
	return Snapshot{Profile: profile, Events: events}, nil
}
