package server

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

const requestIDHeader = "X-Request-ID"

var fallbackRequestIDCounter atomic.Uint64

type ObservabilityOptions struct {
	HTTPMetrics *observability.HTTPMetrics
	Logger      *observability.Logger
}

type responseRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *responseRecorder) WriteHeader(status int) {
	if r.status != 0 {
		return
	}
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *responseRecorder) Write(payload []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	written, err := r.ResponseWriter.Write(payload)
	r.bytes += written
	return written, err
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func withRequestObservability(next http.Handler, options ObservabilityOptions) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		correlationID := acceptedRequestID(r.Header.Get(requestIDHeader))
		if correlationID == "" {
			correlationID = newRequestID()
		}
		w.Header().Set(requestIDHeader, correlationID)
		r = r.WithContext(observability.WithCorrelationID(r.Context(), correlationID))

		if options.HTTPMetrics != nil {
			options.HTTPMetrics.RequestStarted()
		}
		recorder := &responseRecorder{ResponseWriter: w}
		route := metricRoute(r.URL.Path)

		defer func() {
			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			duration := time.Since(started)
			if recovered := recover(); recovered != nil {
				status = http.StatusInternalServerError
				if options.HTTPMetrics != nil {
					options.HTTPMetrics.RequestFinished(r.Method, route, status, duration)
				}
				if options.Logger != nil {
					options.Logger.Error(
						"http.request_panicked",
						observability.F("correlation_id", correlationID),
						observability.F("method", r.Method),
						observability.F("route", route),
						observability.F("status", status),
						observability.F("duration_ms", durationMilliseconds(duration)),
						observability.F("response_bytes", recorder.bytes),
						observability.F("panic_type", fmt.Sprintf("%T", recovered)),
					)
				}
				panic(recovered)
			}

			if options.HTTPMetrics != nil {
				options.HTTPMetrics.RequestFinished(r.Method, route, status, duration)
			}
			if options.Logger == nil {
				return
			}
			fields := []observability.Field{
				observability.F("correlation_id", correlationID),
				observability.F("method", r.Method),
				observability.F("route", route),
				observability.F("status", status),
				observability.F("duration_ms", durationMilliseconds(duration)),
				observability.F("response_bytes", recorder.bytes),
			}
			switch {
			case status >= 500:
				options.Logger.Error("http.request_completed", fields...)
			case status >= 400:
				options.Logger.Warn("http.request_completed", fields...)
			default:
				options.Logger.Info("http.request_completed", fields...)
			}
		}()

		next.ServeHTTP(recorder, r)
	})
}

func metricRoute(path string) string {
	switch path {
	case "/healthz",
		"/readyz",
		"/metrics",
		"/api/v1/status",
		"/api/v1/ingest/prices",
		"/api/v1/ingest/history",
		"/api/v1/markets",
		"/api/v1/prices",
		"/api/v1/prices/query",
		"/api/v1/history",
		"/api/v1/history/query":
		return path
	default:
		return "unmatched"
	}
}

func acceptedRequestID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 1 || len(value) > 128 {
		return ""
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') ||
			(character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '-' || character == '_' || character == '.' || character == ':' {
			continue
		}
		return ""
	}
	return value
}

func newRequestID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err == nil {
		return hex.EncodeToString(buffer)
	}
	return fmt.Sprintf("fallback-%d-%d", time.Now().UTC().UnixNano(), fallbackRequestIDCounter.Add(1))
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}
