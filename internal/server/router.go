package server

import (
	"net/http"

	"github.com/nachodev-ui/albion-market-api/internal/accounts"
	"github.com/nachodev-ui/albion-market-api/internal/billing"
	"github.com/nachodev-ui/albion-market-api/internal/blackmarket"
	"github.com/nachodev-ui/albion-market-api/internal/handlers"
	"github.com/nachodev-ui/albion-market-api/internal/playerprofile"
	"github.com/nachodev-ui/albion-market-api/internal/saveddata"
)

type AccountAuthenticator interface {
	Require(http.Handler) http.Handler
	RequireScope(string, http.Handler) http.Handler
}

type AccountRoutes struct {
	Handler              *accounts.Handler
	Service              *accounts.Service
	BillingHandler       *billing.Handler
	PlayerProfileHandler *playerprofile.Handler
	BlackMarketHandler   *blackmarket.Handler
	SavedDataHandler     *saveddata.Handler
	Authenticator        AccountAuthenticator
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

	if len(accountRoutes) > 0 && accountRoutes[0].PlayerProfileHandler != nil {
		profileHandler := handlers.NewAuthenticatedPlayerProfileHandler(
			accountRoutes[0].PlayerProfileHandler,
			accountRoutes[0].Authenticator,
		)
		mux.HandleFunc("/api/v1/albion/players/search", profileHandler.Search)
	}

	if len(accountRoutes) > 0 {
		routes := accountRoutes[0]
		if routes.Handler != nil && routes.Authenticator != nil {
			accountHandler := handlers.NewAuthenticatedAccountHandler(routes.Handler, routes.Authenticator)
			mux.HandleFunc("/api/v1/me", accountHandler.Me)
			mux.HandleFunc("/api/v1/me/entitlements", accountHandler.Entitlements)

			if adminPanelHandler := routes.Handler.AdminHandler(); adminPanelHandler != nil {
				adminHandler := handlers.NewAuthenticatedAdminHandler(adminPanelHandler, routes.Authenticator)
				mux.HandleFunc("/api/v1/admin/session", adminHandler.Session)
				mux.HandleFunc("/api/v1/admin/users", adminHandler.Users)
				mux.HandleFunc("/api/v1/admin/users/{userId}", adminHandler.UserDetail)
				mux.HandleFunc("/api/v1/admin/users/{userId}/grant-pro", adminHandler.GrantPro)
				mux.HandleFunc("/api/v1/admin/users/{userId}/revoke-pro", adminHandler.RevokePro)
				mux.HandleFunc("/api/v1/admin/audit-events", adminHandler.AuditEvents)
			}
		}
		if routes.PlayerProfileHandler != nil && routes.Authenticator != nil {
			profileHandler := handlers.NewAuthenticatedPlayerProfileHandler(routes.PlayerProfileHandler, routes.Authenticator)
			mux.HandleFunc("/api/v1/me/albion-profile", profileHandler.Current)
			mux.HandleFunc("/api/v1/me/albion-profile/link", profileHandler.Link)
			mux.HandleFunc("/api/v1/me/albion-profile/unlink", profileHandler.Unlink)
			mux.HandleFunc("/api/v1/me/albion-profile/refresh", profileHandler.Refresh)
		}
		if routes.SavedDataHandler != nil && routes.Authenticator != nil {
			protectSavedData := func(handler http.Handler) http.Handler {
				return routes.Authenticator.RequireScope("read:account", handler)
			}
			mux.Handle(
				"/api/v1/me/presets",
				protectSavedData(http.HandlerFunc(routes.SavedDataHandler.Presets)),
			)
			mux.Handle(
				"/api/v1/me/presets/{presetId}",
				protectSavedData(http.HandlerFunc(routes.SavedDataHandler.Preset)),
			)
			mux.Handle(
				"/api/v1/me/calculations",
				protectSavedData(http.HandlerFunc(routes.SavedDataHandler.Calculations)),
			)
			mux.Handle(
				"/api/v1/me/calculations/{calculationId}",
				protectSavedData(http.HandlerFunc(routes.SavedDataHandler.Calculation)),
			)
		}
		if routes.BillingHandler != nil {
			billingHandler := handlers.NewAuthenticatedBillingHandler(routes.BillingHandler, routes.Authenticator)
			mux.HandleFunc("/api/v1/billing/checkout", billingHandler.Checkout)
			mux.HandleFunc("/api/v1/billing/portal", billingHandler.Portal)
			mux.HandleFunc("/api/v1/webhooks/lemonsqueezy", billingHandler.Webhook)
		}
		if routes.BlackMarketHandler != nil && routes.Service != nil && routes.Authenticator != nil {
			protectBlackMarket := func(handler http.Handler) http.Handler {
				return routes.Authenticator.RequireScope(
					"read:account",
					accounts.RequireEntitlement(
						routes.Service,
						blackmarket.EntitlementKey,
						accounts.BoolEnabled,
						handler,
					),
				)
			}
			mux.Handle(
				"/api/v1/black-market/analysis",
				protectBlackMarket(http.HandlerFunc(routes.BlackMarketHandler.Analyze)),
			)
			mux.Handle(
				"/api/v1/black-market/opportunities",
				protectBlackMarket(http.HandlerFunc(routes.BlackMarketHandler.Opportunities)),
			)
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
