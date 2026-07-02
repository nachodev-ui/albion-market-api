package observability

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DatabasePoolStats struct {
	MaxConnections          int32
	TotalConnections        int32
	AcquiredConnections     int32
	IdleConnections         int32
	ConstructingConnections int32
	AcquireCount            int64
	EmptyAcquireCount       int64
	CanceledAcquireCount    int64
	NewConnectionsCount     int64
	AcquireDuration         time.Duration
}

type DatabaseSnapshot struct {
	Healthy     bool
	PingLatency time.Duration
	Pool        DatabasePoolStats
	Err         error
}

type DatabaseMonitor interface {
	Snapshot(ctx context.Context) DatabaseSnapshot
}

type PgxDatabaseMonitor struct {
	pool *pgxpool.Pool
}

func NewPgxDatabaseMonitor(pool *pgxpool.Pool) *PgxDatabaseMonitor {
	return &PgxDatabaseMonitor{pool: pool}
}

func (m *PgxDatabaseMonitor) Snapshot(ctx context.Context) DatabaseSnapshot {
	started := time.Now()
	err := m.pool.Ping(ctx)
	latency := time.Since(started)
	stats := m.pool.Stat()

	return DatabaseSnapshot{
		Healthy:     err == nil,
		PingLatency: latency,
		Err:         err,
		Pool: DatabasePoolStats{
			MaxConnections:          stats.MaxConns(),
			TotalConnections:        stats.TotalConns(),
			AcquiredConnections:     stats.AcquiredConns(),
			IdleConnections:         stats.IdleConns(),
			ConstructingConnections: stats.ConstructingConns(),
			AcquireCount:            stats.AcquireCount(),
			EmptyAcquireCount:       stats.EmptyAcquireCount(),
			CanceledAcquireCount:    stats.CanceledAcquireCount(),
			NewConnectionsCount:     stats.NewConnsCount(),
			AcquireDuration:         stats.AcquireDuration(),
		},
	}
}
