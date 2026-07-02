package observability

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var defaultDurationBuckets = [...]float64{
	0.005,
	0.010,
	0.025,
	0.050,
	0.100,
	0.250,
	0.500,
	1,
	2.5,
	5,
	10,
}

type durationHistogram struct {
	Count   uint64
	Sum     float64
	Buckets [len(defaultDurationBuckets)]uint64
}

func (h *durationHistogram) observe(duration time.Duration) {
	seconds := duration.Seconds()
	h.Count++
	h.Sum += seconds
	for index, upperBound := range defaultDurationBuckets {
		if seconds <= upperBound {
			h.Buckets[index]++
		}
	}
}

type httpRequestKey struct {
	Method string
	Route  string
	Status string
}

type httpDurationKey struct {
	Method string
	Route  string
}

type httpErrorKey struct {
	Method string
	Route  string
	Class  string
}

type HTTPMetricsSnapshot struct {
	InFlight  int64
	Requests  map[httpRequestKey]uint64
	Errors    map[httpErrorKey]uint64
	Durations map[httpDurationKey]durationHistogram
}

type HTTPMetrics struct {
	mu sync.RWMutex

	inFlight  int64
	requests  map[httpRequestKey]uint64
	errors    map[httpErrorKey]uint64
	durations map[httpDurationKey]durationHistogram
}

func NewHTTPMetrics() *HTTPMetrics {
	return &HTTPMetrics{
		requests:  make(map[httpRequestKey]uint64),
		errors:    make(map[httpErrorKey]uint64),
		durations: make(map[httpDurationKey]durationHistogram),
	}
}

func (m *HTTPMetrics) RequestStarted() {
	if m == nil {
		return
	}
	m.mu.Lock()
	m.inFlight++
	m.mu.Unlock()
}

func (m *HTTPMetrics) RequestFinished(method, route string, status int, duration time.Duration) {
	if m == nil {
		return
	}
	method = normalizeHTTPMethod(method)
	route = normalizeMetricLabel(route, "unmatched")
	statusLabel := strconv.Itoa(status)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.inFlight > 0 {
		m.inFlight--
	}
	m.requests[httpRequestKey{Method: method, Route: route, Status: statusLabel}]++
	if status >= 400 && status <= 599 {
		statusClass := strconv.Itoa(status/100) + "xx"
		m.errors[httpErrorKey{Method: method, Route: route, Class: statusClass}]++
	}
	key := httpDurationKey{Method: method, Route: route}
	histogram := m.durations[key]
	histogram.observe(duration)
	m.durations[key] = histogram
}

