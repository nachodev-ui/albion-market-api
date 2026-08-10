package playerprofile

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

type PvPIngestWorkerConfig struct {
	PollInterval            time.Duration
	BusyPollInterval        time.Duration
	MaxBackoff              time.Duration
	PageSize                int
	MaxPages                int
	RetryAttempts           int
	CircuitFailureThreshold int
	CircuitOpenDuration     time.Duration
	FallbackPlayerLimit     int
	FallbackEventsPerPlayer int
}

func DefaultPvPIngestWorkerConfig() PvPIngestWorkerConfig {
	return PvPIngestWorkerConfig{
		PollInterval:            30 * time.Second,
		BusyPollInterval:        10 * time.Second,
		MaxBackoff:              5 * time.Minute,
		PageSize:                50,
		MaxPages:                20,
		RetryAttempts:           3,
		CircuitFailureThreshold: 3,
		CircuitOpenDuration:     2 * time.Minute,
		FallbackPlayerLimit:     100,
		FallbackEventsPerPlayer: 50,
	}
}

type PvPIngestWorker struct {
	repository *PostgresRepository
	primary    PvPEventProvider
	fallback   PvPEventProvider
	logger     *observability.Logger
	config     PvPIngestWorkerConfig
	now        func() time.Time
}

func NewPvPIngestWorker(repository *PostgresRepository, primary, fallback PvPEventProvider, logger *observability.Logger, config PvPIngestWorkerConfig) (*PvPIngestWorker, error) {
	if repository == nil || primary == nil || logger == nil {
		return nil, errors.New("PvP ingest worker dependencies are required")
	}
	if config.PollInterval <= 0 || config.BusyPollInterval <= 0 || config.MaxBackoff <= 0 ||
		config.PageSize < 1 || config.PageSize > 50 || config.MaxPages < 1 || config.MaxPages > 20 ||
		config.RetryAttempts < 1 || config.CircuitFailureThreshold < 1 || config.CircuitOpenDuration <= 0 ||
		config.FallbackPlayerLimit < 1 || config.FallbackEventsPerPlayer < 1 || config.FallbackEventsPerPlayer > 100 {
		return nil, errors.New("invalid PvP ingest worker configuration")
	}
	return &PvPIngestWorker{
		repository: repository,
		primary:    primary,
		fallback:   fallback,
		logger:     logger,
		config:     config,
		now:        time.Now,
	}, nil
}

func (w *PvPIngestWorker) Run(ctx context.Context) {
	servers := []Server{ServerAmericas, ServerEurope, ServerAsia}
	var group sync.WaitGroup
	group.Add(len(servers))
	for _, server := range servers {
		server := server
		go func() {
			defer group.Done()
			w.runServer(ctx, server)
		}()
	}
	group.Wait()
}

func (w *PvPIngestWorker) runServer(ctx context.Context, server Server) {
	state, err := w.repository.GetIngestState(ctx, server)
	if err != nil {
		w.logger.Error("pvp_ingest.state_load_failed", observability.F("server", server), observability.F("error", err))
		state = IngestState{Server: server}
	}

	for ctx.Err() == nil {
		now := w.now().UTC()
		if state.CircuitOpenUntil != nil && now.Before(*state.CircuitOpenUntil) {
			w.reconcileFallback(ctx, server)
			wait := minDuration(w.config.PollInterval, time.Until(*state.CircuitOpenUntil))
			if wait <= 0 {
				wait = w.config.BusyPollInterval
			}
			if !sleepContext(ctx, wait) {
				return
			}
			continue
		}

		latestAt, latestID, pages, err := w.pollPrimary(ctx, server, state.LatestEventID)
		attemptedAt := w.now().UTC()
		if err == nil {
			if markErr := w.repository.MarkIngestSuccess(ctx, server, w.primary.Name(), attemptedAt, latestAt, latestID); markErr != nil {
				w.logger.Error("pvp_ingest.state_success_failed", observability.F("server", server), observability.F("error", markErr))
			} else {
				state.LastAttemptAt = &attemptedAt
				state.LastSuccessAt = &attemptedAt
				state.LatestEventAt = latestAt
				state.LatestEventID = latestID
				state.ConsecutiveFailures = 0
				state.CircuitOpenUntil = nil
				source := w.primary.Name()
				state.ActiveSource = &source
				state.LastError = nil
			}
			interval := w.config.PollInterval
			if pages > 1 {
				interval = w.config.BusyPollInterval
			}
			if !sleepContext(ctx, interval) {
				return
			}
			continue
		}

		state.ConsecutiveFailures++
		message := err.Error()
		state.LastError = &message
		var openUntil *time.Time
		if state.ConsecutiveFailures >= w.config.CircuitFailureThreshold {
			value := attemptedAt.Add(w.config.CircuitOpenDuration)
			state.CircuitOpenUntil = &value
			openUntil = &value
			w.logger.Warn(
				"pvp_ingest.circuit_opened",
				observability.F("server", server),
				observability.F("failures", state.ConsecutiveFailures),
				observability.F("until", value),
				observability.F("error", err),
			)
		} else {
			w.logger.Warn(
				"pvp_ingest.poll_failed",
				observability.F("server", server),
				observability.F("failures", state.ConsecutiveFailures),
				observability.F("error", err),
			)
		}
		if markErr := w.repository.MarkIngestFailure(ctx, server, attemptedAt, state.ConsecutiveFailures, openUntil, message); markErr != nil {
			w.logger.Error("pvp_ingest.state_failure_failed", observability.F("server", server), observability.F("error", markErr))
		}
		if openUntil != nil {
			w.reconcileFallback(ctx, server)
		}
		if !sleepContext(ctx, w.failureBackoff(state.ConsecutiveFailures)) {
			return
		}
	}
}

