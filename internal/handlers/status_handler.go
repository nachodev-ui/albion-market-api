package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

const databaseStatusTimeout = 2 * time.Second

type StatusHandler struct {
	serviceName   string
	environment   string
	startedAt     time.Time
	database      observability.DatabaseMonitor
	ingest        *observability.IngestMetrics
	historyIngest *observability.HistoryIngestMetrics
}

func NewStatusHandler(
	serviceName string,
	environment string,
	startedAt time.Time,
	database observability.DatabaseMonitor,
	ingest *observability.IngestMetrics,
	historyIngest ...*observability.HistoryIngestMetrics,
) *StatusHandler {
	historyMetrics := observability.NewHistoryIngestMetrics()
	if len(historyIngest) > 0 && historyIngest[0] != nil {
		historyMetrics = historyIngest[0]
	}
	return &StatusHandler{
		serviceName:   serviceName,
		environment:   environment,
		startedAt:     startedAt.UTC(),
		database:      database,
		ingest:        ingest,
		historyIngest: historyMetrics,
	}
}

func (h *StatusHandler) Status(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{
			"error": "method not allowed",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), databaseStatusTimeout)
	defer cancel()

	now := time.Now().UTC()
	database := h.database.Snapshot(ctx)
	ingest := h.ingest.Snapshot()
	historyIngest := h.historyIngest.Snapshot()

	status := "ok"
	httpStatus := http.StatusOK
	if !database.Healthy {
		status = "degraded"
		httpStatus = http.StatusServiceUnavailable
	}

	response := statusResponse{
		Status:        status,
		Service:       h.serviceName,
		Environment:   h.environment,
		StartedAt:     h.startedAt,
		Now:           now,
		UptimeSeconds: int64(now.Sub(h.startedAt).Seconds()),
		Database: databaseStatusResponse{
			Status:        databaseStatus(database.Healthy),
			PingLatencyMS: milliseconds(database.PingLatency),
			Pool: databasePoolStatusResponse{
				MaxConnections:          database.Pool.MaxConnections,
				TotalConnections:        database.Pool.TotalConnections,
				AcquiredConnections:     database.Pool.AcquiredConnections,
				IdleConnections:         database.Pool.IdleConnections,
				ConstructingConnections: database.Pool.ConstructingConnections,
				AcquireCount:            database.Pool.AcquireCount,
				EmptyAcquireCount:       database.Pool.EmptyAcquireCount,
				CanceledAcquireCount:    database.Pool.CanceledAcquireCount,
				NewConnectionsCount:     database.Pool.NewConnectionsCount,
				AcquireDurationMS:       milliseconds(database.Pool.AcquireDuration),
			},
		},
		Ingest: ingestStatusResponse{
			RequestsTotal:           ingest.RequestsTotal,
			InFlight:                ingest.InFlight,
			SucceededTotal:          ingest.SucceededTotal,
			DuplicatesTotal:         ingest.DuplicatesTotal,
			ErrorsTotal:             ingest.ErrorsTotal,
			AcceptedEntriesTotal:    ingest.AcceptedEntriesTotal,
			CurrentRowsTouchedTotal: ingest.CurrentRowsTouchedTotal,
			AverageDurationMS:       milliseconds(ingest.AverageDuration),
			LastDurationMS:          milliseconds(ingest.LastDuration),
			MaxDurationMS:           milliseconds(ingest.MaxDuration),
			LastRequestAt:           ingest.LastRequestAt,
			LastSuccessAt:           ingest.LastSuccessAt,
			LastErrorAt:             ingest.LastErrorAt,
			LastErrorKind:           ingest.LastErrorKind,
		},
		HistoryIngest: historyIngestStatusResponse{
			RequestsTotal:           historyIngest.RequestsTotal,
			InFlight:                historyIngest.InFlight,
			SucceededTotal:          historyIngest.SucceededTotal,
			DuplicatesTotal:         historyIngest.DuplicatesTotal,
			ErrorsTotal:             historyIngest.ErrorsTotal,
			AcceptedEntriesTotal:    historyIngest.AcceptedEntriesTotal,
			AcceptedBucketsTotal:    historyIngest.AcceptedBucketsTotal,
			HistoryRowsTouchedTotal: historyIngest.HistoryRowsTouchedTotal,
			AverageDurationMS:       milliseconds(historyIngest.AverageDuration),
			LastDurationMS:          milliseconds(historyIngest.LastDuration),
			MaxDurationMS:           milliseconds(historyIngest.MaxDuration),
			LastRequestAt:           historyIngest.LastRequestAt,
			LastSuccessAt:           historyIngest.LastSuccessAt,
			LastErrorAt:             historyIngest.LastErrorAt,
			LastErrorKind:           historyIngest.LastErrorKind,
		},
	}

	writeJSON(w, httpStatus, response)
}