func (m *HTTPMetrics) Snapshot() HTTPMetricsSnapshot {
	if m == nil {
		return HTTPMetricsSnapshot{
			Requests:  map[httpRequestKey]uint64{},
			Errors:    map[httpErrorKey]uint64{},
			Durations: map[httpDurationKey]durationHistogram{},
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	requests := make(map[httpRequestKey]uint64, len(m.requests))
	for key, value := range m.requests {
		requests[key] = value
	}
	errors := make(map[httpErrorKey]uint64, len(m.errors))
	for key, value := range m.errors {
		errors[key] = value
	}
	durations := make(map[httpDurationKey]durationHistogram, len(m.durations))
	for key, value := range m.durations {
		durations[key] = value
	}
	return HTTPMetricsSnapshot{
		InFlight:  m.inFlight,
		Requests:  requests,
		Errors:    errors,
		Durations: durations,
	}
}

type databaseOperationKey struct {
	Operation string
	Result    string
}

type DatabaseMetricsSnapshot struct {
	Operations map[databaseOperationKey]uint64
	Durations  map[string]durationHistogram
}

type DatabaseMetrics struct {
	mu sync.RWMutex

	operations map[databaseOperationKey]uint64
	durations  map[string]durationHistogram
}

func NewDatabaseMetrics() *DatabaseMetrics {
	return &DatabaseMetrics{
		operations: make(map[databaseOperationKey]uint64),
		durations:  make(map[string]durationHistogram),
	}
}

func (m *DatabaseMetrics) Observe(operation string, duration time.Duration, err error) {
	if m == nil {
		return
	}
	operation = normalizeDatabaseOperation(operation)
	result := "success"
	if err != nil {
		result = "error"
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.operations[databaseOperationKey{Operation: operation, Result: result}]++
	histogram := m.durations[operation]
	histogram.observe(duration)
	m.durations[operation] = histogram
}

func (m *DatabaseMetrics) Snapshot() DatabaseMetricsSnapshot {
	if m == nil {
		return DatabaseMetricsSnapshot{
			Operations: map[databaseOperationKey]uint64{},
			Durations:  map[string]durationHistogram{},
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	operations := make(map[databaseOperationKey]uint64, len(m.operations))
	for key, value := range m.operations {
		operations[key] = value
	}
	durations := make(map[string]durationHistogram, len(m.durations))
	for key, value := range m.durations {
		durations[key] = value
	}
	return DatabaseMetricsSnapshot{Operations: operations, Durations: durations}
}

type PrometheusExporterOptions struct {
	Service          string
	Environment      string
	Version          string
	Revision         string
	StartedAt        time.Time
	HTTP             *HTTPMetrics
	Database         *DatabaseMetrics
	DatabasePool     DatabaseMonitor
	Ingest           *IngestMetrics
	HistoryIngest    *HistoryIngestMetrics
	Readiness        *ReadinessMetrics
	ReadinessChecker ReadinessChecker
	ReadinessTimeout time.Duration
}

type PrometheusExporter struct {
	service          string
	environment      string
	version          string
	revision         string
	startedAt        time.Time
	http             *HTTPMetrics
	database         *DatabaseMetrics
	databasePool     DatabaseMonitor
	ingest           *IngestMetrics
	historyIngest    *HistoryIngestMetrics
	readiness        *ReadinessMetrics
	readinessChecker ReadinessChecker
	readinessTimeout time.Duration
}

func NewPrometheusExporter(options PrometheusExporterOptions) *PrometheusExporter {
	startedAt := options.StartedAt.UTC()
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	readinessTimeout := options.ReadinessTimeout
	if readinessTimeout <= 0 {
		readinessTimeout = time.Second
	}
	return &PrometheusExporter{
		service:          normalizeMetricLabel(options.Service, "albion-market-api"),
		environment:      normalizeMetricLabel(options.Environment, "unknown"),
		version:          normalizeMetricLabel(options.Version, "dev"),
		revision:         normalizeMetricLabel(options.Revision, "unknown"),
		startedAt:        startedAt,
		http:             options.HTTP,
		database:         options.Database,
		databasePool:     options.DatabasePool,
		ingest:           options.Ingest,
		historyIngest:    options.HistoryIngest,
		readiness:        options.Readiness,
		readinessChecker: options.ReadinessChecker,
		readinessTimeout: readinessTimeout,
	}
}

func (e *PrometheusExporter) Write(ctx context.Context, writer io.Writer) error {
	if e == nil {
		return fmt.Errorf("prometheus exporter is not configured")
	}
	if writer == nil {
		return fmt.Errorf("prometheus writer is nil")
	}

	now := time.Now().UTC()
	writeHelpType(writer, "albion_market_api_build_info", "Static build and deployment metadata.", "gauge")
	writeMetric(writer, "albion_market_api_build_info", map[string]string{
		"environment": e.environment,
		"revision":    e.revision,
		"service":     e.service,
		"version":     e.version,
	}, 1)
	writeHelpType(writer, "albion_market_api_process_start_time_seconds", "Unix timestamp when the API process started.", "gauge")
	writeMetric(writer, "albion_market_api_process_start_time_seconds", nil, float64(e.startedAt.Unix()))
	writeHelpType(writer, "albion_market_api_process_uptime_seconds", "API process uptime in seconds.", "gauge")
	writeMetric(writer, "albion_market_api_process_uptime_seconds", nil, now.Sub(e.startedAt).Seconds())

	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	writeHelpType(writer, "albion_market_api_go_goroutines", "Current number of goroutines.", "gauge")
	writeMetric(writer, "albion_market_api_go_goroutines", nil, float64(runtime.NumGoroutine()))
	writeHelpType(writer, "albion_market_api_go_memory_alloc_bytes", "Bytes currently allocated by the Go runtime.", "gauge")
	writeMetric(writer, "albion_market_api_go_memory_alloc_bytes", nil, float64(memory.Alloc))
	writeHelpType(writer, "albion_market_api_go_memory_heap_inuse_bytes", "Heap bytes currently in use.", "gauge")
	writeMetric(writer, "albion_market_api_go_memory_heap_inuse_bytes", nil, float64(memory.HeapInuse))
	writeHelpType(writer, "albion_market_api_go_gc_cycles_total", "Completed Go garbage collection cycles.", "counter")
	writeMetric(writer, "albion_market_api_go_gc_cycles_total", nil, float64(memory.NumGC))

	e.writeHTTP(writer)
	e.refreshReadiness(ctx)
	e.writeReadiness(writer)
	e.writeDatabase(ctx, writer)
	if e.ingest != nil || e.historyIngest != nil {
		writeIngestMetricDescriptions(writer)
	}
	e.writeIngest(writer, "prices", e.ingest)
	e.writeHistoryIngest(writer, "history", e.historyIngest)
	return nil
}

func (e *PrometheusExporter) writeHTTP(writer io.Writer) {
	snapshot := e.http.Snapshot()
	writeHelpType(writer, "albion_market_api_http_requests_in_flight", "HTTP requests currently being processed.", "gauge")
	writeMetric(writer, "albion_market_api_http_requests_in_flight", nil, float64(snapshot.InFlight))
	writeHelpType(writer, "albion_market_api_http_requests_total", "Completed HTTP requests by method, route and status.", "counter")

	requestKeys := make([]httpRequestKey, 0, len(snapshot.Requests))
	for key := range snapshot.Requests {
		requestKeys = append(requestKeys, key)
	}
	sort.Slice(requestKeys, func(i, j int) bool {
		if requestKeys[i].Route != requestKeys[j].Route {
			return requestKeys[i].Route < requestKeys[j].Route
		}
		if requestKeys[i].Method != requestKeys[j].Method {
			return requestKeys[i].Method < requestKeys[j].Method
		}
		return requestKeys[i].Status < requestKeys[j].Status
	})
	for _, key := range requestKeys {
		writeMetric(writer, "albion_market_api_http_requests_total", map[string]string{
			"method": key.Method,
			"route":  key.Route,
			"status": key.Status,
		}, float64(snapshot.Requests[key]))
	}

	writeHelpType(writer, "albion_market_api_http_errors_total", "HTTP 4xx and 5xx responses by method, route and status class.", "counter")
	errorKeys := make([]httpErrorKey, 0, len(snapshot.Errors))
	for key := range snapshot.Errors {
		errorKeys = append(errorKeys, key)
	}
	sort.Slice(errorKeys, func(i, j int) bool {
		if errorKeys[i].Route != errorKeys[j].Route {
			return errorKeys[i].Route < errorKeys[j].Route
		}
		if errorKeys[i].Method != errorKeys[j].Method {
			return errorKeys[i].Method < errorKeys[j].Method
		}
		return errorKeys[i].Class < errorKeys[j].Class
	})
	for _, key := range errorKeys {
		writeMetric(writer, "albion_market_api_http_errors_total", map[string]string{
			"class":  key.Class,
			"method": key.Method,
			"route":  key.Route,
		}, float64(snapshot.Errors[key]))
	}

	writeHelpType(writer, "albion_market_api_http_request_duration_seconds", "HTTP request duration in seconds.", "histogram")
	durationKeys := make([]httpDurationKey, 0, len(snapshot.Durations))
	for key := range snapshot.Durations {
		durationKeys = append(durationKeys, key)
	}
	sort.Slice(durationKeys, func(i, j int) bool {
		if durationKeys[i].Route != durationKeys[j].Route {
			return durationKeys[i].Route < durationKeys[j].Route
		}
		return durationKeys[i].Method < durationKeys[j].Method
	})
	for _, key := range durationKeys {
		writeHistogram(writer, "albion_market_api_http_request_duration_seconds", map[string]string{
			"method": key.Method,
			"route":  key.Route,
		}, snapshot.Durations[key])
	}
}

func (e *PrometheusExporter) refreshReadiness(ctx context.Context) {
	if e.readinessChecker == nil {
		return
	}
	readinessCtx, cancel := context.WithTimeout(ctx, e.readinessTimeout)
	defer cancel()
	_ = e.readinessChecker.Check(readinessCtx)
}

func (e *PrometheusExporter) writeReadiness(writer io.Writer) {
	if e.readiness == nil {
		return
	}
	snapshot := e.readiness.Snapshot()
	ready := 0.0
	if snapshot.Ready {
		ready = 1
	}
	writeHelpType(writer, "albion_market_api_readiness_ready", "Whether the most recent readiness check succeeded.", "gauge")
	writeMetric(writer, "albion_market_api_readiness_ready", nil, ready)
	writeHelpType(writer, "albion_market_api_readiness_checks_total", "Readiness checks by result.", "counter")
	for _, result := range []string{"success", "error"} {
		writeMetric(writer, "albion_market_api_readiness_checks_total", map[string]string{"result": result}, float64(snapshot.Checks[result]))
	}
	writeHelpType(writer, "albion_market_api_readiness_failures_total", "Readiness failures by bounded component.", "counter")
	components := make([]string, 0, len(snapshot.Failures))
	for component := range snapshot.Failures {
		components = append(components, component)
	}
	sort.Strings(components)
	for _, component := range components {
		writeMetric(writer, "albion_market_api_readiness_failures_total", map[string]string{"component": component}, float64(snapshot.Failures[component]))
	}
	writeHelpType(writer, "albion_market_api_readiness_check_duration_seconds", "Readiness check duration in seconds.", "histogram")
	writeHistogram(writer, "albion_market_api_readiness_check_duration_seconds", nil, snapshot.Durations)
	writeOptionalTimestampWithoutLabels(writer, "albion_market_api_readiness_last_success_timestamp_seconds", snapshot.LastSuccessAt)
	writeOptionalTimestampWithoutLabels(writer, "albion_market_api_readiness_last_failure_timestamp_seconds", snapshot.LastFailureAt)
}

func (e *PrometheusExporter) writeDatabase(ctx context.Context, writer io.Writer) {
	if e.databasePool != nil {
		snapshot := e.databasePool.Snapshot(ctx)
		ready := 0.0
		if snapshot.Healthy {
			ready = 1
		}
		writeHelpType(writer, "albion_market_api_database_ready", "Whether PostgreSQL answered the most recent metrics-time ping.", "gauge")
		writeMetric(writer, "albion_market_api_database_ready", nil, ready)
		writeHelpType(writer, "albion_market_api_database_pool_acquisition_duration_seconds", "Duration of the most recent metrics-time PostgreSQL pool acquisition.", "gauge")
		writeMetric(writer, "albion_market_api_database_pool_acquisition_duration_seconds", nil, snapshot.AcquisitionLatency.Seconds())
		writeHelpType(writer, "albion_market_api_database_ping_duration_seconds", "Duration of the most recent metrics-time PostgreSQL ping.", "gauge")
		writeMetric(writer, "albion_market_api_database_ping_duration_seconds", nil, snapshot.PingLatency.Seconds())
		utilization := 0.0
		if snapshot.Pool.MaxConnections > 0 {
			utilization = float64(snapshot.Pool.AcquiredConnections) / float64(snapshot.Pool.MaxConnections)
		}
		writeHelpType(writer, "albion_market_api_database_pool_utilization_ratio", "Fraction of configured PostgreSQL connections currently acquired.", "gauge")
		writeMetric(writer, "albion_market_api_database_pool_utilization_ratio", nil, utilization)

		poolMetrics := []struct {
			name  string
			help  string
			kind  string
			value float64
		}{
			{"albion_market_api_database_pool_max_connections", "Configured maximum PostgreSQL pool size.", "gauge", float64(snapshot.Pool.MaxConnections)},
			{"albion_market_api_database_pool_total_connections", "Current PostgreSQL pool connections.", "gauge", float64(snapshot.Pool.TotalConnections)},
			{"albion_market_api_database_pool_acquired_connections", "Current acquired PostgreSQL pool connections.", "gauge", float64(snapshot.Pool.AcquiredConnections)},
			{"albion_market_api_database_pool_idle_connections", "Current idle PostgreSQL pool connections.", "gauge", float64(snapshot.Pool.IdleConnections)},
			{"albion_market_api_database_pool_constructing_connections", "PostgreSQL connections currently being established.", "gauge", float64(snapshot.Pool.ConstructingConnections)},
			{"albion_market_api_database_pool_acquire_total", "Total successful pool acquisitions.", "counter", float64(snapshot.Pool.AcquireCount)},
			{"albion_market_api_database_pool_empty_acquire_total", "Pool acquisitions that initially found no idle connection.", "counter", float64(snapshot.Pool.EmptyAcquireCount)},
			{"albion_market_api_database_pool_canceled_acquire_total", "Canceled PostgreSQL pool acquisitions.", "counter", float64(snapshot.Pool.CanceledAcquireCount)},
			{"albion_market_api_database_pool_new_connections_total", "Connections created by the PostgreSQL pool.", "counter", float64(snapshot.Pool.NewConnectionsCount)},
			{"albion_market_api_database_pool_acquire_duration_seconds_total", "Cumulative time spent acquiring PostgreSQL connections.", "counter", snapshot.Pool.AcquireDuration.Seconds()},
		}
		for _, metric := range poolMetrics {
			writeHelpType(writer, metric.name, metric.help, metric.kind)
			writeMetric(writer, metric.name, nil, metric.value)
		}
		averageAcquire := 0.0
		if snapshot.Pool.AcquireCount > 0 {
			averageAcquire = snapshot.Pool.AcquireDuration.Seconds() / float64(snapshot.Pool.AcquireCount)
		}
		writeHelpType(writer, "albion_market_api_database_pool_acquire_duration_seconds_average", "Average PostgreSQL pool acquisition duration since process start.", "gauge")
		writeMetric(writer, "albion_market_api_database_pool_acquire_duration_seconds_average", nil, averageAcquire)
	}

	snapshot := e.database.Snapshot()
	writeHelpType(writer, "albion_market_api_database_operations_total", "Database operations by bounded operation name and result.", "counter")
	operationKeys := make([]databaseOperationKey, 0, len(snapshot.Operations))
	for key := range snapshot.Operations {
		operationKeys = append(operationKeys, key)
	}
	sort.Slice(operationKeys, func(i, j int) bool {
		if operationKeys[i].Operation != operationKeys[j].Operation {
			return operationKeys[i].Operation < operationKeys[j].Operation
		}
		return operationKeys[i].Result < operationKeys[j].Result
	})
	for _, key := range operationKeys {
		writeMetric(writer, "albion_market_api_database_operations_total", map[string]string{
			"operation": key.Operation,
			"result":    key.Result,
		}, float64(snapshot.Operations[key]))
	}
	writeHelpType(writer, "albion_market_api_database_errors_total", "Failed database operations by bounded operation name.", "counter")
	for _, key := range operationKeys {
		if key.Result != "error" {
			continue
		}
		writeMetric(writer, "albion_market_api_database_errors_total", map[string]string{
			"operation": key.Operation,
		}, float64(snapshot.Operations[key]))
	}

	writeHelpType(writer, "albion_market_api_database_operation_duration_seconds", "Database operation duration in seconds.", "histogram")
	operations := make([]string, 0, len(snapshot.Durations))
	for operation := range snapshot.Durations {
		operations = append(operations, operation)
	}
	sort.Strings(operations)
	for _, operation := range operations {
		writeHistogram(writer, "albion_market_api_database_operation_duration_seconds", map[string]string{
			"operation": operation,
		}, snapshot.Durations[operation])
	}
	writeMappedDatabaseHistogram(writer, "albion_market_api_ingest_copy_duration_seconds", "CopyFrom duration by ingest stream.", snapshot.Durations, map[string]string{
		"copy_raw_prices":  "prices",
		"copy_raw_history": "history",
	})
	writeMappedDatabaseHistogram(writer, "albion_market_api_ingest_upsert_duration_seconds", "Hot read-model upsert duration by ingest stream.", snapshot.Durations, map[string]string{
		"upsert_current_prices": "prices",
		"upsert_market_history": "history",
	})
	writeMappedDatabaseHistogram(writer, "albion_market_api_database_transaction_duration_seconds", "Database transaction duration by ingest stream.", snapshot.Durations, map[string]string{
		"transaction_prices":  "prices",
		"transaction_history": "history",
	})
}

func writeMappedDatabaseHistogram(writer io.Writer, name, help string, durations map[string]durationHistogram, operations map[string]string) {
	writeHelpType(writer, name, help, "histogram")
	operationNames := make([]string, 0, len(operations))
	for operation := range operations {
		operationNames = append(operationNames, operation)
	}
	sort.Strings(operationNames)
	for _, operation := range operationNames {
		histogram, ok := durations[operation]
		if !ok {
			continue
		}
		writeHistogram(writer, name, map[string]string{"stream": operations[operation]}, histogram)
	}
}

type ingestExportSnapshot struct {
	requestsTotal         uint64
	inFlight              uint64
	succeededTotal        uint64
	duplicatesTotal       uint64
	errorsTotal           uint64
	receivedEntriesTotal  uint64
	acceptedEntriesTotal  uint64
	storedEntriesTotal    uint64
	rejectedEntriesTotal  uint64
	duplicateEntriesTotal uint64
	rowsTouchedTotal      uint64
	durationTotal         time.Duration
	lastRequestAt         *time.Time
	lastSuccessAt         *time.Time
	lastErrorAt           *time.Time
}

func (e *PrometheusExporter) writeIngest(writer io.Writer, stream string, metrics *IngestMetrics) {
	if metrics == nil {
		return
	}
	snapshot := metrics.Snapshot()
	writeIngestCommon(writer, stream, ingestExportSnapshot{
		requestsTotal:         snapshot.RequestsTotal,
		inFlight:              snapshot.InFlight,
		succeededTotal:        snapshot.SucceededTotal,
		duplicatesTotal:       snapshot.DuplicatesTotal,
		errorsTotal:           snapshot.ErrorsTotal,
		receivedEntriesTotal:  snapshot.ReceivedEntriesTotal,
		acceptedEntriesTotal:  snapshot.AcceptedEntriesTotal,
		storedEntriesTotal:    snapshot.StoredEntriesTotal,
		rejectedEntriesTotal:  snapshot.RejectedEntriesTotal,
		duplicateEntriesTotal: snapshot.DuplicateEntriesTotal,
		rowsTouchedTotal:      snapshot.CurrentRowsTouchedTotal,
		durationTotal:         snapshot.DurationTotal,
		lastRequestAt:         snapshot.LastRequestAt,
		lastSuccessAt:         snapshot.LastSuccessAt,
		lastErrorAt:           snapshot.LastErrorAt,
	})
}

func (e *PrometheusExporter) writeHistoryIngest(writer io.Writer, stream string, metrics *HistoryIngestMetrics) {
	if metrics == nil {
		return
	}
	snapshot := metrics.Snapshot()
	writeIngestCommon(writer, stream, ingestExportSnapshot{
		requestsTotal:         snapshot.RequestsTotal,
		inFlight:              snapshot.InFlight,
		succeededTotal:        snapshot.SucceededTotal,
		duplicatesTotal:       snapshot.DuplicatesTotal,
		errorsTotal:           snapshot.ErrorsTotal,
		receivedEntriesTotal:  snapshot.ReceivedEntriesTotal,
		acceptedEntriesTotal:  snapshot.AcceptedEntriesTotal,
		storedEntriesTotal:    snapshot.AcceptedEntriesTotal,
		rejectedEntriesTotal:  snapshot.RejectedEntriesTotal,
		duplicateEntriesTotal: snapshot.DuplicateEntriesTotal,
		rowsTouchedTotal:      snapshot.HistoryRowsTouchedTotal,
		durationTotal:         snapshot.DurationTotal,
		lastRequestAt:         snapshot.LastRequestAt,
		lastSuccessAt:         snapshot.LastSuccessAt,
		lastErrorAt:           snapshot.LastErrorAt,
	})
	labels := map[string]string{"stream": stream}
	writeMetric(writer, "albion_market_api_ingest_buckets_received_total", labels, float64(snapshot.ReceivedBucketsTotal))
	writeMetric(writer, "albion_market_api_ingest_accepted_buckets_total", labels, float64(snapshot.AcceptedBucketsTotal))
	writeMetric(writer, "albion_market_api_ingest_buckets_stored_total", labels, float64(snapshot.StoredBucketsTotal))
	writeMetric(writer, "albion_market_api_ingest_buckets_rejected_total", labels, float64(snapshot.RejectedBucketsTotal))
	writeMetric(writer, "albion_market_api_ingest_buckets_duplicate_total", labels, float64(snapshot.DuplicateBucketsTotal))
}

func writeIngestCommon(writer io.Writer, stream string, snapshot ingestExportSnapshot) {
	newSuccesses := snapshot.succeededTotal
	if snapshot.duplicatesTotal <= snapshot.succeededTotal {
		newSuccesses -= snapshot.duplicatesTotal
	}
	labels := map[string]string{"stream": stream}

	results := []struct {
		name  string
		value uint64
	}{
		{name: "success", value: newSuccesses},
		{name: "duplicate", value: snapshot.duplicatesTotal},
		{name: "error", value: snapshot.errorsTotal},
	}
	for _, result := range results {
		resultLabels := map[string]string{"result": result.name, "stream": stream}
		writeMetric(writer, "albion_market_api_ingest_requests_total", resultLabels, float64(result.value))
		writeMetric(writer, "albion_market_api_ingest_batches_total", resultLabels, float64(result.value))
	}
	writeMetric(writer, "albion_market_api_ingest_errors_total", labels, float64(snapshot.errorsTotal))
	writeMetric(writer, "albion_market_api_ingest_requests_in_flight", labels, float64(snapshot.inFlight))
	writeMetric(writer, "albion_market_api_ingest_entries_received_total", labels, float64(snapshot.receivedEntriesTotal))
	writeMetric(writer, "albion_market_api_ingest_accepted_entries_total", labels, float64(snapshot.acceptedEntriesTotal))
	writeMetric(writer, "albion_market_api_ingest_entries_stored_total", labels, float64(snapshot.storedEntriesTotal))
	writeMetric(writer, "albion_market_api_ingest_entries_rejected_total", labels, float64(snapshot.rejectedEntriesTotal))
	writeMetric(writer, "albion_market_api_ingest_entries_duplicate_total", labels, float64(snapshot.duplicateEntriesTotal))
	writeMetric(writer, "albion_market_api_ingest_rows_touched_total", labels, float64(snapshot.rowsTouchedTotal))
	writeMetric(writer, "albion_market_api_ingest_request_duration_seconds_sum", labels, snapshot.durationTotal.Seconds())
	writeMetric(writer, "albion_market_api_ingest_request_duration_seconds_count", labels, float64(snapshot.succeededTotal+snapshot.errorsTotal))
	writeMetric(writer, "albion_market_api_ingest_observed_requests_total", labels, float64(snapshot.requestsTotal))
	writeOptionalTimestamp(writer, "albion_market_api_ingest_last_request_timestamp_seconds", stream, snapshot.lastRequestAt)
	writeOptionalTimestamp(writer, "albion_market_api_ingest_last_success_timestamp_seconds", stream, snapshot.lastSuccessAt)
	writeOptionalTimestamp(writer, "albion_market_api_ingest_last_error_timestamp_seconds", stream, snapshot.lastErrorAt)
}

func writeIngestMetricDescriptions(writer io.Writer) {
	writeHelpType(writer, "albion_market_api_ingest_requests_total", "Ingest requests by stream and outcome.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_batches_total", "Completed ingest batches by stream and outcome.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_errors_total", "Failed ingest batches by stream.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_requests_in_flight", "Ingest requests currently being processed.", "gauge")
	writeHelpType(writer, "albion_market_api_ingest_entries_received_total", "Parsed entries received by ingest stream.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_accepted_entries_total", "Accepted entries for non-duplicate ingest requests.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_entries_stored_total", "Entries stored by non-duplicate ingest requests.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_entries_rejected_total", "Parsed entries rejected with a failed ingest request.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_entries_duplicate_total", "Entries associated with duplicate ingest requests.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_buckets_received_total", "Parsed history buckets received.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_accepted_buckets_total", "Accepted history buckets for non-duplicate requests.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_buckets_stored_total", "History buckets stored by non-duplicate requests.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_buckets_rejected_total", "Parsed history buckets rejected with a failed request.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_buckets_duplicate_total", "History buckets associated with duplicate requests.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_rows_touched_total", "Read-model rows touched by non-duplicate ingest requests.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_request_duration_seconds", "Ingest request duration summary without quantiles.", "summary")
	writeHelpType(writer, "albion_market_api_ingest_observed_requests_total", "All ingest requests observed by the process.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_last_request_timestamp_seconds", "Timestamp of the last ingest request.", "gauge")
	writeHelpType(writer, "albion_market_api_ingest_last_success_timestamp_seconds", "Timestamp of the last successful ingest request.", "gauge")
	writeHelpType(writer, "albion_market_api_ingest_last_error_timestamp_seconds", "Timestamp of the last failed ingest request.", "gauge")
}

func writeOptionalTimestamp(writer io.Writer, name, stream string, value *time.Time) {
	if value != nil {
		writeMetric(writer, name, map[string]string{"stream": stream}, float64(value.UTC().Unix()))
	}
}

func writeOptionalTimestampWithoutLabels(writer io.Writer, name string, value *time.Time) {
	if value != nil {
		writeMetric(writer, name, nil, float64(value.UTC().Unix()))
	}
}

func writeHistogram(writer io.Writer, name string, labels map[string]string, histogram durationHistogram) {
	for index, upperBound := range defaultDurationBuckets {
		bucketLabels := copyLabels(labels)
		bucketLabels["le"] = strconv.FormatFloat(upperBound, 'g', -1, 64)
		writeMetric(writer, name+"_bucket", bucketLabels, float64(histogram.Buckets[index]))
	}
	infinityLabels := copyLabels(labels)
	infinityLabels["le"] = "+Inf"
	writeMetric(writer, name+"_bucket", infinityLabels, float64(histogram.Count))
	writeMetric(writer, name+"_sum", labels, histogram.Sum)
	writeMetric(writer, name+"_count", labels, float64(histogram.Count))
}

func writeHelpType(writer io.Writer, name, help, metricType string) {
	_, _ = fmt.Fprintf(writer, "# HELP %s %s\n", name, strings.ReplaceAll(help, "\n", " "))
	_, _ = fmt.Fprintf(writer, "# TYPE %s %s\n", name, metricType)
}

func writeMetric(writer io.Writer, name string, labels map[string]string, value float64) {
	_, _ = io.WriteString(writer, name)
	if len(labels) > 0 {
		keys := make([]string, 0, len(labels))
		for key := range labels {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		_, _ = io.WriteString(writer, "{")
		for index, key := range keys {
			if index > 0 {
				_, _ = io.WriteString(writer, ",")
			}
			_, _ = fmt.Fprintf(writer, "%s=\"%s\"", key, escapePrometheusLabel(labels[key]))
		}
		_, _ = io.WriteString(writer, "}")
	}
	_, _ = fmt.Fprintf(writer, " %s\n", strconv.FormatFloat(value, 'g', -1, 64))
}

func copyLabels(labels map[string]string) map[string]string {
	copy := make(map[string]string, len(labels)+1)
	for key, value := range labels {
		copy[key] = value
	}
	return copy
}

func escapePrometheusLabel(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\n", "\\n")
	return strings.ReplaceAll(value, "\"", "\\\"")
}

func normalizeMetricLabel(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func normalizeDatabaseOperation(operation string) string {
	switch strings.TrimSpace(operation) {
	case "copy_raw_history",
		"copy_raw_prices",
		"ingest_history",
		"ingest_prices",
		"ping",
		"query_current_prices",
		"query_market_history",
		"readiness_acquire",
		"readiness_ping",
		"readiness_schema",
		"transaction_history",
		"transaction_prices",
		"upsert_current_prices",
		"upsert_market_history":
		return strings.TrimSpace(operation)
	default:
		return "unknown"
	}
}

func normalizeHTTPMethod(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
		return strings.ToUpper(strings.TrimSpace(method))
	default:
		return "OTHER"
	}
}
