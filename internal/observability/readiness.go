package observability

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const ExpectedSchemaVersion = 18

const (
	ReadinessComponentPool     = "database_pool"
	ReadinessComponentDatabase = "database"
	ReadinessComponentSchema   = "schema"
)

var requiredReadinessRelations = []string{
	"public.account_admin_audit_events",
	"public.app_admins",
	"public.app_schema_state",
	"public.albion_player_events",
	"public.albion_player_profiles",
	"public.app_users",
	"public.billing_notification_outbox",
	"public.billing_orders",
	"public.billing_webhook_events",
	"public.plan_entitlements",
	"public.plans",
	"public.saved_calculations",
	"public.saved_presets",
	"public.subscriptions",
	"public.user_entitlement_overrides",
	"public.current_market_prices",
	"public.market_history_buckets",
	"public.market_history_ingest_raw",
	"public.market_history_ingest_requests",
	"public.market_ingest_raw",
	"public.market_ingest_requests",
}

type ReadinessSnapshot struct {
	Ready           bool
	FailedComponent string
	Duration        time.Duration
	CheckedAt       time.Time
	Err             error
}

type ReadinessChecker interface {
	Check(ctx context.Context) ReadinessSnapshot
}

type ReadinessMetricsSnapshot struct {
	Ready         bool
	Checks        map[string]uint64
	Failures      map[string]uint64
	Durations     durationHistogram
	LastSuccessAt *time.Time
	LastFailureAt *time.Time
}

type ReadinessMetrics struct {
	mu sync.RWMutex

	ready         bool
	checks        map[string]uint64
	failures      map[string]uint64
	durations     durationHistogram
	lastSuccessAt *time.Time
	lastFailureAt *time.Time
}

func NewReadinessMetrics() *ReadinessMetrics {
	return &ReadinessMetrics{
		checks:   make(map[string]uint64),
		failures: make(map[string]uint64),
	}
}

func (m *ReadinessMetrics) Observe(snapshot ReadinessSnapshot) {
	if m == nil {
		return
	}
	result := "success"
	if !snapshot.Ready {
		result = "error"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ready = snapshot.Ready
	m.checks[result]++
	m.durations.observe(snapshot.Duration)
	checkedAt := snapshot.CheckedAt.UTC()
	if snapshot.Ready {
		m.lastSuccessAt = &checkedAt
		return
	}
	component := normalizeReadinessComponent(snapshot.FailedComponent)
	m.failures[component]++
	m.lastFailureAt = &checkedAt
}

func (m *ReadinessMetrics) Snapshot() ReadinessMetricsSnapshot {
	if m == nil {
		return ReadinessMetricsSnapshot{
			Checks:   map[string]uint64{},
			Failures: map[string]uint64{},
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	checks := make(map[string]uint64, len(m.checks))
	for key, value := range m.checks {
		checks[key] = value
	}
	failures := make(map[string]uint64, len(m.failures))
	for key, value := range m.failures {
		failures[key] = value
	}
	return ReadinessMetricsSnapshot{
		Ready:         m.ready,
		Checks:        checks,
		Failures:      failures,
		Durations:     m.durations,
		LastSuccessAt: cloneTime(m.lastSuccessAt),
		LastFailureAt: cloneTime(m.lastFailureAt),
	}
}

type PgxReadinessChecker struct {
	pool                  *pgxpool.Pool
	expectedSchemaVersion int
	requiredRelations     []string
	databaseMetrics       *DatabaseMetrics
	readinessMetrics      *ReadinessMetrics
}

func NewPgxReadinessChecker(
	pool *pgxpool.Pool,
	databaseMetrics *DatabaseMetrics,
	readinessMetrics *ReadinessMetrics,
) *PgxReadinessChecker {
	relations := append([]string(nil), requiredReadinessRelations...)
	sort.Strings(relations)
	return &PgxReadinessChecker{
		pool:                  pool,
		expectedSchemaVersion: ExpectedSchemaVersion,
		requiredRelations:     relations,
		databaseMetrics:       databaseMetrics,
		readinessMetrics:      readinessMetrics,
	}
}

func (c *PgxReadinessChecker) Check(ctx context.Context) (snapshot ReadinessSnapshot) {
	started := time.Now()
	snapshot.CheckedAt = started.UTC()
	defer func() {
		snapshot.Duration = time.Since(started)
		if c != nil {
			c.readinessMetrics.Observe(snapshot)
		}
	}()

	if c == nil || c.pool == nil {
		snapshot.FailedComponent = ReadinessComponentPool
		snapshot.Err = fmt.Errorf("database pool is not configured")
		return
	}

	acquireStarted := time.Now()
	conn, err := c.pool.Acquire(ctx)
	c.databaseMetrics.Observe("readiness_acquire", time.Since(acquireStarted), err)
	if err != nil {
		snapshot.FailedComponent = ReadinessComponentPool
		snapshot.Err = fmt.Errorf("acquire readiness connection: %w", err)
		return
	}
	defer conn.Release()

	pingStarted := time.Now()
	err = conn.Ping(ctx)
	c.databaseMetrics.Observe("readiness_ping", time.Since(pingStarted), err)
	if err != nil {
		snapshot.FailedComponent = ReadinessComponentDatabase
		snapshot.Err = fmt.Errorf("ping readiness connection: %w", err)
		return
	}

	schemaStarted := time.Now()
	missingRelations, schemaVersion, schemaErr := c.checkSchema(ctx, conn)
	if schemaErr == nil && len(missingRelations) > 0 {
		schemaErr = fmt.Errorf("required relations are missing: %v", missingRelations)
	}
	if schemaErr == nil && schemaVersion < c.expectedSchemaVersion {
		schemaErr = fmt.Errorf(
			"schema version %d is older than required version %d",
			schemaVersion,
			c.expectedSchemaVersion,
		)
	}
	c.databaseMetrics.Observe("readiness_schema", time.Since(schemaStarted), schemaErr)
	if schemaErr != nil {
		snapshot.FailedComponent = ReadinessComponentSchema
		snapshot.Err = fmt.Errorf("check readiness schema: %w", schemaErr)
		return
	}

	snapshot.Ready = true
	return
}

func (c *PgxReadinessChecker) checkSchema(
	ctx context.Context,
	conn *pgxpool.Conn,
) ([]string, int, error) {
	const missingRelationsQuery = `
		select relation_name
		from unnest($1::text[]) as required(relation_name)
		where to_regclass(relation_name) is null
		order by relation_name
	`
	rows, err := conn.Query(ctx, missingRelationsQuery, c.requiredRelations)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	missing := make([]string, 0)
	for rows.Next() {
		var relation string
		if err := rows.Scan(&relation); err != nil {
			return nil, 0, err
		}
		missing = append(missing, relation)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, err
	}
	rows.Close()
	if len(missing) > 0 {
		return missing, 0, nil
	}

	const versionQuery = `
		select version
		from public.app_schema_state
		where singleton = true
	`
	var version int
	if err := conn.QueryRow(ctx, versionQuery).Scan(&version); err != nil {
		return nil, 0, err
	}
	return nil, version, nil
}

func normalizeReadinessComponent(component string) string {
	switch component {
	case ReadinessComponentPool, ReadinessComponentDatabase, ReadinessComponentSchema:
		return component
	default:
		return "unknown"
	}
}
