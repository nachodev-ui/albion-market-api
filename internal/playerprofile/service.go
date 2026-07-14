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

type CooldownError struct{ RetryAfter time.Duration }

func (e CooldownError) Error() string { return "profile refresh is on cooldown" }

type Service struct {
	repository Repository
	provider   Provider
	accounts   *accounts.Service
	now        func() time.Time
	cooldown   time.Duration
	eventLimit int
}

func NewService(repository Repository, provider Provider, accountService *accounts.Service, cooldown time.Duration, eventLimit int) (*Service, error) {
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

func (s *Service) Current(ctx context.Context, identity authn.Identity) (CurrentResponse, error) {
	userID, err := s.userID(ctx, identity)
	if err != nil {
		return CurrentResponse{}, err
	}
	snapshot, err := s.repository.Get(ctx, userID)
	if err != nil {
		return CurrentResponse{}, err
	}
	return responseFromSnapshot(snapshot), nil
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
	player, events, err := s.fetchSnapshot(ctx, server, playerID)
	if err != nil {
		return CurrentResponse{}, err
	}
	if player.PlayerID != playerID {
		return CurrentResponse{}, fmt.Errorf("%w: provider returned a different player", ErrInvalidRequest)
	}
	snapshot, err := s.repository.Save(ctx, userID, player, events, s.now().UTC())
	if err != nil {
		return CurrentResponse{}, err
	}
	return responseFromSnapshot(snapshot), nil
}

func (s *Service) Refresh(ctx context.Context, identity authn.Identity) (CurrentResponse, error) {
	userID, err := s.userID(ctx, identity)
	if err != nil {
		return CurrentResponse{}, err
	}
	current, err := s.repository.Get(ctx, userID)
	if err != nil {
		return CurrentResponse{}, err
	}
	now := s.now().UTC()
	if current.Profile.LastRefreshAttempt != nil {
		next := current.Profile.LastRefreshAttempt.Add(s.cooldown)
		if now.Before(next) {
			return CurrentResponse{}, CooldownError{RetryAfter: next.Sub(now)}
		}
	}
	player, events, err := s.fetchSnapshot(ctx, current.Profile.Server, current.Profile.PlayerID)
	if err != nil {
		_ = s.repository.MarkRefreshFailure(ctx, userID, now, err.Error())
		return CurrentResponse{}, err
	}
	snapshot, err := s.repository.Save(ctx, userID, player, events, now)
	if err != nil {
		return CurrentResponse{}, err
	}
	return responseFromSnapshot(snapshot), nil
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

func (s *Service) fetchSnapshot(ctx context.Context, server Server, playerID string) (Player, []Event, error) {
	player, err := s.provider.Player(ctx, server, playerID)
	if err != nil {
		return Player{}, nil, fmt.Errorf("fetch Albion player: %w", err)
	}
	events, err := s.provider.Events(ctx, server, playerID, s.eventLimit)
	if err != nil {
		return Player{}, nil, fmt.Errorf("fetch Albion activity: %w", err)
	}
	return player, events, nil
}

func responseFromSnapshot(snapshot Snapshot) CurrentResponse {
	summary := Summary{
		KillFame:         snapshot.Profile.KillFame,
		DeathFame:        snapshot.Profile.DeathFame,
		FameRatio:        snapshot.Profile.FameRatio,
		RecentFightCount: len(snapshot.Events),
	}
	for _, event := range snapshot.Events {
		if event.Result == "kill" {
			summary.RecentKills++
		}
		if event.Result == "death" {
			summary.RecentDeaths++
		}
	}
	if summary.RecentDeaths > 0 {
		ratio := float64(summary.RecentKills) / float64(summary.RecentDeaths)
		summary.KDRatio = &ratio
	} else if summary.RecentKills > 0 {
		ratio := float64(summary.RecentKills)
		summary.KDRatio = &ratio
	}
	if snapshot.Events == nil {
		snapshot.Events = []Event{}
	}
	return CurrentResponse{Profile: snapshot.Profile, Summary: summary, Events: snapshot.Events}
}
