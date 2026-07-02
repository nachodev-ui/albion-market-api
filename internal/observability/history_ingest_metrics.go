package observability

import (
	"sync"
	"time"
)

type HistoryIngestObservation struct {
	CompletedAt        time.Time
	Duration           time.Duration
	StatusCode         int
	ReceivedEntries    int
	ReceivedBuckets    int
	AcceptedEntries    int
	AcceptedBuckets    int
	StoredBuckets      int
	RejectedEntries    int
	RejectedBuckets    int
	DuplicateEntries   int
	DuplicateBuckets   int
	HistoryRowsTouched int64
	Duplicate          bool
	ErrorKind          string
}

type HistoryIngestMetricsSnapshot struct {
	RequestsTotal           uint64
	InFlight                uint64
	SucceededTotal          uint64
	DuplicatesTotal         uint64
	ErrorsTotal             uint64
	ReceivedEntriesTotal    uint64
	ReceivedBucketsTotal    uint64
	AcceptedEntriesTotal    uint64
	AcceptedBucketsTotal    uint64
	StoredBucketsTotal      uint64
	RejectedEntriesTotal    uint64
	RejectedBucketsTotal    uint64
	DuplicateEntriesTotal   uint64
	DuplicateBucketsTotal   uint64
	HistoryRowsTouchedTotal uint64
	DurationTotal           time.Duration
	AverageDuration         time.Duration
	LastDuration            time.Duration
	MaxDuration             time.Duration
	LastRequestAt           *time.Time
	LastSuccessAt           *time.Time
	LastErrorAt             *time.Time
	LastErrorKind           string
}

type HistoryIngestMetrics struct {
	mu sync.RWMutex

	requestsTotal           uint64
	inFlight                uint64
	succeededTotal          uint64
	duplicatesTotal         uint64
	errorsTotal             uint64
	receivedEntriesTotal    uint64
	receivedBucketsTotal    uint64
	acceptedEntriesTotal    uint64
	acceptedBucketsTotal    uint64
	storedBucketsTotal      uint64
	rejectedEntriesTotal    uint64
	rejectedBucketsTotal    uint64
	duplicateEntriesTotal   uint64
	duplicateBucketsTotal   uint64
	historyRowsTouchedTotal uint64
	durationTotal           time.Duration
	lastDuration            time.Duration
	maxDuration             time.Duration
	lastRequestAt           *time.Time
	lastSuccessAt           *time.Time
	lastErrorAt             *time.Time
	lastErrorKind           string
}

func NewHistoryIngestMetrics() *HistoryIngestMetrics {
	return &HistoryIngestMetrics{}
}

func (m *HistoryIngestMetrics) RequestStarted(at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requestsTotal++
	m.inFlight++
	timestamp := at.UTC()
	m.lastRequestAt = &timestamp
}

func (m *HistoryIngestMetrics) RequestFinished(observation HistoryIngestObservation) {
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
	m.receivedEntriesTotal += uint64(max(observation.ReceivedEntries, 0))
	m.receivedBucketsTotal += uint64(max(observation.ReceivedBuckets, 0))
	m.rejectedEntriesTotal += uint64(max(observation.RejectedEntries, 0))
	m.rejectedBucketsTotal += uint64(max(observation.RejectedBuckets, 0))

	completedAt := observation.CompletedAt.UTC()
	if observation.StatusCode >= 200 && observation.StatusCode < 300 {
		m.succeededTotal++
		m.lastSuccessAt = &completedAt
		if observation.Duplicate {
			m.duplicatesTotal++
			m.duplicateEntriesTotal += uint64(max(observation.DuplicateEntries, 0))
			m.duplicateBucketsTotal += uint64(max(observation.DuplicateBuckets, 0))
			return
		}
		m.acceptedEntriesTotal += uint64(max(observation.AcceptedEntries, 0))
		m.acceptedBucketsTotal += uint64(max(observation.AcceptedBuckets, 0))
		m.storedBucketsTotal += uint64(max(observation.StoredBuckets, 0))
		m.historyRowsTouchedTotal += uint64(max(observation.HistoryRowsTouched, 0))
		return
	}

	m.errorsTotal++
	m.lastErrorAt = &completedAt
	m.lastErrorKind = observation.ErrorKind
}

func (m *HistoryIngestMetrics) Snapshot() HistoryIngestMetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	average := time.Duration(0)
	completed := m.succeededTotal + m.errorsTotal
	if completed > 0 {
		average = m.durationTotal / time.Duration(completed)
	}

	return HistoryIngestMetricsSnapshot{
		RequestsTotal:           m.requestsTotal,
		InFlight:                m.inFlight,
		SucceededTotal:          m.succeededTotal,
		DuplicatesTotal:         m.duplicatesTotal,
		ErrorsTotal:             m.errorsTotal,
		ReceivedEntriesTotal:    m.receivedEntriesTotal,
		ReceivedBucketsTotal:    m.receivedBucketsTotal,
		AcceptedEntriesTotal:    m.acceptedEntriesTotal,
		AcceptedBucketsTotal:    m.acceptedBucketsTotal,
		StoredBucketsTotal:      m.storedBucketsTotal,
		RejectedEntriesTotal:    m.rejectedEntriesTotal,
		RejectedBucketsTotal:    m.rejectedBucketsTotal,
		DuplicateEntriesTotal:   m.duplicateEntriesTotal,
		DuplicateBucketsTotal:   m.duplicateBucketsTotal,
		HistoryRowsTouchedTotal: m.historyRowsTouchedTotal,
		DurationTotal:           m.durationTotal,
		AverageDuration:         average,
		LastDuration:            m.lastDuration,
		MaxDuration:             m.maxDuration,
		LastRequestAt:           cloneTime(m.lastRequestAt),
		LastSuccessAt:           cloneTime(m.lastSuccessAt),
		LastErrorAt:             cloneTime(m.lastErrorAt),
		LastErrorKind:           m.lastErrorKind,
	}
}
