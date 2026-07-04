package repository

import (
	"errors"

	"github.com/jackc/pgx/v5"
)

var (
	ErrRequestAlreadyProcessing = errors.New("request_id is already processing")
	ErrRequestPayloadConflict   = errors.New("request_id was already used with a different payload")
	errCopySourceNotPositioned  = errors.New("raw price copy source is not positioned on a row")
)

var (
	marketIngestRawTable   = pgx.Identifier{"market_ingest_raw"}
	marketIngestRawColumns = []string{
		"request_id",
		"server",
		"observed_at",
		"location_id",
		"item_key",
		"quality",
		"sell_price_min",
		"sell_price_min_at",
		"buy_price_max",
		"buy_price_max_at",
	}
)
