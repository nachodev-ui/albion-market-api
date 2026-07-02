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

type HTTPMetricsSnapshot struct {
	InFlight  int64
	Requests  map[httpRequestKey]uint64
	Durations map[httpDurationKey]durationHistogram
}

type HTTPMetrics struct {
	mu sync.RWMutex

	inFlight  int64
	requests  map[httpRequestKey]uint64
	durations map[httpDurationKey]durationHistogram
}

func NewHTTPMetrics() *HTTPMetrics {
	return &HTTPMetrics{
		requests:  make(map[httpRequestKey]uint64),
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
	key := httpDurationKey{Method: method, Route: route}
	histogram := m.durations[key]
	histogram.observe(duration)
	m.durations[key] = histogram
}

func (m *HTTPMetrics) Snapshot() HTTPMetricsSnapshot {
	if m == nil {
		return HTTPMetricsSnapshot{
			Requests:  map[httpRequestKey]uint64{},
			Durations: map[httpDurationKey]durationHistogram{},
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	requests := make(map[httpRequestKey]uint64, len(m.requests))
	for key, value := range m.requests {
		requests[key] = value
	}
	durations := make(map[httpDurationKey]durationHistogram, len(m.durations))
	for key, value := range m.durations {
		durations[key] = value
	}
	return HTTPMetricsSnapshot{
		InFlight:  m.inFlight,
		Requests:  requests,
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
	operation = normalizeMetricLabel(operation, "unknown")
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
	Service       string
	Environment   string
	Version       string
	Revision      string
	StartedAt     time.Time
	HTTP          *HTTPMetrics
	Database      *DatabaseMetrics
	DatabasePool  DatabaseMonitor
	Ingest        *IngestMetrics
	HistoryIngest *HistoryIngestMetrics
}

type PrometheusExporter struct {
	service       string
	environment   string
	version       string
	revision      string
	startedAt     time.Time
	http          *HTTPMetrics
	database      *DatabaseMetrics
	databasePool  DatabaseMonitor
	ingest        *IngestMetrics
	historyIngest *HistoryIngestMetrics
}

func NewPrometheusExporter(options PrometheusExporterOptions) *PrometheusExporter {
	startedAt := options.StartedAt.UTC()
	if startedAt.IsZero() {
		startedAt = time.Now().UTC()
	}
	return &PrometheusExporter{
		service:       normalizeMetricLabel(options.Service, "albion-market-api"),
		environment:   normalizeMetricLabel(options.Environment, "unknown"),
		version:       normalizeMetricLabel(options.Version, "dev"),
		revision:      normalizeMetricLabel(options.Revision, "unknown"),
		startedAt:     startedAt,
		http:          options.HTTP,
		database:      options.Database,
		databasePool:  options.DatabasePool,
		ingest:        options.Ingest,
		historyIngest: options.HistoryIngest,
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

func (e *PrometheusExporter) writeDatabase(ctx context.Context, writer io.Writer) {
	if e.databasePool != nil {
		snapshot := e.databasePool.Snapshot(ctx)
		ready := 0.0
		if snapshot.Healthy {
			ready = 1
		}
		writeHelpType(writer, "albion_market_api_database_ready", "Whether PostgreSQL answered the most recent metrics-time ping.", "gauge")
		writeMetric(writer, "albion_market_api_database_ready", nil, ready)
		writeHelpType(writer, "albion_market_api_database_ping_duration_seconds", "Duration of the most recent metrics-time PostgreSQL ping.", "gauge")
		writeMetric(writer, "albion_market_api_database_ping_duration_seconds", nil, snapshot.PingLatency.Seconds())

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
}

func (e *PrometheusExporter) writeIngest(writer io.Writer, stream string, metrics *IngestMetrics) {
	if metrics == nil {
		return
	}
	snapshot := metrics.Snapshot()
	writeIngestCommon(
		writer,
		stream,
		snapshot.RequestsTotal,
		snapshot.InFlight,
		snapshot.SucceededTotal,
		snapshot.DuplicatesTotal,
		snapshot.ErrorsTotal,
		snapshot.AcceptedEntriesTotal,
		snapshot.CurrentRowsTouchedTotal,
		snapshot.DurationTotal,
		snapshot.LastRequestAt,
		snapshot.LastSuccessAt,
		snapshot.LastErrorAt,
	)
}

func (e *PrometheusExporter) writeHistoryIngest(writer io.Writer, stream string, metrics *HistoryIngestMetrics) {
	if metrics == nil {
		return
	}
	snapshot := metrics.Snapshot()
	writeIngestCommon(
		writer,
		stream,
		snapshot.RequestsTotal,
		snapshot.InFlight,
		snapshot.SucceededTotal,
		snapshot.DuplicatesTotal,
		snapshot.ErrorsTotal,
		snapshot.AcceptedEntriesTotal,
		snapshot.HistoryRowsTouchedTotal,
		snapshot.DurationTotal,
		snapshot.LastRequestAt,
		snapshot.LastSuccessAt,
		snapshot.LastErrorAt,
	)
	writeMetric(writer, "albion_market_api_ingest_accepted_buckets_total", map[string]string{"stream": stream}, float64(snapshot.AcceptedBucketsTotal))
}

func writeIngestCommon(
	writer io.Writer,
	stream string,
	requests uint64,
	inFlight uint64,
	succeeded uint64,
	duplicates uint64,
	errors uint64,
	accepted uint64,
	rowsTouched uint64,
	durationTotal time.Duration,
	lastRequest *time.Time,
	lastSuccess *time.Time,
	lastError *time.Time,
) {
	newSuccesses := succeeded
	if duplicates <= succeeded {
		newSuccesses -= duplicates
	}

	writeMetric(writer, "albion_market_api_ingest_requests_total", map[string]string{"result": "success", "stream": stream}, float64(newSuccesses))
	writeMetric(writer, "albion_market_api_ingest_requests_total", map[string]string{"result": "duplicate", "stream": stream}, float64(duplicates))
	writeMetric(writer, "albion_market_api_ingest_requests_total", map[string]string{"result": "error", "stream": stream}, float64(errors))
	writeMetric(writer, "albion_market_api_ingest_requests_in_flight", map[string]string{"stream": stream}, float64(inFlight))
	writeMetric(writer, "albion_market_api_ingest_accepted_entries_total", map[string]string{"stream": stream}, float64(accepted))
	writeMetric(writer, "albion_market_api_ingest_rows_touched_total", map[string]string{"stream": stream}, float64(rowsTouched))
	writeMetric(writer, "albion_market_api_ingest_request_duration_seconds_sum", map[string]string{"stream": stream}, durationTotal.Seconds())
	writeMetric(writer, "albion_market_api_ingest_request_duration_seconds_count", map[string]string{"stream": stream}, float64(succeeded+errors))
	writeMetric(writer, "albion_market_api_ingest_observed_requests_total", map[string]string{"stream": stream}, float64(requests))
	writeOptionalTimestamp(writer, "albion_market_api_ingest_last_request_timestamp_seconds", stream, lastRequest)
	writeOptionalTimestamp(writer, "albion_market_api_ingest_last_success_timestamp_seconds", stream, lastSuccess)
	writeOptionalTimestamp(writer, "albion_market_api_ingest_last_error_timestamp_seconds", stream, lastError)
}

func writeIngestMetricDescriptions(writer io.Writer) {
	writeHelpType(writer, "albion_market_api_ingest_requests_total", "Ingest requests by stream and outcome.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_requests_in_flight", "Ingest requests currently being processed.", "gauge")
	writeHelpType(writer, "albion_market_api_ingest_accepted_entries_total", "Accepted entries for non-duplicate ingest requests.", "counter")
	writeHelpType(writer, "albion_market_api_ingest_accepted_buckets_total", "Accepted history buckets for non-duplicate requests.", "counter")
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

func normalizeHTTPMethod(method string) string {
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE":
		return strings.ToUpper(strings.TrimSpace(method))
	default:
		return "OTHER"
	}
}
