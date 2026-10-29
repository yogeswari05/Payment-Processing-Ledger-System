package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
)

type OutboxRepo struct{}

func NewOutboxRepo() *OutboxRepo {
	return &OutboxRepo{}
}

func (r *OutboxRepo) Create(ctx context.Context, tx repository.DBTX, evt *domain.OutboxEvent) error {
	query := `
		INSERT INTO outbox_events (
			id, event_type, aggregate_type, aggregate_id, payload,
			status, attempts, max_attempts, next_retry_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := tx.Exec(
		ctx, query,
		evt.ID, evt.EventType, evt.AggregateType, evt.AggregateID, evt.Payload,
		evt.Status, evt.Attempts, evt.MaxAttempts, evt.NextRetryAt, evt.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert outbox event: %w", err)
	}
	return nil
}

// ClaimBatch uses PostgreSQL row-level locks with SKIP LOCKED to safely claim a batch of events
// without contending or blocking concurrent worker instances.
func (r *OutboxRepo) ClaimBatch(ctx context.Context, pool *pgxpool.Pool, workerID string, batchSize int, lockTimeout time.Duration) ([]domain.OutboxEvent, error) {
	query := `
		UPDATE outbox_events
		SET
			status = 'PROCESSING',
			locked_at = NOW(),
			locked_by = $1,
			attempts = attempts + 1
		WHERE id IN (
			SELECT id FROM outbox_events
			WHERE (
				status = 'PENDING' OR
				(status = 'PROCESSING' AND locked_at < NOW() - ($2 * INTERVAL '1 second'))
			)
			AND next_retry_at <= NOW()
			ORDER BY created_at ASC
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		RETURNING
			id, event_type, aggregate_type, aggregate_id, payload,
			status, attempts, max_attempts, next_retry_at, locked_at, locked_by,
			last_error, created_at, published_at
	`

	rows, err := pool.Query(ctx, query, workerID, int(lockTimeout.Seconds()), batchSize)
	if err != nil {
		return nil, fmt.Errorf("failed claiming outbox events batch: %w", err)
	}
	defer rows.Close()

	var events []domain.OutboxEvent
	for rows.Next() {
		var e domain.OutboxEvent
		if err := rows.Scan(
			&e.ID, &e.EventType, &e.AggregateType, &e.AggregateID, &e.Payload,
			&e.Status, &e.Attempts, &e.MaxAttempts, &e.NextRetryAt, &e.LockedAt, &e.LockedBy,
			&e.LastError, &e.CreatedAt, &e.PublishedAt,
		); err != nil {
			return nil, fmt.Errorf("failed scanning claimed outbox event: %w", err)
		}
		events = append(events, e)
	}

	return events, nil
}

func (r *OutboxRepo) MarkPublished(ctx context.Context, tx repository.DBTX, eventID uuid.UUID) error {
	query := `
		UPDATE outbox_events
		SET
			status = 'PUBLISHED',
			published_at = NOW(),
			locked_at = NULL,
			locked_by = NULL
		WHERE id = $1
	`
	_, err := tx.Exec(ctx, query, eventID)
	if err != nil {
		return fmt.Errorf("failed to mark outbox event published: %w", err)
	}
	return nil
}

func (r *OutboxRepo) RecordFailure(ctx context.Context, tx repository.DBTX, eventID uuid.UUID, errMsg string, nextRetry time.Time, isDeadLetter bool) error {
	newStatus := domain.OutboxStatusPending
	if isDeadLetter {
		newStatus = domain.OutboxStatusDeadLetter
	}

	query := `
		UPDATE outbox_events
		SET
			status = $1,
			last_error = $2,
			next_retry_at = $3,
			locked_at = NULL,
			locked_by = NULL
		WHERE id = $4
	`
	_, err := tx.Exec(ctx, query, newStatus, errMsg, nextRetry, eventID)
	if err != nil {
		return fmt.Errorf("failed recording outbox failure: %w", err)
	}
	return nil
}

// RecordConsumed demonstrates idempotent consumer deduplication.
// Returns true if the event was consumed for the first time, false if duplicate.
func (r *OutboxRepo) RecordConsumed(ctx context.Context, tx repository.DBTX, eventID uuid.UUID, consumerName string) (bool, error) {
	query := `
		INSERT INTO consumed_events (event_id, consumer_name, consumed_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (event_id) DO NOTHING
	`
	tag, err := tx.Exec(ctx, query, eventID, consumerName)
	if err != nil {
		return false, fmt.Errorf("failed recording event consumption: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}
