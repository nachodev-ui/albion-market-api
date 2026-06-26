package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/config"
	"github.com/nachodev-ui/albion-market-api/internal/handlers"
	"github.com/nachodev-ui/albion-market-api/internal/observability"
	"github.com/nachodev-ui/albion-market-api/internal/repository"
	"github.com/nachodev-ui/albion-market-api/internal/server"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

const serviceName = "albion-market-api"

func main() {
	bootstrapLogger := observability.NewLogger(os.Stdout, "auto")
	cfg, err := config.Load()
	if err != nil {
		bootstrapLogger.Error("config.load_failed", observability.F("error", err))
		return
	}
	logger := observability.NewLogger(os.Stdout, cfg.LogColor)
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

	repo := repository.NewMarketRepository(dbpool)
	svc := service.NewMarketService(repo)
	ingestMetrics := observability.NewIngestMetrics()
	databaseMonitor := observability.NewPgxDatabaseMonitor(dbpool)

	healthHandler := handlers.NewHealthHandler(svc)
	ingestHandler := handlers.NewIngestHandler(
		svc,
		[]string{cfg.IngestBearerToken, cfg.IngestPreviousBearerToken},
		cfg.MaxIngestBodyBytes,
		ingestMetrics,
		logger,
	)
	pricesHandler := handlers.NewPricesHandler(svc)
	statusHandler := handlers.NewStatusHandler(
		serviceName,
		cfg.AppEnv,
		startedAt,
		databaseMonitor,
		ingestMetrics,
	)

	router := server.NewRouter(healthHandler, ingestHandler, pricesHandler, statusHandler)

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info(
			"api.started",
			observability.F("address", cfg.HTTPAddr),
			observability.F("environment", cfg.AppEnv),
			observability.F("health", "/healthz"),
			observability.F("status", "/api/v1/status"),
			observability.F("color", cfg.LogColor),
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
