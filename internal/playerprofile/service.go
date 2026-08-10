package playerprofile

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/accounts"
	"github.com/nachodev-ui/albion-market-api/internal/authn"
)

var (
	ErrInvalidRequest      = errors.New("invalid player profile request")
	ErrProviderUnavailable = errors.New("Albion provider unavailable")
)

// CooldownError remains part of the HTTP contract for backwards-compatible
// clients, but local-store refreshes no longer use a cooldown.
type CooldownError struct{ RetryAfter time.Duration }

func (e CooldownError) Error() string { return "profile refresh is on cooldown" }

type Service struct {
	repository Repository
	provider   PlayerIdentityProvider
	accounts   *accounts.Service
	now        func() time.Time
	cooldown   time.Duration
	eventLimit int
}

func NewService(repository Repository, provider PlayerIdentityProvider, accountService *accounts.Service, cooldown time.Duration, eventLimit int) (*Service, error) {
	if repository == nil || provider == nil || accountService == nil {
		return nil, errors.New("player profile dependencies are required")
	}
	if cooldown <= 0 || eventLimit < 1 || eventLimit > 100 {
		return nil, errors.New("invalid player profile service configuration")
	}
	return &Service{repository: repository, provider: provider, accounts: accountService, now: time.Now, cooldown: cooldown, eventLimit: eventLimit}, nil
}

func (s *Service) Search(ctx context.Context, serverValue, name string) ([]SearchResult, error) {
	server, err := NormalizeServer(serverValue)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	results, err := s.provider.Search(ctx, server, name)
	if err != nil {
		return nil, fmt.Errorf("%w: search players: %v", ErrProviderUnavailable, err)
	}
	return results, nil
}

// Current is intentionally local-only. Identity comes from the cached linked
// profile and combat activity comes from albion_pvp_events. Opening a profile
// therefore never waits for GameInfo or any secondary provider.
func (s *Service) Current(ctx context.Context, identity authn.Identity) (CurrentResponse, error) {
	userID, err := s.userID(ctx, identity)
	if err != nil {
		return CurrentResponse{}, err
	}
	snapshot, err := s.repository.Get(ctx, userID)
	if err != nil {
		return CurrentResponse{}, err
	}
	return responseFromSnapshot(limitSnapshotEvents(snapshot, s.eventLimit)), nil
}

func (s *Service) Link(ctx context.Context, identity authn.Identity, request LinkRequest) (CurrentResponse, error) {
	server, err := NormalizeServer(string(request.Server))
	if err != nil {
		return CurrentResponse{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	playerID := strings.TrimSpace(request.PlayerID)
	if playerID == "" || len(playerID) > 128 || strings.ContainsAny(playerID, "/\\\r\n") {
		return CurrentResponse{}, fmt.Errorf("%w: invalid player id", ErrInvalidRequest)
	}
	userID, err := s.userID(ctx, identity)
	if err != nil {
		return CurrentResponse{}, err
	}

	// Linking is the only authenticated profile operation that needs identity
	// confirmation immediately. It does not request kills/deaths; those are
	// already collected independently by the continuous worker.
	player, err := s.provider.Player(ctx, server, playerID)
	if err != nil {
		return CurrentResponse{}, fmt.Errorf("%w: fetch Albion player: %v", ErrProviderUnavailable, err)
	}
	if player.PlayerID != playerID {
		return CurrentResponse{}, fmt.Errorf("%w: provider returned a different player", ErrInvalidRequest)
	}
	snapshot, err := s.repository.Save(ctx, userID, player, nil, s.now().UTC())
	if err != nil {
		return CurrentResponse{}, err
	}
	return responseFromSnapshot(limitSnapshotEvents(snapshot, s.eventLimit)), nil
}

// Refresh now means "read the newest locally ingested snapshot". The ingestion
// and identity workers own upstream I/O, retries and circuit breaking. This makes
// the user-facing refresh effectively a database read and removes the former
// five-minute penalty after upstream failures.
func (s *Service) Refresh(ctx context.Context, identity authn.Identity) (CurrentResponse, error) {
	return s.Current(ctx, identity)
}

func (s *Service) Delete(ctx context.Context, identity authn.Identity) error {
	userID, err := s.userID(ctx, identity)
	if err != nil {
		return err
	}
	return s.repository.Delete(ctx, userID)
}

func (s *Service) userID(ctx context.Context, identity authn.Identity) (string, error) {
	access, err := s.accounts.Current(ctx, identity)
	if err != nil {
		return "", fmt.Errorf("resolve profile account: %w", err)
	}
	return access.User.ID, nil
}

func limitSnapshotEvents(snapshot Snapshot, limit int) Snapshot {
	if limit > 0 && len(snapshot.Events) > limit {
		snapshot.Events = snapshot.Events[:limit]
	}
	return snapshot
}

// Kept for compatibility with migration/legacy-cache tests. New profile reads no
// longer perform provider rehydration in the HTTP request path.
func refreshWindowElapsed(profile Profile, now time.Time, cooldown time.Duration) bool {
	if profile.LastRefreshAttempt == nil {
		return true
	}
	return !now.Before(profile.LastRefreshAttempt.Add(cooldown))
}

func snapshotNeedsEquipmentRefresh(snapshot Snapshot) bool {
	if len(snapshot.Events) == 0 {
		return false
	}

	legacySignal := false
	for _, event := range snapshot.Events {
		playerSlots := equipmentSlotCount(event.PlayerEquipment)
		opponentSlots := equipmentSlotCount(event.OpponentEquipment)
		if playerSlots > 1 || opponentSlots > 0 {
			return false
		}
		if playerSlots == 1 || event.WeaponType != nil {
			legacySignal = true
		}
	}
	return legacySignal
}

func equipmentSlotCount(equipment Equipment) int {
	items := []*string{
		equipment.MainHand,
		equipment.OffHand,
		equipment.Head,
		equipment.Armor,
		equipment.Shoes,
		equipment.Bag,
		equipment.Cape,
		equipment.Mount,
		equipment.Potion,
		equipment.Food,
	}
	count := 0
	for _, item := range items {
		if item != nil && strings.TrimSpace(*item) != "" {
			count++
		}
	}
	return count
}

func responseFromSnapshot(snapshot Snapshot) CurrentResponse {
	if snapshot.Events == nil {
		snapshot.Events = []Event{}
	}
	summary := Summary{RecentFightCount: len(snapshot.Events)}
	for _, event := range snapshot.Events {
		switch event.Result {
		case "kill":
			summary.RecentKills++
			summary.KillFame += event.KillFame
		case "death":
			summary.RecentDeaths++
			summary.DeathFame += event.KillFame
		}
	}
	if summary.RecentDeaths > 0 {
		ratio := float64(summary.RecentKills) / float64(summary.RecentDeaths)
		summary.KDRatio = &ratio
	} else if summary.RecentKills > 0 {
		ratio := float64(summary.RecentKills)
		summary.KDRatio = &ratio
	}
	if summary.DeathFame > 0 {
		summary.FameRatio = float64(summary.KillFame) / float64(summary.DeathFame)
	} else if summary.KillFame > 0 {
		summary.FameRatio = float64(summary.KillFame)
	}
	return CurrentResponse{Profile: snapshot.Profile, Summary: summary, Events: snapshot.Events}
}
