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

const maxProviderResponseBytes = 8 << 20

var serverBaseURLs = map[Server]string{
	ServerAmericas: "https://gameinfo.albiononline.com/api/gameinfo",
	ServerEurope:   "https://gameinfo-ams.albiononline.com/api/gameinfo",
	ServerAsia:     "https://gameinfo-sgp.albiononline.com/api/gameinfo",
}

type Provider interface {
	Search(context.Context, Server, string) ([]SearchResult, error)
	Player(context.Context, Server, string) (Player, error)
	Events(context.Context, Server, string, int) ([]Event, error)
}

type GameInfoProvider struct {
	client *http.Client
}

func NewGameInfoProvider(timeout time.Duration) (*GameInfoProvider, error) {
	if timeout <= 0 {
		return nil, errors.New("provider timeout must be greater than zero")
	}
	return &GameInfoProvider{client: &http.Client{Timeout: timeout}}, nil
}

func NormalizeServer(value string) (Server, error) {
	server := Server(strings.ToLower(strings.TrimSpace(value)))
	if _, ok := serverBaseURLs[server]; !ok {
		return "", errors.New("server must be americas, europe or asia")
	}
	return server, nil
}

func (p *GameInfoProvider) Search(ctx context.Context, server Server, query string) ([]SearchResult, error) {
	query = strings.TrimSpace(query)
	if len(query) < 3 || len(query) > 32 || strings.ContainsAny(query, "\r\n") {
		return nil, errors.New("player name must contain 3-32 characters")
	}
	var response struct {
		Players []rawPlayer `json:"players"`
	}
	if err := p.getJSON(ctx, server, "/search?q="+url.QueryEscape(query), &response); err != nil {
		return nil, err
	}
	results := make([]SearchResult, 0, len(response.Players))
	for _, player := range response.Players {
		if strings.TrimSpace(player.ID) == "" || strings.TrimSpace(player.Name) == "" {
			continue
		}
		results = append(results, SearchResult{
			Server:       server,
			PlayerID:     strings.TrimSpace(player.ID),
			PlayerName:   strings.TrimSpace(player.Name),
			GuildName:    optionalString(player.GuildName),
			AllianceName: optionalString(player.AllianceName),
			Avatar:       optionalString(player.Avatar),
			AvatarRing:   optionalString(player.AvatarRing),
			KillFame:     player.KillFame,
			DeathFame:    player.DeathFame,
			FameRatio:    player.FameRatio,
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		iExact := strings.EqualFold(results[i].PlayerName, query)
		jExact := strings.EqualFold(results[j].PlayerName, query)
		if iExact != jExact {
			return iExact
		}
		return results[i].KillFame > results[j].KillFame
	})
	if len(results) > 20 {
		results = results[:20]
	}
	return results, nil
}

func (p *GameInfoProvider) Player(ctx context.Context, server Server, playerID string) (Player, error) {
	playerID = strings.TrimSpace(playerID)
	if playerID == "" || len(playerID) > 128 || strings.ContainsAny(playerID, "/\\\r\n") {
		return Player{}, errors.New("invalid player id")
	}
	var raw rawPlayer
	if err := p.getJSON(ctx, server, "/players/"+url.PathEscape(playerID), &raw); err != nil {
		return Player{}, err
	}
	if strings.TrimSpace(raw.ID) == "" || strings.TrimSpace(raw.Name) == "" {
		return Player{}, errors.New("provider returned an incomplete player")
	}
	return Player{
		Server:       server,
		PlayerID:     strings.TrimSpace(raw.ID),
		PlayerName:   strings.TrimSpace(raw.Name),
		GuildID:      optionalString(raw.GuildID),
		GuildName:    optionalString(raw.GuildName),
		AllianceID:   optionalString(raw.AllianceID),
		AllianceName: optionalString(raw.AllianceName),
		Avatar:       optionalString(raw.Avatar),
		AvatarRing:   optionalString(raw.AvatarRing),
		KillFame:     raw.KillFame,
		DeathFame:    raw.DeathFame,
		FameRatio:    raw.FameRatio,
	}, nil
}

func (p *GameInfoProvider) Events(ctx context.Context, server Server, playerID string, limit int) ([]Event, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("event limit must be between 1 and 100")
	}
	playerID = strings.TrimSpace(playerID)
	if playerID == "" || len(playerID) > 128 || strings.ContainsAny(playerID, "/\\\r\n") {
		return nil, errors.New("invalid player id")
	}
	perType := limit
	var kills, deaths []rawEvent
	if err := p.getJSON(ctx, server, fmt.Sprintf("/players/%s/kills?limit=%d&offset=0", url.PathEscape(playerID), perType), &kills); err != nil {
		return nil, fmt.Errorf("fetch kills: %w", err)
	}
	if err := p.getJSON(ctx, server, fmt.Sprintf("/players/%s/deaths?limit=%d&offset=0", url.PathEscape(playerID), perType), &deaths); err != nil {
		return nil, fmt.Errorf("fetch deaths: %w", err)
	}
	events := make([]Event, 0, len(kills)+len(deaths))
	for _, raw := range kills {
		events = append(events, normalizeEvent(raw, "kill", true))
	}
	for _, raw := range deaths {
		events = append(events, normalizeEvent(raw, "death", false))
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].OccurredAt.After(events[j].OccurredAt) })
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func (p *GameInfoProvider) getJSON(ctx context.Context, server Server, path string, destination any) error {
	baseURL, ok := serverBaseURLs[server]
	if !ok || p == nil || p.client == nil {
		return errors.New("provider is not configured")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Mozilla/5.0 (compatible; AlbionProductionCalculator/1.0; +https://albion-production-calculator.pages.dev)")
	request.Header.Set("Referer", "https://albiononline.com/")
	response, err := p.client.Do(request)
	if err != nil {
		return fmt.Errorf("request gameinfo provider: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("gameinfo provider returned HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxProviderResponseBytes))
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("decode gameinfo response: %w", err)
	}
	return nil
}

type rawEquipmentItem struct {
	Type string `json:"Type"`
}

type rawEquipment struct {
	MainHand *rawEquipmentItem `json:"MainHand"`
	OffHand  *rawEquipmentItem `json:"OffHand"`
	Head     *rawEquipmentItem `json:"Head"`
	Armor    *rawEquipmentItem `json:"Armor"`
	Shoes    *rawEquipmentItem `json:"Shoes"`
	Bag      *rawEquipmentItem `json:"Bag"`
	Cape     *rawEquipmentItem `json:"Cape"`
	Mount    *rawEquipmentItem `json:"Mount"`
	Potion   *rawEquipmentItem `json:"Potion"`
	Food     *rawEquipmentItem `json:"Food"`
}

type rawPlayer struct {
	ID               string       `json:"Id"`
	Name             string       `json:"Name"`
	GuildID          string       `json:"GuildId"`
	GuildName        string       `json:"GuildName"`
	AllianceID       string       `json:"AllianceId"`
	AllianceName     string       `json:"AllianceName"`
	Avatar           string       `json:"Avatar"`
	AvatarRing       string       `json:"AvatarRing"`
	KillFame         int64        `json:"KillFame"`
	DeathFame        int64        `json:"DeathFame"`
	FameRatio        float64      `json:"FameRatio"`
	AverageItemPower float64      `json:"AverageItemPower"`
	Equipment        rawEquipment `json:"Equipment"`
}

type rawEvent struct {
	EventID              int64     `json:"EventId"`
	TimeStamp            time.Time `json:"TimeStamp"`
	Killer               rawPlayer `json:"Killer"`
	Victim               rawPlayer `json:"Victim"`
	TotalVictimKillFame  int64     `json:"TotalVictimKillFame"`
	NumberOfParticipants int       `json:"numberOfParticipants"`
	GroupMemberCount     int       `json:"groupMemberCount"`
}

func normalizeEvent(raw rawEvent, result string, playerIsKiller bool) Event {
	player := raw.Victim
	opponent := raw.Killer
	if playerIsKiller {
		player = raw.Killer
		opponent = raw.Victim
	}
	playerEquipment := normalizeEquipment(player.Equipment)
	return Event{
		EventID:           raw.EventID,
		OccurredAt:        raw.TimeStamp.UTC(),
		Result:            result,
		OpponentID:        optionalString(opponent.ID),
		OpponentName:      strings.TrimSpace(opponent.Name),
		OpponentGuild:     optionalString(opponent.GuildName),
		KillFame:          raw.TotalVictimKillFame,
		PlayerItemPower:   player.AverageItemPower,
		OpponentItemPower: opponent.AverageItemPower,
		WeaponType:        playerEquipment.MainHand,
		PlayerEquipment:   playerEquipment,
		OpponentEquipment: normalizeEquipment(opponent.Equipment),
		ParticipantCount:  raw.NumberOfParticipants,
		GroupMemberCount:  raw.GroupMemberCount,
	}
}

func normalizeEquipment(raw rawEquipment) Equipment {
	return Equipment{
		MainHand: equipmentItemType(raw.MainHand),
		OffHand:  equipmentItemType(raw.OffHand),
		Head:     equipmentItemType(raw.Head),
		Armor:    equipmentItemType(raw.Armor),
		Shoes:    equipmentItemType(raw.Shoes),
		Bag:      equipmentItemType(raw.Bag),
		Cape:     equipmentItemType(raw.Cape),
		Mount:    equipmentItemType(raw.Mount),
		Potion:   equipmentItemType(raw.Potion),
		Food:     equipmentItemType(raw.Food),
	}
}

func equipmentItemType(item *rawEquipmentItem) *string {
	if item == nil {
		return nil
	}
	return optionalString(item.Type)
}

func optionalString(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