type statusResponse struct {
	Status        string                      `json:"status"`
	Service       string                      `json:"service"`
	Environment   string                      `json:"environment"`
	StartedAt     time.Time                   `json:"started_at"`
	Now           time.Time                   `json:"now"`
	UptimeSeconds int64                       `json:"uptime_seconds"`
	Database      databaseStatusResponse      `json:"database"`
	Ingest        ingestStatusResponse        `json:"ingest"`
	HistoryIngest historyIngestStatusResponse `json:"history_ingest"`
}

type databaseStatusResponse struct {
	Status        string                     `json:"status"`
	PingLatencyMS float64                    `json:"ping_latency_ms"`
	Pool          databasePoolStatusResponse `json:"pool"`
}

type databasePoolStatusResponse struct {
	MaxConnections          int32   `json:"max_connections"`
	TotalConnections        int32   `json:"total_connections"`
	AcquiredConnections     int32   `json:"acquired_connections"`
	IdleConnections         int32   `json:"idle_connections"`
	ConstructingConnections int32   `json:"constructing_connections"`
	AcquireCount            int64   `json:"acquire_count"`
	EmptyAcquireCount       int64   `json:"empty_acquire_count"`
	CanceledAcquireCount    int64   `json:"canceled_acquire_count"`
	NewConnectionsCount     int64   `json:"new_connections_count"`
	AcquireDurationMS       float64 `json:"acquire_duration_ms"`
}

type ingestStatusResponse struct {
	RequestsTotal           uint64     `json:"requests_total"`
	InFlight                uint64     `json:"in_flight"`
	SucceededTotal          uint64     `json:"succeeded_total"`
	DuplicatesTotal         uint64     `json:"duplicates_total"`
	ErrorsTotal             uint64     `json:"errors_total"`
	AcceptedEntriesTotal    uint64     `json:"accepted_entries_total"`
	CurrentRowsTouchedTotal uint64     `json:"current_rows_touched_total"`
	AverageDurationMS       float64    `json:"average_duration_ms"`
	LastDurationMS          float64    `json:"last_duration_ms"`
	MaxDurationMS           float64    `json:"max_duration_ms"`
	LastRequestAt           *time.Time `json:"last_request_at"`
	LastSuccessAt           *time.Time `json:"last_success_at"`
	LastErrorAt             *time.Time `json:"last_error_at"`
	LastErrorKind           string     `json:"last_error_kind"`
}

type historyIngestStatusResponse struct {
	RequestsTotal           uint64     `json:"requests_total"`
	InFlight                uint64     `json:"in_flight"`
	SucceededTotal          uint64     `json:"succeeded_total"`
	DuplicatesTotal         uint64     `json:"duplicates_total"`
	ErrorsTotal             uint64     `json:"errors_total"`
	AcceptedEntriesTotal    uint64     `json:"accepted_entries_total"`
	AcceptedBucketsTotal    uint64     `json:"accepted_buckets_total"`
	HistoryRowsTouchedTotal uint64     `json:"history_rows_touched_total"`
	AverageDurationMS       float64    `json:"average_duration_ms"`
	LastDurationMS          float64    `json:"last_duration_ms"`
	MaxDurationMS           float64    `json:"max_duration_ms"`
	LastRequestAt           *time.Time `json:"last_request_at"`
	LastSuccessAt           *time.Time `json:"last_success_at"`
	LastErrorAt             *time.Time `json:"last_error_at"`
	LastErrorKind           string     `json:"last_error_kind"`
}

func databaseStatus(healthy bool) string {
	if healthy {
		return "ok"
	}
	return "unavailable"
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}
