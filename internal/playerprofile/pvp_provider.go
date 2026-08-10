package playerprofile

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// PlayerIdentityProvider supplies slowly-changing public character identity data.
// It is deliberately independent from PvP event ingestion so profile reads never
// need to contact an upstream provider.
type PlayerIdentityProvider interface {
	Search(context.Context, Server, string) ([]SearchResult, error)
	Player(context.Context, Server, string) (Player, error)
}

// PvPEventProvider supplies immutable combat events. Implementations may use the
// Albion global event feed or a secondary reconciliation source.
type PvPEventProvider interface {
	Name() string
	FetchRecent(context.Context, PvPFetchRequest) ([]PvPEventRecord, error)
}

type GameInfoEventsProvider struct {
	gameInfo *GameInfoProvider
}

func NewGameInfoEventsProvider(timeout time.Duration) (*GameInfoEventsProvider, error) {
	provider, err := NewGameInfoProvider(timeout)
	if err != nil {
		return nil, err
	}
	return &GameInfoEventsProvider{gameInfo: provider}, nil
}

func (p *GameInfoEventsProvider) Name() string { return "gameinfo" }

func (p *GameInfoEventsProvider) FetchRecent(ctx context.Context, request PvPFetchRequest) ([]PvPEventRecord, error) {
	if p == nil || p.gameInfo == nil {
		return nil, errors.New("gameinfo events provider is not configured")
	}
	if _, err := NormalizeServer(string(request.Server)); err != nil {
		return nil, err
	}
	if request.Limit < 1 || request.Limit > 50 {
		return nil, errors.New("event page limit must be between 1 and 50")
	}
	if request.Offset < 0 || request.Offset > 5000 {
		return nil, errors.New("event offset must be between 0 and 5000")
	}

	var raws []rawEvent
	if request.PlayerID == nil {
		path := fmt.Sprintf("/events?limit=%d&offset=%d", request.Limit, request.Offset)
		if err := p.gameInfo.getJSON(ctx, request.Server, path, &raws); err != nil {
			return nil, err
		}
	} else {
		playerID := strings.TrimSpace(*request.PlayerID)
		if playerID == "" || len(playerID) > 128 || strings.ContainsAny(playerID, "/\\\r\n") {
			return nil, errors.New("invalid player id")
		}
		var kills, deaths []rawEvent
		base := "/players/" + url.PathEscape(playerID)
		if err := p.gameInfo.getJSON(ctx, request.Server,
			fmt.Sprintf("%s/kills?limit=%d&offset=%d", base, request.Limit, request.Offset), &kills); err != nil {
			return nil, fmt.Errorf("fetch player kills: %w", err)
		}
		if err := p.gameInfo.getJSON(ctx, request.Server,
			fmt.Sprintf("%s/deaths?limit=%d&offset=%d", base, request.Limit, request.Offset), &deaths); err != nil {
			return nil, fmt.Errorf("fetch player deaths: %w", err)
		}
		raws = append(kills, deaths...)
		sort.SliceStable(raws, func(i, j int) bool { return raws[i].TimeStamp.After(raws[j].TimeStamp) })
		if len(raws) > request.Limit {
			raws = raws[:request.Limit]
		}
	}

	events := make([]PvPEventRecord, 0, len(raws))
	for _, raw := range raws {
		event, ok := normalizePvPEvent(raw, request.Server, p.Name())
		if ok {
			events = append(events, event)
		}
	}
	return events, nil
}

// MurderLedgerFallback is intentionally a reconciliation-only provider. The
// request must contain a player ID; it is never called while serving a user HTTP
// request. MurderLedger is not assumed to support all Albion regions, so failures
// are isolated to the worker and never affect profile reads.
type MurderLedgerFallback struct {
	client  *http.Client
	baseURL string
}

func NewMurderLedgerFallback(baseURL string, timeout time.Duration) (*MurderLedgerFallback, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://murderledger.com/api"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("invalid MurderLedger API base URL")
	}
	if timeout <= 0 {
		return nil, errors.New("MurderLedger timeout must be greater than zero")
	}
	return &MurderLedgerFallback{client: &http.Client{Timeout: timeout}, baseURL: baseURL}, nil
}

func (p *MurderLedgerFallback) Name() string { return "murderledger" }

