package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nachodev-ui/albion-market-api/internal/config"
	"github.com/nachodev-ui/albion-market-api/internal/handlers"
	"github.com/nachodev-ui/albion-market-api/internal/repository"
	"github.com/nachodev-ui/albion-market-api/internal/server"
	"github.com/nachodev-ui/albion-market-api/internal/service"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dbpool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("create pgx pool: %v", err)
	}
	defer dbpool.Close()

	if err := dbpool.Ping(ctx); err != nil {
		log.Fatalf("ping database: %v", err)
	}

	repo := repository.NewMarketRepository(dbpool)
	svc := service.NewMarketService(repo)

	healthHandler := handlers.NewHealthHandler(svc)
	ingestHandler := handlers.NewIngestHandler(
		svc,
		[]string{cfg.IngestBearerToken, cfg.IngestPreviousBearerToken},
		cfg.MaxIngestBodyBytes,
	)
	pricesHandler := handlers.NewPricesHandler(svc)

	router := server.NewRouter(healthHandler, ingestHandler, pricesHandler)

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	go func() {
		log.Printf("api listening on %s", cfg.HTTPAddr)
		log.Printf("ingest bearer required: true")
		if cfg.IngestPreviousBearerToken != "" {
			log.Printf("ingest previous bearer accepted: true")
		}
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen and serve: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutdown signal received")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("server shutdown: %v", err)
	}

	log.Println("server stopped")
}
