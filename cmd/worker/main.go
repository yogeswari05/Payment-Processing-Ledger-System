package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/payflow/payflow/internal/config"
	"github.com/payflow/payflow/internal/database"
	"github.com/payflow/payflow/internal/repository/postgres"
	"github.com/payflow/payflow/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration loading failed", "error", err)
		os.Exit(1)
	}

	logger.Info("starting standalone payflow outbox worker")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := database.NewPool(ctx, database.Config{
		URL:             cfg.DatabaseURL,
		MaxConns:        cfg.DatabaseMaxConns,
		MinConns:        cfg.DatabaseMinConns,
		MaxConnLifetime: cfg.DatabaseMaxConnLife,
		MaxConnIdleTime: cfg.DatabaseMaxConnIdle,
	})
	if err != nil {
		logger.Error("failed connecting to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	txManager := postgres.NewTxManager(pool)
	outboxRepo := postgres.NewOutboxRepo()

	worker := service.NewOutboxWorker(
		pool, txManager, outboxRepo, "standalone-outbox-worker",
		cfg.WorkerPollInterval, cfg.WorkerBatchSize, logger,
	)

	worker.Start(ctx)

	shutdownSig := make(chan os.Signal, 1)
	signal.Notify(shutdownSig, syscall.SIGINT, syscall.SIGTERM)

	sig := <-shutdownSig
	logger.Info("shutting down worker", "signal", sig.String())
	cancel()
	worker.Stop()
	logger.Info("worker exited cleanly")
}
