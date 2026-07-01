package server

import (
	"net/http"

	"github.com/nachodev-ui/albion-market-api/internal/handlers"
)

func NewRouter(
	healthHandler *handlers.HealthHandler,
	ingestHandler *handlers.IngestHandler,
	pricesHandler *handlers.PricesHandler,
	statusHandler *handlers.StatusHandler,
	security SecurityOptions,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", healthHandler.Healthz)
	mux.HandleFunc("/api/v1/ingest/prices", ingestHandler.IngestPrices)
	mux.HandleFunc("/api/v1/prices/query", pricesHandler.QueryCurrentPrices)
	mux.HandleFunc("/api/v1/status", statusHandler.Status)

	var handler http.Handler = mux
	if security.RateLimit.Enabled {
		handler = withRateLimit(handler, newIPRateLimiter(security.RateLimit))
	}
	handler = withCORS(handler, security.AllowedOrigins)
	handler = withSecurityHeaders(handler)
	return handler
}
