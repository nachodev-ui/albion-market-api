package domain

import "time"

type Server string

const (
	ServerWest   Server = "west"
	ServerEast   Server = "east"
	ServerEurope Server = "europe"
)

type IngestPricesRequest struct {
	RequestID string        `json:"request_id"`
	Server    Server        `json:"server"`
	Entries   []PriceIngest `json:"entries"`
}

type PriceIngest struct {
	ObservedAt     time.Time  `json:"observed_at"`
	LocationID     int16      `json:"location_id"`
	ItemKey        string     `json:"item_key"`
	Quality        int16      `json:"quality"`
	SellPriceMin   *int64     `json:"sell_price_min"`
	SellPriceMinAt *time.Time `json:"sell_price_min_at"`
	BuyPriceMax    *int64     `json:"buy_price_max"`
	BuyPriceMaxAt  *time.Time `json:"buy_price_max_at"`
}

type IngestPersistenceTiming struct {
	Transaction         time.Duration
	Commit              time.Duration
	TransactionMeasured bool
	CommitMeasured      bool
}

type IngestPricesResponse struct {
	RequestID          string                  `json:"request_id"`
	Accepted           int                     `json:"accepted"`
	CurrentRowsTouched int64                   `json:"current_rows_touched"`
	Duplicate          bool                    `json:"duplicate"`
	PersistenceTiming  IngestPersistenceTiming `json:"-"`
}

type IngestPricesResult struct {
	Accepted           int
	CurrentRowsTouched int64
	Duplicate          bool
	PersistenceTiming  IngestPersistenceTiming
}

// IngestHistoryRequest is the authenticated receiver-to-central-API contract.
// Each entry represents one normalized market-history capture and contains the
// time buckets delivered by Albion Online Data Project.
type IngestHistoryRequest struct {
	RequestID string          `json:"request_id"`
	Server    Server          `json:"server"`
	Entries   []HistoryIngest `json:"entries"`
}

type HistoryIngest struct {
	ObservedAt time.Time             `json:"observed_at"`
	LocationID int16                 `json:"location_id"`
	ItemKey    string                `json:"item_key"`
	Quality    int16                 `json:"quality"`
	History    []HistoryBucketIngest `json:"history"`
}

type HistoryBucketIngest struct {
	Timestamp        time.Time `json:"timestamp"`
	ItemCount        int64     `json:"item_count"`
	AverageUnitPrice *int64    `json:"average_unit_price"`
}

type IngestHistoryResponse struct {
	RequestID          string                  `json:"request_id"`
	AcceptedEntries    int                     `json:"accepted_entries"`
	AcceptedBuckets    int                     `json:"accepted_buckets"`
	HistoryRowsTouched int64                   `json:"history_rows_touched"`
	Duplicate          bool                    `json:"duplicate"`
	PersistenceTiming  IngestPersistenceTiming `json:"-"`
}

type IngestHistoryResult struct {
	AcceptedEntries    int
	AcceptedBuckets    int
	HistoryRowsTouched int64
	Duplicate          bool
	PersistenceTiming  IngestPersistenceTiming
}

type MarketType string

const (
	MarketTypeRegular     MarketType = "regular"
	MarketTypeBlackMarket MarketType = "black-market"
)

// MarketDefinition is the public market catalog contract. Internal Albion
// location IDs are deliberately omitted so frontend clients depend only on the
// stable key.
type MarketDefinition struct {
	Key     string     `json:"key"`
	Name    string     `json:"name"`
	Type    MarketType `json:"type"`
	Enabled bool       `json:"enabled"`
}

type MarketCatalogResponse struct {
	SchemaVersion int                `json:"schemaVersion"`
	Count         int                `json:"count"`
	Data          []MarketDefinition `json:"data"`
}

// PriceQueryRequest is the browser-facing batch contract. marketKeys are
// stable public identifiers such as "martlock" or "fort_sterling".
type PriceQueryRequest struct {
	Server     Server            `json:"server"`
	MarketKeys []string          `json:"marketKeys"`
	Entries    []PriceQueryEntry `json:"entries"`
}

type PriceQueryEntry struct {
	ItemKey string `json:"itemIdentifier"`
	Quality int16  `json:"quality"`
}

// CurrentPriceLookup is internal to the service/repository boundary. It keeps
// numeric locations out of HTTP request and response contracts.
type CurrentPriceLookup struct {
	Server      Server
	LocationIDs []int16
	Entries     []PriceQueryEntry
}

type CurrentPrice struct {
	Server                Server     `json:"server"`
	MarketKey             string     `json:"marketKey"`
	LocationID            int16      `json:"-"`
	ItemKey               string     `json:"itemIdentifier"`
	Quality               int16      `json:"quality"`
	SellPriceMin          *int64     `json:"sellPriceMin"`
	SellPriceMinAt        *time.Time `json:"sellPriceMinDate"`
	BuyPriceMax           *int64     `json:"buyPriceMax"`
	BuyPriceMaxAt         *time.Time `json:"buyPriceMaxDate"`
	UpdatedAt             time.Time  `json:"updatedAt"`
	HistoryObservations7D int64      `json:"historyObservations7d"`
	HistoryVolume7D       int64      `json:"historyVolume7d"`
	MedianPrice7D         *int64     `json:"medianPrice7d"`
}

type PriceQueryResponse struct {
	RequestedAt time.Time      `json:"requestedAt"`
	Count       int            `json:"count"`
	Data        []CurrentPrice `json:"data"`
}

// HistoryQueryRequest is the browser-facing batch contract. rangeStart and
// rangeEnd are inclusive UTC dates in YYYY-MM-DD format.
type HistoryQueryRequest struct {
	Server     Server              `json:"server"`
	MarketKeys []string            `json:"marketKeys"`
	Entries    []HistoryQueryEntry `json:"entries"`
	RangeStart string              `json:"rangeStart"`
	RangeEnd   string              `json:"rangeEnd"`
}

type HistoryQueryEntry struct {
	ItemKey string `json:"itemIdentifier"`
	Quality int16  `json:"quality"`
}

// MarketHistoryLookup is internal and therefore may contain numeric location
// IDs and parsed timestamps. RangeEndExclusive is the UTC instant immediately
// after the inclusive public rangeEnd date.
type MarketHistoryLookup struct {
	Server            Server
	LocationIDs       []int16
	Entries           []HistoryQueryEntry
	RangeStart        time.Time
	RangeEndExclusive time.Time
}

type MarketHistoryPoint struct {
	Timestamp        time.Time `json:"timestamp"`
	ItemCount        int64     `json:"itemCount"`
	AverageUnitPrice *int64    `json:"averageUnitPrice"`
}

type MarketHistorySeries struct {
	Server     Server               `json:"server"`
	MarketKey  string               `json:"marketKey"`
	LocationID int16                `json:"-"`
	ItemKey    string               `json:"itemIdentifier"`
	Quality    int16                `json:"quality"`
	History    []MarketHistoryPoint `json:"history"`
}

type HistoryQueryResponse struct {
	RequestedAt time.Time             `json:"requestedAt"`
	RangeStart  string                `json:"rangeStart"`
	RangeEnd    string                `json:"rangeEnd"`
	Count       int                   `json:"count"`
	BucketCount int                   `json:"bucketCount"`
	Data        []MarketHistorySeries `json:"data"`
}
