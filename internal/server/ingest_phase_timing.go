package server

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

type bufferedResponseWriter struct {
	header     http.Header
	body       bytes.Buffer
	statusCode int
}

func newBufferedResponseWriter(header http.Header) *bufferedResponseWriter {
	return &bufferedResponseWriter{header: header.Clone()}
}

func (w *bufferedResponseWriter) Header() http.Header {
	return w.header
}

func (w *bufferedResponseWriter) WriteHeader(statusCode int) {
	if w.statusCode == 0 {
		w.statusCode = statusCode
	}
}

func (w *bufferedResponseWriter) Write(data []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.body.Write(data)
}

func (w *bufferedResponseWriter) flushTo(target http.ResponseWriter) {
	destination := target.Header()
	for key := range destination {
		destination.Del(key)
	}
	for key, values := range w.header {
		for _, value := range values {
			destination.Add(key, value)
		}
	}
	statusCode := w.statusCode
	if statusCode == 0 {
		statusCode = http.StatusOK
	}
	target.WriteHeader(statusCode)
	_, _ = target.Write(w.body.Bytes())
}

func withIngestPhaseTiming(stream string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		ctx := observability.WithIngestPhaseTiming(r.Context(), stream)
		buffered := newBufferedResponseWriter(w.Header())

		next.ServeHTTP(buffered, r.WithContext(ctx))

		setIngestServerTiming(buffered.Header(), startedAt, observability.IngestPhaseTiming(ctx))
		buffered.flushTo(w)
	})
}

func setIngestServerTiming(header http.Header, startedAt time.Time, timing observability.IngestPhaseSnapshot) {
	values := []string{
		fmt.Sprintf("api;dur=%.3f", durationInMilliseconds(time.Since(startedAt))),
	}
	if timing.Transaction > 0 {
		values = append(values, fmt.Sprintf(
			"postgres-tx;dur=%.3f",
			durationInMilliseconds(timing.Transaction),
		))
	}
	if timing.Commit > 0 {
		values = append(values, fmt.Sprintf(
			"postgres-commit;dur=%.3f",
			durationInMilliseconds(timing.Commit),
		))
	}
	header.Set("Server-Timing", strings.Join(values, ", "))
}

func durationInMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}
