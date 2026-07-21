package observability

import (
	"context"
	"fmt"
	"time"
)

// DataCoverage describes how much of the current read model is represented by
// recent observations for one public server or market.
type DataCoverage struct {
	Key             string
	Name            string
	TotalObjects    int64
	RecentObjects   int64
	LastUpdatedAt   *time.Time
}

// DataTrustSnapshot is a read-only operational projection derived from the
// existing ingest ledgers and current-price table.
type DataTrustSnapshot struct {
	LastPriceReceptionAt   *time.Time
	LastHistoryReceptionAt *time.Time
	TotalObjects           int64
	RecentObjects          int64
	Servers                []DataCoverage
	Markets                []DataCoverage
	Err                    error
}

var knownServers = []DataCoverage{
	{Key: "west", Name: "Americas"},
	{Key: "east", Name: "Asia"},
	{Key: "europe", Name: "Europe"},
}

var knownMarkets = []struct {
	locationID int16
	coverage   DataCoverage
}{
	{2004, DataCoverage{Key: "bridgewatch", Name: "Bridgewatch"}},
	{3008, DataCoverage{Key: "martlock", Name: "Martlock"}},
	{1002, DataCoverage{Key: "lymhurst", Name: "Lymhurst"}},
	{4002, DataCoverage{Key: "fort_sterling", Name: "Fort Sterling"}},
	{7, DataCoverage{Key: "thetford", Name: "Thetford"}},
	{3005, DataCoverage{Key: "caerleon", Name: "Caerleon"}},
	{5003, DataCoverage{Key: "brecilien", Name: "Brecilien"}},
}

// DataTrustSnapshot returns coverage without storing any derived metrics. The
// caller supplies the freshness threshold so the public definition remains
// explicit and testable.
func (m *PgxDatabaseMonitor) DataTrustSnapshot(
	ctx context.Context,
	recentSince time.Time,
) DataTrustSnapshot {
	if m == nil || m.pool == nil {
		return DataTrustSnapshot{Err: fmt.Errorf("database pool is not configured")}
	}

	var snapshot DataTrustSnapshot
	const overviewQuery = `
		select
			(select max(completed_at) from market_ingest_requests where status = 'completed'),
			(select max(completed_at) from market_history_ingest_requests where status = 'completed'),
			count(distinct (server, item_key, quality))::bigint,
			count(distinct (server, item_key, quality)) filter (
				where greatest(sell_price_min_at, buy_price_max_at) >= $1
			)::bigint
		from current_market_prices
	`
	if err := m.pool.QueryRow(ctx, overviewQuery, recentSince).Scan(
		&snapshot.LastPriceReceptionAt,
		&snapshot.LastHistoryReceptionAt,
		&snapshot.TotalObjects,
		&snapshot.RecentObjects,
	); err != nil {
		snapshot.Err = fmt.Errorf("query data trust overview: %w", err)
		return snapshot
	}

	serverByID := make(map[int16]DataCoverage, len(knownServers))
	const serverQuery = `
		select
			server,
			count(distinct (item_key, quality))::bigint,
			count(distinct (item_key, quality)) filter (
				where greatest(sell_price_min_at, buy_price_max_at) >= $1
			)::bigint,
			max(greatest(sell_price_min_at, buy_price_max_at))
		from current_market_prices
		group by server
	`
	rows, err := m.pool.Query(ctx, serverQuery, recentSince)
	if err != nil {
		snapshot.Err = fmt.Errorf("query data trust servers: %w", err)
		return snapshot
	}
	for rows.Next() {
		var serverID int16
		var coverage DataCoverage
		if err := rows.Scan(
			&serverID,
			&coverage.TotalObjects,
			&coverage.RecentObjects,
			&coverage.LastUpdatedAt,
		); err != nil {
			rows.Close()
			snapshot.Err = fmt.Errorf("scan data trust server: %w", err)
			return snapshot
		}
		serverByID[serverID] = coverage
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		snapshot.Err = fmt.Errorf("rows data trust servers: %w", err)
		return snapshot
	}
	rows.Close()

	for index, definition := range knownServers {
		coverage := definition
		if observed, ok := serverByID[int16(index+1)]; ok {
			coverage.TotalObjects = observed.TotalObjects
			coverage.RecentObjects = observed.RecentObjects
			coverage.LastUpdatedAt = observed.LastUpdatedAt
		}
		snapshot.Servers = append(snapshot.Servers, coverage)
	}

	marketByLocation := make(map[int16]DataCoverage, len(knownMarkets))
	const marketQuery = `
		select
			location_id,
			count(distinct (server, item_key, quality))::bigint,
			count(distinct (server, item_key, quality)) filter (
				where greatest(sell_price_min_at, buy_price_max_at) >= $1
			)::bigint,
			max(greatest(sell_price_min_at, buy_price_max_at))
		from current_market_prices
		group by location_id
	`
	rows, err = m.pool.Query(ctx, marketQuery, recentSince)
	if err != nil {
		snapshot.Err = fmt.Errorf("query data trust markets: %w", err)
		return snapshot
	}
	for rows.Next() {
		var locationID int16
		var coverage DataCoverage
		if err := rows.Scan(
			&locationID,
			&coverage.TotalObjects,
			&coverage.RecentObjects,
			&coverage.LastUpdatedAt,
		); err != nil {
			rows.Close()
			snapshot.Err = fmt.Errorf("scan data trust market: %w", err)
			return snapshot
		}
		marketByLocation[locationID] = coverage
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		snapshot.Err = fmt.Errorf("rows data trust markets: %w", err)
		return snapshot
	}
	rows.Close()

	for _, definition := range knownMarkets {
		coverage := definition.coverage
		if observed, ok := marketByLocation[definition.locationID]; ok {
			coverage.TotalObjects = observed.TotalObjects
			coverage.RecentObjects = observed.RecentObjects
			coverage.LastUpdatedAt = observed.LastUpdatedAt
		}
		snapshot.Markets = append(snapshot.Markets, coverage)
	}

	return snapshot
}
