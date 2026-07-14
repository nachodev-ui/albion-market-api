package playerprofile

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

func (r *PostgresRepository) Save(ctx context.Context, userID string, player Player, events []Event, refreshedAt time.Time) (Snapshot, error) {
	tx, err := r.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return Snapshot{}, fmt.Errorf("begin Albion profile save: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	const upsert = `
		insert into albion_player_profiles (
			user_id, server, player_id, player_name, guild_id, guild_name,
			alliance_id, alliance_name, avatar, avatar_ring, verification_status,
			kill_fame, death_fame, fame_ratio, linked_at, last_refreshed_at,
			last_refresh_attempt_at, last_refresh_status, last_refresh_error, updated_at
		) values (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'unverified',
			$11, $12, $13, now(), $14, $14, 'ok', null, now()
		)
		on conflict (user_id) do update set
			server = excluded.server,
			player_id = excluded.player_id,
			player_name = excluded.player_name,
			guild_id = excluded.guild_id,
			guild_name = excluded.guild_name,
			alliance_id = excluded.alliance_id,
			alliance_name = excluded.alliance_name,
			avatar = excluded.avatar,
			avatar_ring = excluded.avatar_ring,
			verification_status = 'unverified',
			kill_fame = excluded.kill_fame,
			death_fame = excluded.death_fame,
			fame_ratio = excluded.fame_ratio,
			last_refreshed_at = excluded.last_refreshed_at,
			last_refresh_attempt_at = excluded.last_refresh_attempt_at,
			last_refresh_status = 'ok',
			last_refresh_error = null,
			updated_at = now()
		returning id::text
	`
	var profileID string
	if err := tx.QueryRow(ctx, upsert,
		userID, player.Server, player.PlayerID, player.PlayerName, player.GuildID,
		player.GuildName, player.AllianceID, player.AllianceName, player.Avatar,
		player.AvatarRing, player.KillFame, player.DeathFame, player.FameRatio,
		refreshedAt.UTC(),
	).Scan(&profileID); err != nil {
		return Snapshot{}, fmt.Errorf("upsert Albion profile: %w", err)
	}
	if _, err := tx.Exec(ctx, `delete from albion_player_events where profile_id = $1::uuid`, profileID); err != nil {
		return Snapshot{}, fmt.Errorf("clear Albion events: %w", err)
	}
	const insertEvent = `
		insert into albion_player_events (
			profile_id, event_id, occurred_at, result, opponent_id, opponent_name,
			opponent_guild, kill_fame, player_item_power, opponent_item_power,
			weapon_type, participant_count, group_member_count
		) values ($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)
		on conflict (profile_id, event_id, result) do nothing
	`
	for _, event := range events {
		if _, err := tx.Exec(ctx, insertEvent, profileID, event.EventID, event.OccurredAt,
			event.Result, event.OpponentID, event.OpponentName, event.OpponentGuild,
			event.KillFame, event.PlayerItemPower, event.OpponentItemPower,
			event.WeaponType, event.ParticipantCount, event.GroupMemberCount); err != nil {
			return Snapshot{}, fmt.Errorf("insert Albion event: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return Snapshot{}, fmt.Errorf("commit Albion profile save: %w", err)
	}
	return r.Get(ctx, userID)
}

func (r *PostgresRepository) Delete(ctx context.Context, userID string) error {
	result, err := r.db.Exec(ctx, `delete from albion_player_profiles where user_id = $1::uuid`, userID)
	if err != nil {
		return fmt.Errorf("delete Albion profile: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotLinked
	}
	return nil
}

func (r *PostgresRepository) MarkRefreshFailure(ctx context.Context, userID string, attemptedAt time.Time, message string) error {
	if len(message) > 500 {
		message = message[:500]
	}
	result, err := r.db.Exec(ctx, `
		update albion_player_profiles
		set last_refresh_attempt_at = $2, last_refresh_status = 'error',
			last_refresh_error = $3, updated_at = now()
		where user_id = $1::uuid
	`, userID, attemptedAt.UTC(), message)
	if err != nil {
		return fmt.Errorf("mark Albion refresh failure: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotLinked
	}
	return nil
}
