package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/payflow/payflow/internal/api"
	"github.com/payflow/payflow/internal/config"
	"github.com/payflow/payflow/internal/database"
	"github.com/payflow/payflow/internal/metrics"
	"github.com/payflow/payflow/internal/repository/postgres"
	"github.com/payflow/payflow/internal/service"
)

func main() {
	// 1. Structured JSON Logger
	logLevel := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "DEBUG" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// 2. Load Configuration
	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration loading failed", "error", err)
		os.Exit(1)
	}

	logger.Info("starting payflow api server", "port", cfg.Port, "environment", cfg.Environment)

	// 3. Database Connection Pool
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
	logger.Info("connected to postgresql database successfully")

	// 4. Apply Database Migrations
	if err := database.MigrateUp(ctx, pool, logger); err != nil {
		logger.Error("database migration failed", "error", err)
		os.Exit(1)
	}

	// 5. Initialize Observability & Repositories
	appMetrics := metrics.New()
	txManager := postgres.NewTxManager(pool)

	merchantRepo := postgres.NewMerchantRepo()
	paymentRepo := postgres.NewPaymentRepo()
	refundRepo := postgres.NewRefundRepo()
	ledgerRepo := postgres.NewLedgerRepo()
	idempotencyRepo := postgres.NewIdempotencyRepo()
	outboxRepo := postgres.NewOutboxRepo()
	reconRepo := postgres.NewReconciliationRepo()

	// 6. Initialize Services
	idempotencyService := service.NewIdempotencyService(idempotencyRepo, cfg.IdempotencyExpiryHours)
	merchantService := service.NewMerchantService(txManager, merchantRepo)
	ledgerService := service.NewLedgerService(ledgerRepo)
	paymentService := service.NewPaymentService(
		txManager, paymentRepo, merchantRepo, ledgerService,
		outboxRepo, idempotencyService, cfg.PlatformFeeBps, cfg.PlatformFeeFixedMinor,
	)
	refundService := service.NewRefundService(
		txManager, paymentRepo, refundRepo, ledgerService,
		outboxRepo, idempotencyService,
	)
	reconService := service.NewReconciliationService(txManager, reconRepo, logger)

	// 7. Background Outbox Worker (if enabled)
	var outboxWorker *service.OutboxWorker
	if cfg.WorkerEnabled {
		outboxWorker = service.NewOutboxWorker(
			pool, txManager, outboxRepo, "api-embedded-worker",
			cfg.WorkerPollInterval, cfg.WorkerBatchSize, logger,
		)
		outboxWorker.Start(ctx)
	}

	// 8. Construct HTTP Router
	handlers := api.Handlers{
		Merchant:       api.NewMerchantHandler(merchantService),
		Payment:        api.NewPaymentHandler(paymentService),
		Refund:         api.NewRefundHandler(refundService),
		Ledger:         api.NewLedgerHandler(pool, ledgerService),
		Reconciliation: api.NewReconciliationHandler(reconService),
		Health:         api.NewHealthHandler(pool),
	}

	router := api.NewRouter(handlers, merchantService, pool, appMetrics, logger)

	server := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// 9. Server Lifecycle & Graceful Shutdown
	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErrors <- fmt.Errorf("http server failure: %w", err)
		}
	}()

	shutdownSig := make(chan os.Signal, 1)
	signal.Notify(shutdownSig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		logger.Error("server encountered fatal error", "error", err)
	case sig := <-shutdownSig:
		logger.Info("initiating graceful shutdown", "signal", sig.String())

		// Stop background worker first
		if outboxWorker != nil {
			outboxWorker.Stop()
		}

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", "error", err)
			_ = server.Close()
		}
		logger.Info("server shutdown complete")
	}
}
