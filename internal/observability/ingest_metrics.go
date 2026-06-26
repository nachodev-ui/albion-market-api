package observability

import (
	"sync"
	"time"
)

type IngestObservation struct {
	CompletedAt        time.Time
	Duration           time.Duration
	StatusCode         int
	Accepted           int
	CurrentRowsTouched int64
	Duplicate          bool
	ErrorKind          string
}

type IngestMetricsSnapshot struct {
	RequestsTotal           uint64
	InFlight                uint64
	SucceededTotal          uint64
	DuplicatesTotal         uint64
	ErrorsTotal             uint64
	AcceptedEntriesTotal    uint64
	CurrentRowsTouchedTotal uint64
	AverageDuration         time.Duration
	LastDuration            time.Duration
	MaxDuration             time.Duration
	LastRequestAt           *time.Time
	LastSuccessAt           *time.Time
	LastErrorAt             *time.Time
	LastErrorKind           string
}

// IngestMetrics stores process-local counters. A mutex keeps snapshots internally
// consistent while allowing handlers to update the metrics concurrently.
type IngestMetrics struct {
	mu sync.RWMutex

	requestsTotal           uint64
	inFlight                uint64
	succeededTotal          uint64
	duplicatesTotal         uint64
	errorsTotal             uint64
	acceptedEntriesTotal    uint64
	currentRowsTouchedTotal uint64
	durationTotal           time.Duration
	lastDuration            time.Duration
	maxDuration             time.Duration
	lastRequestAt           *time.Time
	lastSuccessAt           *time.Time
	lastErrorAt             *time.Time
	lastErrorKind           string
}

func NewIngestMetrics() *IngestMetrics {
	return &IngestMetrics{}
}

func (m *IngestMetrics) RequestStarted(at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requestsTotal++
	m.inFlight++
	timestamp := at.UTC()
	m.lastRequestAt = &timestamp
}

func (m *IngestMetrics) RequestFinished(observation IngestObservation) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.inFlight > 0 {
		m.inFlight--
	}
	m.durationTotal += observation.Duration
	m.lastDuration = observation.Duration
	if observation.Duration > m.maxDuration {
		m.maxDuration = observation.Duration
	}

	completedAt := observation.CompletedAt.UTC()
	if observation.StatusCode >= 200 && observation.StatusCode < 300 {
		m.succeededTotal++
		m.lastSuccessAt = &completedAt
		if observation.Duplicate {
			m.duplicatesTotal++
			return
		}
		m.acceptedEntriesTotal += uint64(max(observation.Accepted, 0))
		m.currentRowsTouchedTotal += uint64(max(observation.CurrentRowsTouched, 0))
		return
	}

	m.errorsTotal++
	m.lastErrorAt = &completedAt
	m.lastErrorKind = observation.ErrorKind
}

func (m *IngestMetrics) Snapshot() IngestMetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	average := time.Duration(0)
	completed := m.succeededTotal + m.errorsTotal
	if completed > 0 {
		average = m.durationTotal / time.Duration(completed)
	}

	return IngestMetricsSnapshot{
		RequestsTotal:           m.requestsTotal,
		InFlight:                m.inFlight,
		SucceededTotal:          m.succeededTotal,
		DuplicatesTotal:         m.duplicatesTotal,
		ErrorsTotal:             m.errorsTotal,
		AcceptedEntriesTotal:    m.acceptedEntriesTotal,
		CurrentRowsTouchedTotal: m.currentRowsTouchedTotal,
		AverageDuration:         average,
		LastDuration:            m.lastDuration,
		MaxDuration:             m.maxDuration,
		LastRequestAt:           cloneTime(m.lastRequestAt),
		LastSuccessAt:           cloneTime(m.lastSuccessAt),
		LastErrorAt:             cloneTime(m.lastErrorAt),
		LastErrorKind:           m.lastErrorKind,
	}
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
