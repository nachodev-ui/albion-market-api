package server

import (
	"net/http"

	"github.com/nachodev-ui/albion-market-api/internal/accounts"
	"github.com/nachodev-ui/albion-market-api/internal/handlers"
)

type AccountAuthenticator interface {
	Require(http.Handler) http.Handler
	RequireScope(string, http.Handler) http.Handler
}

type AccountRoutes struct {
	Handler       *accounts.Handler
	Authenticator AccountAuthenticator
}

func NewRouter(
	healthHandler *handlers.HealthHandler,
	ingestHandler *handlers.IngestHandler,
	pricesHandler *handlers.PricesHandler,
	historyHandler *handlers.HistoryHandler,
	statusHandler *handlers.StatusHandler,
	metricsHandler *handlers.MetricsHandler,
	security SecurityOptions,
	observability ObservabilityOptions,
	accountRoutes ...AccountRoutes,
) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", healthHandler.Healthz)
	mux.HandleFunc("/readyz", healthHandler.Readyz)
	mux.HandleFunc("/metrics", metricsHandler.Metrics)
	mux.HandleFunc("/api/v1/ingest/prices", ingestHandler.IngestPrices)
	mux.HandleFunc("/api/v1/ingest/history", ingestHandler.IngestHistory)
	mux.HandleFunc("/api/v1/markets", pricesHandler.ListMarkets)
	mux.HandleFunc("/api/v1/prices", pricesHandler.GetCurrentPrices)
	mux.HandleFunc("/api/v1/prices/query", pricesHandler.QueryCurrentPrices)
	mux.HandleFunc("/api/v1/history", historyHandler.GetMarketHistory)
	mux.HandleFunc("/api/v1/history/query", historyHandler.QueryMarketHistory)
	mux.HandleFunc("/api/v1/status", statusHandler.Status)

	if len(accountRoutes) > 0 {
		routes := accountRoutes[0]
		if routes.Handler != nil && routes.Authenticator != nil {
			accountHandler := handlers.NewAuthenticatedAccountHandler(routes.Handler, routes.Authenticator)
			mux.HandleFunc("/api/v1/me", accountHandler.Me)
			mux.HandleFunc("/api/v1/me/entitlements", accountHandler.Entitlements)
		}
	}

	var handler http.Handler = mux
	if security.RateLimit.Enabled {
		handler = withRateLimit(handler, newIPRateLimiter(security.RateLimit))
	}
	handler = withCORS(handler, security.AllowedOrigins)
	handler = withSecurityHeaders(handler)
	handler = withRequestObservability(handler, observability)
	return handler
}
