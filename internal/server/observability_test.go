package server

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/nachodev-ui/albion-market-api/internal/observability"
)

func TestRequestObservabilityRecordsHTTPMetrics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		path       string
		handler    http.Handler
		wantRoute  string
		wantStatus int
	}{
		{
			name: "health endpoint",
			path: "/healthz",
			handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
			wantRoute:  "/healthz",
			wantStatus: http.StatusOK,
		},
		{
			name:       "unknown route",
			path:       "/does-not-exist",
			handler:    http.NotFoundHandler(),
			wantRoute:  "unmatched",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			httpMetrics := observability.NewHTTPMetrics()
			handler := withRequestObservability(
				test.handler,
				ObservabilityOptions{HTTPMetrics: httpMetrics},
			)

			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.wantStatus {
				t.Fatalf("status code = %d, want %d", response.Code, test.wantStatus)
			}

			snapshot := httpMetrics.Snapshot()
			if snapshot.InFlight != 0 {
				t.Fatalf("InFlight = %d, want 0", snapshot.InFlight)
			}

			requestCount := uint64(0)
			for key, count := range snapshot.Requests {
				if key.Method == http.MethodGet && key.Route == test.wantRoute && key.Status == strconv.Itoa(test.wantStatus) {
					requestCount += count
				}
			}
			if requestCount != 1 {
				t.Fatalf("GET %s status %d request count = %d, want 1; snapshot=%#v", test.wantRoute, test.wantStatus, requestCount, snapshot.Requests)
			}

			durationCount := uint64(0)
			for key, histogram := range snapshot.Durations {
				if key.Method == http.MethodGet && key.Route == test.wantRoute {
					durationCount += histogram.Count
				}
			}
			if durationCount != 1 {
				t.Fatalf("GET %s duration Count = %d, want 1; snapshot=%#v", test.wantRoute, durationCount, snapshot.Durations)
			}
		})
	}
}
