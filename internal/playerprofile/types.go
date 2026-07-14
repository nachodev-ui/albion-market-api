package playerprofile

import "time"

type Server string

const (
	ServerAmericas Server = "americas"
	ServerEurope   Server = "europe"
	ServerAsia     Server = "asia"
)

type SearchResult struct {
	Server       Server  `json:"server"`
	PlayerID     string  `json:"playerId"`
	PlayerName   string  `json:"playerName"`
	GuildName    *string `json:"guildName,omitempty"`
	AllianceName *string `json:"allianceName,omitempty"`
	Avatar       *string `json:"avatar,omitempty"`
	AvatarRing   *string `json:"avatarRing,omitempty"`
	KillFame     int64   `json:"killFame"`
	DeathFame    int64   `json:"deathFame"`
	FameRatio    float64 `json:"fameRatio"`
}

type Player struct {
	Server       Server
	PlayerID     string
	PlayerName   string
	GuildID      *string
	GuildName    *string
	AllianceID   *string
	AllianceName *string
	Avatar       *string
	AvatarRing   *string
	KillFame     int64
	DeathFame    int64
	FameRatio    float64
}

type Equipment struct {
	MainHand *string `json:"mainHand,omitempty"`
	OffHand  *string `json:"offHand,omitempty"`
	Head     *string `json:"head,omitempty"`
	Armor    *string `json:"armor,omitempty"`
	Shoes    *string `json:"shoes,omitempty"`
	Bag      *string `json:"bag,omitempty"`
	Cape     *string `json:"cape,omitempty"`
	Mount    *string `json:"mount,omitempty"`
	Potion   *string `json:"potion,omitempty"`
	Food     *string `json:"food,omitempty"`
}

type Event struct {
	EventID           int64     `json:"eventId"`
	OccurredAt        time.Time `json:"occurredAt"`
	Result            string    `json:"result"`
	OpponentID        *string   `json:"opponentId,omitempty"`
	OpponentName      string    `json:"opponentName"`
	OpponentGuild     *string   `json:"opponentGuild,omitempty"`
	KillFame          int64     `json:"killFame"`
	PlayerItemPower   float64   `json:"playerItemPower"`
	OpponentItemPower float64   `json:"opponentItemPower"`
	WeaponType        *string   `json:"weaponType,omitempty"`
	PlayerEquipment   Equipment `json:"playerEquipment"`
	OpponentEquipment Equipment `json:"opponentEquipment"`
	ParticipantCount  int       `json:"participantCount"`
	GroupMemberCount  int       `json:"groupMemberCount"`
}

type Profile struct {
	ID                 string     `json:"id"`
	Server             Server     `json:"server"`
	PlayerID           string     `json:"playerId"`
	PlayerName         string     `json:"playerName"`
	GuildID            *string    `json:"guildId,omitempty"`
	GuildName          *string    `json:"guildName,omitempty"`
	AllianceID         *string    `json:"allianceId,omitempty"`
	AllianceName       *string    `json:"allianceName,omitempty"`
	Avatar             *string    `json:"avatar,omitempty"`
	AvatarRing         *string    `json:"avatarRing,omitempty"`
	VerificationStatus string     `json:"verificationStatus"`
	KillFame           int64      `json:"killFame"`
	DeathFame          int64      `json:"deathFame"`
	FameRatio          float64    `json:"fameRatio"`
	LinkedAt           time.Time  `json:"linkedAt"`
	LastRefreshedAt    *time.Time `json:"lastRefreshedAt,omitempty"`
	LastRefreshAttempt *time.Time `json:"lastRefreshAttemptAt,omitempty"`
	LastRefreshStatus  string     `json:"lastRefreshStatus"`
	LastRefreshError   *string    `json:"lastRefreshError,omitempty"`
}

type Summary struct {
	RecentKills      int      `json:"recentKills"`
	RecentDeaths     int      `json:"recentDeaths"`
	RecentFightCount int      `json:"recentFightCount"`
	KDRatio          *float64 `json:"kdRatio,omitempty"`
	KillFame         int64    `json:"killFame"`
	DeathFame        int64    `json:"deathFame"`
	FameRatio        float64  `json:"fameRatio"`
}

type CurrentResponse struct {
	Profile Profile `json:"profile"`
	Summary Summary `json:"summary"`
	Events  []Event `json:"events"`
}

type LinkRequest struct {
	Server   Server `json:"server"`
	PlayerID string `json:"playerId"`
}