func (p *MurderLedgerFallback) FetchRecent(ctx context.Context, request PvPFetchRequest) ([]PvPEventRecord, error) {
	if p == nil || p.client == nil {
		return nil, errors.New("MurderLedger provider is not configured")
	}
	if request.PlayerID == nil {
		return nil, errors.New("MurderLedger reconciliation requires a player id")
	}
	playerID := strings.TrimSpace(*request.PlayerID)
	if playerID == "" || len(playerID) > 128 || strings.ContainsAny(playerID, "/\\\r\n") {
		return nil, errors.New("invalid player id")
	}
	if request.Limit < 1 || request.Limit > 100 {
		return nil, errors.New("MurderLedger event limit must be between 1 and 100")
	}
	if request.Offset < 0 || request.Offset > 5000 {
		return nil, errors.New("MurderLedger event offset must be between 0 and 5000")
	}

	// MurderLedger's documented player-events endpoint has historically used a
	// skip parameter. Keeping the base URL configurable lets operations change the
	// upstream route without touching the request path used by our application.
	endpoint := fmt.Sprintf("%s/players/%s/events?limit=%d&skip=%d",
		p.baseURL, url.PathEscape(playerID), request.Limit, request.Offset)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", "AlbionProductionCalculator-PvP-Reconciler/1.0")
	response, err := p.client.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("request MurderLedger: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return nil, fmt.Errorf("MurderLedger returned HTTP %d", response.StatusCode)
	}
	payload, err := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read MurderLedger response: %w", err)
	}
	raws, err := decodeMurderLedgerEvents(payload)
	if err != nil {
		return nil, err
	}
	events := make([]PvPEventRecord, 0, len(raws))
	for _, raw := range raws {
		event, ok := normalizePvPEvent(raw, request.Server, p.Name())
		if ok {
			events = append(events, event)
		}
	}
	return events, nil
}

func decodeMurderLedgerEvents(payload []byte) ([]rawEvent, error) {
	var direct []rawEvent
	if err := json.Unmarshal(payload, &direct); err == nil {
		return direct, nil
	}
	var wrapped struct {
		Events []rawEvent `json:"events"`
		Data   []rawEvent `json:"data"`
	}
	if err := json.Unmarshal(payload, &wrapped); err != nil {
		return nil, fmt.Errorf("decode MurderLedger response: %w", err)
	}
	if wrapped.Events != nil {
		return wrapped.Events, nil
	}
	if wrapped.Data != nil {
		return wrapped.Data, nil
	}
	return []rawEvent{}, nil
}

func normalizePvPEvent(raw rawEvent, server Server, source string) (PvPEventRecord, bool) {
	killerID := strings.TrimSpace(raw.Killer.ID)
	victimID := strings.TrimSpace(raw.Victim.ID)
	killerName := strings.TrimSpace(raw.Killer.Name)
	victimName := strings.TrimSpace(raw.Victim.Name)
	if raw.EventID <= 0 || raw.TimeStamp.IsZero() || killerID == "" || victimID == "" || killerName == "" || victimName == "" {
		return PvPEventRecord{}, false
	}
	killerEquipment := normalizeEquipment(raw.Killer.Equipment)
	victimEquipment := normalizeEquipment(raw.Victim.Equipment)
	return PvPEventRecord{
		Server:              server,
		EventID:             raw.EventID,
		OccurredAt:          raw.TimeStamp.UTC(),
		KillerID:            killerID,
		KillerName:          killerName,
		KillerGuildID:       optionalString(raw.Killer.GuildID),
		KillerGuildName:     optionalString(raw.Killer.GuildName),
		KillerAllianceID:    optionalString(raw.Killer.AllianceID),
		KillerAllianceName:  optionalString(raw.Killer.AllianceName),
		KillerItemPower:     raw.Killer.AverageItemPower,
		KillerWeaponType:    killerEquipment.MainHand,
		KillerEquipment:     killerEquipment,
		VictimID:            victimID,
		VictimName:          victimName,
		VictimGuildID:       optionalString(raw.Victim.GuildID),
		VictimGuildName:     optionalString(raw.Victim.GuildName),
		VictimAllianceID:    optionalString(raw.Victim.AllianceID),
		VictimAllianceName:  optionalString(raw.Victim.AllianceName),
		VictimItemPower:     raw.Victim.AverageItemPower,
		VictimWeaponType:    victimEquipment.MainHand,
		VictimEquipment:     victimEquipment,
		TotalVictimKillFame: raw.TotalVictimKillFame,
		ParticipantCount:    raw.NumberOfParticipants,
		GroupMemberCount:    raw.GroupMemberCount,
		Source:              source,
	}, true
}