func (w *PvPIngestWorker) pollPrimary(ctx context.Context, server Server, anchorID *int64) (*time.Time, *int64, int, error) {
	var latestAt *time.Time
	var latestID *int64
	pagesFetched := 0
	anchorSeen := false

	for page := 0; page < w.config.MaxPages; page++ {
		offset := page * w.config.PageSize
		events, err := w.fetchWithRetry(ctx, w.primary, PvPFetchRequest{
			Server: server,
			Limit:  w.config.PageSize,
			Offset: offset,
		})
		if err != nil {
			return nil, nil, pagesFetched, fmt.Errorf("fetch %s PvP page %d: %w", server, page, err)
		}
		pagesFetched++
		if len(events) == 0 {
			break
		}
		if latestAt == nil {
			candidateAt := events[0].OccurredAt.UTC()
			candidateID := events[0].EventID
			for _, event := range events[1:] {
				if event.OccurredAt.After(candidateAt) {
					candidateAt = event.OccurredAt.UTC()
					candidateID = event.EventID
				}
			}
			latestAt = &candidateAt
			latestID = &candidateID
		}
		if anchorID != nil {
			for _, event := range events {
				if event.EventID == *anchorID {
					anchorSeen = true
					break
				}
			}
		}
		affected, err := w.repository.UpsertPvPEvents(ctx, events)
		if err != nil {
			return nil, nil, pagesFetched, err
		}
		w.logger.Info(
			"pvp_ingest.page",
			observability.F("server", server),
			observability.F("page", page),
			observability.F("received", len(events)),
			observability.F("touched", affected),
		)
		if anchorSeen || len(events) < w.config.PageSize {
			break
		}
	}

	if anchorID != nil && !anchorSeen && pagesFetched == w.config.MaxPages {
		w.logger.Warn(
			"pvp_ingest.anchor_outside_window",
			observability.F("server", server),
			observability.F("anchor_event_id", *anchorID),
			observability.F("pages", pagesFetched),
		)
	}
	return latestAt, latestID, pagesFetched, nil
}

