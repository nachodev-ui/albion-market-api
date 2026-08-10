package playerprofile

import (
	"context"
	"fmt"
	"time"
)

// MarkFallbackSuccess records that the local activity store was refreshed from a
// secondary source while preserving the primary-provider failure counter and
// open-circuit window. This keeps freshness honest without prematurely closing
// the GameInfo circuit breaker.
func (r *PostgresRepository) MarkFallbackSuccess(ctx context.Context, server Server, refreshedAt time.Time, latestAt *time.Time, latestID *int64) error {
	_, err := r.db.Exec(ctx, `
		insert into albion_pvp_ingest_state (
			server, last_attempt_at, last_success_at, latest_event_at, latest_event_id,
			active_source, consecutive_failures, updated_at
		) values ($1,$2,$2,$3,$4,'murderledger',0,now())
		on conflict (server) do update set
			last_success_at = excluded.last_success_at,
			latest_event_at = coalesce(excluded.latest_event_at, albion_pvp_ingest_state.latest_event_at),
			latest_event_id = coalesce(excluded.latest_event_id, albion_pvp_ingest_state.latest_event_id),
			active_source = 'murderledger',
			updated_at = now()
	`, server, refreshedAt.UTC(), latestAt, latestID)
	if err != nil {
		return fmt.Errorf("mark PvP fallback success: %w", err)
	}
	return nil
}
