package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
)

type OutboxWorker struct {
	pool         *pgxpool.Pool
	txManager    repository.TxManager
	outboxRepo   repository.OutboxRepo
	workerID     string
	pollInterval time.Duration
	batchSize    int
	lockTimeout  time.Duration
	logger       *slog.Logger
	wg           sync.WaitGroup
	stopCh       chan struct{}
}

func NewOutboxWorker(
	pool *pgxpool.Pool,
	tm repository.TxManager,
	or repository.OutboxRepo,
	workerID string,
	pollInterval time.Duration,
	batchSize int,
	logger *slog.Logger,
) *OutboxWorker {
	if workerID == "" {
		workerID = "worker-" + uuid.New().String()[:8]
	}
	if pollInterval <= 0 {
		pollInterval = 1 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 20
	}
	return &OutboxWorker{
		pool:         pool,
		txManager:    tm,
		outboxRepo:   or,
		workerID:     workerID,
		pollInterval: pollInterval,
		batchSize:    batchSize,
		lockTimeout:  60 * time.Second,
		logger:       logger.With("component", "outbox_worker", "worker_id", workerID),
		stopCh:       make(chan struct{}),
	}
}

// Start begins the outbox event processing loop in a background goroutine.
func (w *OutboxWorker) Start(ctx context.Context) {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.logger.Info("outbox worker started", "poll_interval", w.pollInterval, "batch_size", w.batchSize)
		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				w.logger.Info("outbox worker stopping due to context cancellation")
				return
			case <-w.stopCh:
				w.logger.Info("outbox worker stopping due to stop signal")
				return
			case <-ticker.C:
				w.processBatch(ctx)
			}
		}
	}()
}

// Stop initiates graceful shutdown and waits for current batch to complete.
func (w *OutboxWorker) Stop() {
	close(w.stopCh)
	w.wg.Wait()
	w.logger.Info("outbox worker stopped cleanly")
}

// ProcessOnce processes a single batch synchronously (useful for tests and manual triggering).
func (w *OutboxWorker) ProcessOnce(ctx context.Context) (int, error) {
	events, err := w.outboxRepo.ClaimBatch(ctx, w.pool, w.workerID, w.batchSize, w.lockTimeout)
	if err != nil {
		return 0, fmt.Errorf("failed claiming batch: %w", err)
	}

	for _, evt := range events {
		if err := w.deliverEvent(ctx, evt); err != nil {
			w.logger.Error("failed delivering outbox event", "event_id", evt.ID, "event_type", evt.EventType, "error", err)
		}
	}

	return len(events), nil
}

func (w *OutboxWorker) processBatch(ctx context.Context) {
	events, err := w.outboxRepo.ClaimBatch(ctx, w.pool, w.workerID, w.batchSize, w.lockTimeout)
	if err != nil {
		w.logger.Error("error claiming outbox batch", "error", err)
		return
	}

	for _, evt := range events {
		select {
		case <-ctx.Done():
			return
		default:
			if err := w.deliverEvent(ctx, evt); err != nil {
				w.logger.Error("failed delivering outbox event", "event_id", evt.ID, "event_type", evt.EventType, "error", err)
			}
		}
	}
}

func (w *OutboxWorker) deliverEvent(ctx context.Context, evt domain.OutboxEvent) error {
	w.logger.Debug("processing outbox event", "event_id", evt.ID, "type", evt.EventType, "attempt", evt.Attempts)

	// Simulate downstream delivery to message broker / webhook endpoint.
	// For educational and testing realism, we enforce consumer deduplication using consumed_events.
	err := w.txManager.WithTx(ctx, func(tx repository.DBTX) error {
		// Idempotent consumer simulation
		_, err := w.outboxRepo.RecordConsumed(ctx, tx, evt.ID, "downstream_consumer_sim")
		if err != nil {
			return fmt.Errorf("consumer recording failed: %w", err)
		}

		// Mark outbox event published
		if err := w.outboxRepo.MarkPublished(ctx, tx, evt.ID); err != nil {
			return fmt.Errorf("failed to mark event published: %w", err)
		}

		return nil
	})

	if err != nil {
		// Handle retry policy and dead lettering
		isDeadLetter := evt.Attempts >= evt.MaxAttempts
		backoffSeconds := math.Pow(2, float64(evt.Attempts))
		if backoffSeconds > 300 {
			backoffSeconds = 300 // Max 5 min backoff
		}
		nextRetry := time.Now().Add(time.Duration(backoffSeconds) * time.Second)

		_ = w.txManager.WithTx(ctx, func(tx repository.DBTX) error {
			return w.outboxRepo.RecordFailure(ctx, tx, evt.ID, err.Error(), nextRetry, isDeadLetter)
		})
		return err
	}

	w.logger.Info("outbox event delivered and published", "event_id", evt.ID, "type", evt.EventType)
	return nil
}
