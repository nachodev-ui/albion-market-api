package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/accounts"
	"github.com/nachodev-ui/albion-market-api/internal/authn"
	"github.com/nachodev-ui/albion-market-api/internal/config"
	"github.com/nachodev-ui/albion-market-api/internal/handlers"
	"github.com/nachodev-ui/albion-market-api/internal/ingestauth"
	"github.com/nachodev-ui/albion-market-api/internal/observability"
	"github.com/nachodev-ui/albion-market-api/internal/repository"
	"github.com/nachodev-ui/albion-market-api/internal/server"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

const serviceName = "albion-market-api"

var (
	version  = "dev"
	revision = "unknown"
	created  = "unknown"
)

func main() {
	bootstrapLogger := observability.NewLogger(os.Stdout, "auto")
	cfg, err := config.Load()
	if err != nil {
		bootstrapLogger.Error("config.load_failed", observability.F("error", err))
		return
	}
	authCfg, err := config.LoadAccountAuth(cfg.AppEnv)
	if err != nil {
		bootstrapLogger.Error("auth.config_load_failed", observability.F("error", err))
		return
	}
	logger := observability.NewLoggerWithFormat(os.Stdout, cfg.LogColor, cfg.LogFormat)
	startedAt := time.Now().UTC()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dbpool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database.pool_create_failed", observability.F("error", err))
		return
	}
	defer dbpool.Close()

	pingStarted := time.Now()
	if err := dbpool.Ping(ctx); err != nil {
		logger.Error(
			"database.ping_failed",
			observability.F("duration_ms", durationMilliseconds(time.Since(pingStarted))),
			observability.F("error", err),
		)
		return
	}
	logger.Success(
		"database.connected",
		observability.F("duration_ms", durationMilliseconds(time.Since(pingStarted))),
		observability.F("max_connections", dbpool.Stat().MaxConns()),
	)

	databaseMetrics := observability.NewDatabaseMetrics()
	repo := repository.NewMarketRepository(dbpool, databaseMetrics)
	svc := service.NewMarketService(repo)
	accountService := accounts.NewService(dbpool)
	accountHandler := accounts.NewHandler(accountService)
	accountAuthenticator, err := authn.New(authCfg)
	if err != nil {
		logger.Error("auth.configure_failed", observability.F("error", err))
		return
	}

	ingestMetrics := observability.NewIngestMetrics()
	historyIngestMetrics := observability.NewHistoryIngestMetrics()
	httpMetrics := observability.NewHTTPMetrics()
	databaseMonitor := observability.NewPgxDatabaseMonitor(dbpool)
	readinessMetrics := observability.NewReadinessMetrics()
	readinessChecker := observability.NewPgxReadinessChecker(
		dbpool,
		databaseMetrics,
		readinessMetrics,
	)

	ingestAuthenticator, err := ingestauth.New(cfg.IngestCredentials, ingestauth.Options{
		RequireHTTPS:      cfg.IngestRequireHTTPS,
		TrustProxyHeaders: cfg.TrustProxyHeaders,
	})
	for index := range cfg.IngestCredentials {
		cfg.IngestCredentials[index].Token = ""
	}
	cfg.IngestCredentials = nil
	if err != nil {
		logger.Error("ingest_auth.configure_failed", observability.F("error", err))
		return
	}

	healthHandler := handlers.NewHealthHandler(readinessChecker)
	metricsHandler := handlers.NewMetricsHandler(observability.NewPrometheusExporter(
		observability.PrometheusExporterOptions{
			Service:          serviceName,
			Environment:      cfg.AppEnv,
			Version:          version,
			Revision:         revision,
			StartedAt:        startedAt,
			HTTP:             httpMetrics,
			Database:         databaseMetrics,
			DatabasePool:     databaseMonitor,
			Ingest:           ingestMetrics,
			HistoryIngest:    historyIngestMetrics,
			Readiness:        readinessMetrics,
			ReadinessChecker: readinessChecker,
		},
	))
	legacyIngestHandler := handlers.NewIngestHandler(
		svc,
		ingestAuthenticator,
		cfg.MaxIngestBodyBytes,
		ingestMetrics,
		logger,
		historyIngestMetrics,
	)
	pricesHandler := handlers.NewPricesHandler(svc, cfg.MaxPublicBodyBytes)
	historyHandler := handlers.NewHistoryHandler(svc, cfg.MaxPublicBodyBytes)
	statusHandler := handlers.NewStatusHandler(
		serviceName,
		cfg.AppEnv,
		startedAt,
		databaseMonitor,
		ingestMetrics,
		historyIngestMetrics,
	)

	router := server.NewRouter(
		healthHandler,
		legacyIngestHandler,
		pricesHandler,
		historyHandler,
		statusHandler,
		metricsHandler,
		server.SecurityOptions{
			AllowedOrigins: cfg.CORSAllowedOrigins,
			RateLimit: server.RateLimitOptions{
				Enabled:           cfg.RateLimitEnabled,
				RequestsPerSecond: cfg.RateLimitRequestsPerSec,
				Burst:             cfg.RateLimitBurst,
				ClientTTL:         cfg.RateLimitClientTTL,
				TrustProxyHeaders: cfg.TrustProxyHeaders,
			},
		},
		server.ObservabilityOptions{
			HTTPMetrics: httpMetrics,
			Logger:      logger,
		},
		server.AccountRoutes{
			Handler:       accountHandler,
			Authenticator: accountAuthenticator,
		},
	)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: cfg.ReadHeaderTimeout,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    cfg.MaxHeaderBytes,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info(
			"api.started",
			observability.F("address", cfg.HTTPAddr),
			observability.F("environment", cfg.AppEnv),
			observability.F("health", "/healthz"),
			observability.F("readiness", "/readyz"),
			observability.F("metrics", "/metrics"),
			observability.F("status", "/api/v1/status"),
			observability.F("markets", "/api/v1/markets"),
			observability.F("prices", "/api/v1/prices"),
			observability.F("prices_query", "/api/v1/prices/query"),
			observability.F("history", "/api/v1/history"),
			observability.F("history_query", "/api/v1/history/query"),
			observability.F("account_me", "/api/v1/me"),
			observability.F("account_entitlements", "/api/v1/me/entitlements"),
			observability.F("auth_enabled", authCfg.Enabled),
			observability.F("history_ingest", "/api/v1/ingest/history"),
			observability.F("ingest_credential_ids", credentialIDs(cfg.IngestCredentialSources)),
			observability.F("ingest_require_https", cfg.IngestRequireHTTPS),
			observability.F("cors_origins", len(cfg.CORSAllowedOrigins)),
			observability.F("rate_limit_enabled", cfg.RateLimitEnabled),
			observability.F("trust_proxy_headers", cfg.TrustProxyHeaders),
			observability.F("color", cfg.LogColor),
			observability.F("log_format", cfg.LogFormat),
			observability.F("version", version),
			observability.F("revision", revision),
			observability.F("created", created),
		)
		serverErrors <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		logger.Info("api.shutdown_requested", observability.F("reason", ctx.Err()))
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			logger.Error("api.serve_failed", observability.F("error", err))
			return
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("api.shutdown_failed", observability.F("error", err))
		return
	}

	logger.Success("api.stopped", observability.F("uptime", time.Since(startedAt).Round(time.Millisecond)))
}

func durationMilliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}

func credentialIDs(sources []config.CredentialSource) string {
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.ID+":"+source.Source)
	}
	return strings.Join(ids, ",")
}
