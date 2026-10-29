package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
)

type IdempotencyRepo struct{}

func NewIdempotencyRepo() *IdempotencyRepo {
	return &IdempotencyRepo{}
}

// LockOrGet attempts to atomically claim or retrieve an idempotency key within a transaction.
// If the key does not exist or has expired, a new 'started' record is created.
// If the key already exists and is active, the row is locked with FOR UPDATE and returned.
func (r *IdempotencyRepo) LockOrGet(ctx context.Context, tx repository.DBTX, rec *domain.IdempotencyRecord) (*domain.IdempotencyRecord, error) {
	selectQuery := `
		SELECT merchant_id, key, request_hash, status, resource_id, response_code, response_body, created_at, expires_at
		FROM idempotency_keys
		WHERE merchant_id = $1 AND key = $2
		FOR UPDATE
	`

	existing := &domain.IdempotencyRecord{}
	err := tx.QueryRow(ctx, selectQuery, rec.MerchantID, rec.Key).Scan(
		&existing.MerchantID, &existing.Key, &existing.RequestHash, &existing.Status,
		&existing.ResourceID, &existing.ResponseCode, &existing.ResponseBody,
		&existing.CreatedAt, &existing.ExpiresAt,
	)

	if err == nil {
		// Found existing active or expired record
		if time.Now().After(existing.ExpiresAt) {
			// Expired: reset to started with new request hash
			updateQuery := `
				UPDATE idempotency_keys
				SET request_hash = $1, status = 'started', resource_id = NULL,
				    response_code = NULL, response_body = NULL, created_at = NOW(), expires_at = $2
				WHERE merchant_id = $3 AND key = $4
				RETURNING merchant_id, key, request_hash, status, created_at, expires_at
			`
			err = tx.QueryRow(ctx, updateQuery, rec.RequestHash, rec.ExpiresAt, rec.MerchantID, rec.Key).Scan(
				&rec.MerchantID, &rec.Key, &rec.RequestHash, &rec.Status, &rec.CreatedAt, &rec.ExpiresAt,
			)
			if err != nil {
				return nil, fmt.Errorf("failed to reset expired idempotency record: %w", err)
			}
			return rec, nil
		}
		return existing, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("failed querying idempotency record: %w", err)
	}

	// Not found, insert new record
	insertQuery := `
		INSERT INTO idempotency_keys (
			merchant_id, key, request_hash, status, created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at
	`
	rec.Status = domain.IdempotencyStatusStarted
	err = tx.QueryRow(
		ctx, insertQuery,
		rec.MerchantID, rec.Key, rec.RequestHash, rec.Status, rec.CreatedAt, rec.ExpiresAt,
	).Scan(&rec.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("failed to insert new idempotency record: %w", err)
	}

	return rec, nil
}

func (r *IdempotencyRepo) SaveResponse(ctx context.Context, tx repository.DBTX, rec *domain.IdempotencyRecord) error {
	query := `
		UPDATE idempotency_keys
		SET
			status = 'completed',
			resource_id = $1,
			response_code = $2,
			response_body = $3
		WHERE merchant_id = $4 AND key = $5
	`
	_, err := tx.Exec(
		ctx, query,
		rec.ResourceID, rec.ResponseCode, rec.ResponseBody,
		rec.MerchantID, rec.Key,
	)
	if err != nil {
		return fmt.Errorf("failed to save idempotency response: %w", err)
	}
	return nil
}
