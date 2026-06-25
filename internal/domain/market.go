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

type IngestPricesResponse struct {
	RequestID          string `json:"request_id"`
	Accepted           int    `json:"accepted"`
	CurrentRowsTouched int64  `json:"current_rows_touched"`
	Duplicate          bool   `json:"duplicate"`
}

type IngestPricesResult struct {
	Accepted           int
	CurrentRowsTouched int64
	Duplicate          bool
}

type PriceQueryRequest struct {
	Server      Server            `json:"server"`
	LocationIDs []int16           `json:"location_ids"`
	Entries     []PriceQueryEntry `json:"entries"`
}

type PriceQueryEntry struct {
	ItemKey string `json:"item_key"`
	Quality int16  `json:"quality"`
}

type CurrentPrice struct {
	Server         Server     `json:"server"`
	LocationID     int16      `json:"location_id"`
	ItemKey        string     `json:"item_key"`
	Quality        int16      `json:"quality"`
	SellPriceMin   *int64     `json:"sell_price_min"`
	SellPriceMinAt *time.Time `json:"sell_price_min_at"`
	BuyPriceMax    *int64     `json:"buy_price_max"`
	BuyPriceMaxAt  *time.Time `json:"buy_price_max_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type PriceQueryResponse struct {
	Prices []CurrentPrice `json:"prices"`
}