func (w *PvPIngestWorker) fetchWithRetry(ctx context.Context, provider PvPEventProvider, request PvPFetchRequest) ([]PvPEventRecord, error) {
	var lastErr error
	for attempt := 1; attempt <= w.config.RetryAttempts; attempt++ {
		events, err := provider.FetchRecent(ctx, request)
		if err == nil {
			return events, nil
		}
		lastErr = err
		if attempt == w.config.RetryAttempts {
			break
		}
		delay := time.Duration(1<<(attempt-1)) * 500 * time.Millisecond
		if !sleepContext(ctx, delay) {
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

func (w *PvPIngestWorker) reconcileFallback(ctx context.Context, server Server) {
	if w.fallback == nil || ctx.Err() != nil {
		return
	}
	players, err := w.repository.ListLinkedPlayers(ctx, server, w.config.FallbackPlayerLimit)
	if err != nil {
		w.logger.Error("pvp_ingest.fallback_players_failed", observability.F("server", server), observability.F("error", err))
		return
	}
	var latestAt *time.Time
	var latestID *int64
	var touched int64
	var successes int
	for _, player := range players {
		playerID := player.PlayerID
		events, fetchErr := w.fetchWithRetry(ctx, w.fallback, PvPFetchRequest{
			Server:   server,
			Limit:    w.config.FallbackEventsPerPlayer,
			Offset:   0,
			PlayerID: &playerID,
		})
		if fetchErr != nil {
			w.logger.Warn(
				"pvp_ingest.fallback_player_failed",
				observability.F("server", server),
				observability.F("player_id", playerID),
				observability.F("error", fetchErr),
			)
			continue
		}
		affected, saveErr := w.repository.UpsertPvPEvents(ctx, events)
		if saveErr != nil {
			w.logger.Error("pvp_ingest.fallback_save_failed", observability.F("server", server), observability.F("error", saveErr))
			continue
		}
		touched += affected
		successes++
		for _, event := range events {
			if latestAt == nil || event.OccurredAt.After(*latestAt) {
				candidateAt := event.OccurredAt.UTC()
				candidateID := event.EventID
				latestAt = &candidateAt
				latestID = &candidateID
			}
		}
	}
	if successes == 0 {
		return
	}
	refreshedAt := w.now().UTC()
	if err := w.repository.MarkFallbackSuccess(ctx, server, refreshedAt, latestAt, latestID); err != nil {
		w.logger.Error("pvp_ingest.fallback_state_failed", observability.F("server", server), observability.F("error", err))
		return
	}
	w.logger.Info(
		"pvp_ingest.fallback_reconciled",
		observability.F("server", server),
		observability.F("players", successes),
		observability.F("touched", touched),
	)
}

func (w *PvPIngestWorker) failureBackoff(failures int) time.Duration {
	if failures < 1 {
		return w.config.PollInterval
	}
	shift := failures - 1
	if shift > 6 {
		shift = 6
	}
	backoff := w.config.PollInterval * time.Duration(1<<shift)
	if backoff > w.config.MaxBackoff {
		return w.config.MaxBackoff
	}
	return backoff
}

type IdentityRefreshWorkerConfig struct {
	PollInterval  time.Duration
	StaleAfter    time.Duration
	BatchSize     int
	RetryAttempts int
}

func DefaultIdentityRefreshWorkerConfig() IdentityRefreshWorkerConfig {
	return IdentityRefreshWorkerConfig{
		PollInterval:  15 * time.Minute,
		StaleAfter:    6 * time.Hour,
		BatchSize:     50,
		RetryAttempts: 3,
	}
}

type IdentityRefreshWorker struct {
	repository *PostgresRepository
	provider   PlayerIdentityProvider
	logger     *observability.Logger
	config     IdentityRefreshWorkerConfig
	now        func() time.Time
}

func NewIdentityRefreshWorker(repository *PostgresRepository, provider PlayerIdentityProvider, logger *observability.Logger, config IdentityRefreshWorkerConfig) (*IdentityRefreshWorker, error) {
	if repository == nil || provider == nil || logger == nil {
		return nil, errors.New("identity refresh worker dependencies are required")
	}
	if config.PollInterval <= 0 || config.StaleAfter <= 0 || config.BatchSize < 1 || config.RetryAttempts < 1 {
		return nil, errors.New("invalid identity refresh worker configuration")
	}
	return &IdentityRefreshWorker{repository: repository, provider: provider, logger: logger, config: config, now: time.Now}, nil
}

func (w *IdentityRefreshWorker) Run(ctx context.Context) {
	w.refreshBatch(ctx)
	ticker := time.NewTicker(w.config.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.refreshBatch(ctx)
		}
	}
}

func (w *IdentityRefreshWorker) refreshBatch(ctx context.Context) {
	candidates, err := w.repository.ListIdentityRefreshCandidates(ctx, w.now().UTC().Add(-w.config.StaleAfter), w.config.BatchSize)
	if err != nil {
		w.logger.Error("player_identity.candidates_failed", observability.F("error", err))
		return
	}
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return
		}
		player, fetchErr := w.fetchIdentityWithRetry(ctx, candidate.Server, candidate.PlayerID)
		attemptedAt := w.now().UTC()
		if fetchErr != nil {
			_ = w.repository.MarkRefreshFailure(ctx, candidate.UserID, attemptedAt, fetchErr.Error())
			w.logger.Warn(
				"player_identity.refresh_failed",
				observability.F("server", candidate.Server),
				observability.F("player_id", candidate.PlayerID),
				observability.F("error", fetchErr),
			)
			continue
		}
		if err := w.repository.UpdateIdentity(ctx, candidate.UserID, player, attemptedAt); err != nil {
			w.logger.Error("player_identity.save_failed", observability.F("server", candidate.Server), observability.F("player_id", candidate.PlayerID), observability.F("error", err))
		}
	}
}

func (w *IdentityRefreshWorker) fetchIdentityWithRetry(ctx context.Context, server Server, playerID string) (Player, error) {
	var lastErr error
	for attempt := 1; attempt <= w.config.RetryAttempts; attempt++ {
		player, err := w.provider.Player(ctx, server, playerID)
		if err == nil {
			return player, nil
		}
		lastErr = err
		if attempt == w.config.RetryAttempts {
			break
		}
		if !sleepContext(ctx, time.Duration(1<<(attempt-1))*500*time.Millisecond) {
			return Player{}, ctx.Err()
		}
	}
	return Player{}, lastErr
}

func sleepContext(ctx context.Context, duration time.Duration) bool {
	if duration <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
