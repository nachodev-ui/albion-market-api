package server

import "net/http"

func withIngestPhaseTimingRoutes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/ingest/prices":
			withIngestPhaseTiming("prices", next).ServeHTTP(w, r)
		case "/api/v1/ingest/history":
			withIngestPhaseTiming("history", next).ServeHTTP(w, r)
		default:
			next.ServeHTTP(w, r)
		}
	})
}
