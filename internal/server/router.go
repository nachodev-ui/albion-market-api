package server

import (
	"net/http"

	"github.com/nachodev-ui/albion-market-api/internal/handlers"
)

func NewRouter(
	healthHandler *handlers.HealthHandler,
	ingestHandler *handlers.IngestHandler,
	pricesHandler *handlers.PricesHandler,
	historyHandler *handlers.HistoryHandler,
	statusHandler *handlers.StatusHandler,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", healthHandler.Healthz)
	mux.HandleFunc("/api/v1/ingest/prices", ingestHandler.IngestPrices)
	mux.HandleFunc("/api/v1/ingest/history", ingestHandler.IngestHistory)
	mux.HandleFunc("/api/v1/markets", pricesHandler.ListMarkets)
	mux.HandleFunc("/api/v1/prices", pricesHandler.GetCurrentPrices)
	mux.HandleFunc("/api/v1/prices/query", pricesHandler.QueryCurrentPrices)
	mux.HandleFunc("/api/v1/history", historyHandler.GetMarketHistory)
	mux.HandleFunc("/api/v1/history/query", historyHandler.QueryMarketHistory)
	mux.HandleFunc("/api/v1/status", statusHandler.Status)

	return withCORS(mux)
}

func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
