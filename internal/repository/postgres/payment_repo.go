package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
)

type PaymentRepo struct{}

func NewPaymentRepo() *PaymentRepo {
	return &PaymentRepo{}
}

func (r *PaymentRepo) Create(ctx context.Context, tx repository.DBTX, p *domain.Payment) error {
	query := `
		INSERT INTO payments (
			id, merchant_id, customer_id, amount, currency, status,
			captured_amount, refunded_amount, description, metadata, version,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
		)
	`
	_, err := tx.Exec(
		ctx, query,
		p.ID, p.MerchantID, p.CustomerID, p.Amount, p.Currency, p.Status,
		p.CapturedAmount, p.RefundedAmount, p.Description, p.Metadata, p.Version,
		p.CreatedAt, p.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert payment: %w", err)
	}
	return nil
}

func (r *PaymentRepo) GetByID(ctx context.Context, tx repository.DBTX, merchantID, id uuid.UUID) (*domain.Payment, error) {
	query := `
		SELECT
			id, merchant_id, customer_id, amount, currency, status,
			captured_amount, refunded_amount, description, metadata, version,
			created_at, updated_at
		FROM payments
		WHERE merchant_id = $1 AND id = $2
	`
	p := &domain.Payment{}
	err := tx.QueryRow(ctx, query, merchantID, id).Scan(
		&p.ID, &p.MerchantID, &p.CustomerID, &p.Amount, &p.Currency, &p.Status,
		&p.CapturedAmount, &p.RefundedAmount, &p.Description, &p.Metadata, &p.Version,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPaymentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get payment by id: %w", err)
	}
	return p, nil
}

// GetByIDForUpdate fetches the payment while acquiring a row-level lock (FOR UPDATE),
// preventing race conditions on concurrent captures and refunds.
func (r *PaymentRepo) GetByIDForUpdate(ctx context.Context, tx repository.DBTX, merchantID, id uuid.UUID) (*domain.Payment, error) {
	query := `
		SELECT
			id, merchant_id, customer_id, amount, currency, status,
			captured_amount, refunded_amount, description, metadata, version,
			created_at, updated_at
		FROM payments
		WHERE merchant_id = $1 AND id = $2
		FOR UPDATE
	`
	p := &domain.Payment{}
	err := tx.QueryRow(ctx, query, merchantID, id).Scan(
		&p.ID, &p.MerchantID, &p.CustomerID, &p.Amount, &p.Currency, &p.Status,
		&p.CapturedAmount, &p.RefundedAmount, &p.Description, &p.Metadata, &p.Version,
		&p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrPaymentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to lock payment for update: %w", err)
	}
	return p, nil
}

func (r *PaymentRepo) Update(ctx context.Context, tx repository.DBTX, p *domain.Payment) error {
	query := `
		UPDATE payments
		SET
			status = $1,
			captured_amount = $2,
			refunded_amount = $3,
			version = version + 1,
			updated_at = NOW()
		WHERE id = $4 AND merchant_id = $5 AND version = $6
		RETURNING version, updated_at
	`
	err := tx.QueryRow(
		ctx, query,
		p.Status, p.CapturedAmount, p.RefundedAmount,
		p.ID, p.MerchantID, p.Version,
	).Scan(&p.Version, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("concurrent modification detected for payment %s: optimistic lock failed", p.ID)
	}
	if err != nil {
		return fmt.Errorf("failed to update payment: %w", err)
	}
	return nil
}

func (r *PaymentRepo) List(ctx context.Context, tx repository.DBTX, filter repository.ListPaymentsFilter) ([]domain.Payment, *time.Time, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	query := `
		SELECT
			id, merchant_id, customer_id, amount, currency, status,
			captured_amount, refunded_amount, description, metadata, version,
			created_at, updated_at
		FROM payments
		WHERE merchant_id = $1
	`
	args := []any{filter.MerchantID}
	argIdx := 2

	if filter.CustomerID != nil {
		query += fmt.Sprintf(" AND customer_id = $%d", argIdx)
		args = append(args, *filter.CustomerID)
		argIdx++
	}

	if filter.Status != nil {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, *filter.Status)
		argIdx++
	}

	if filter.Cursor != nil {
		query += fmt.Sprintf(" AND created_at < $%d", argIdx)
		args = append(args, *filter.Cursor)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", argIdx)
	args = append(args, limit+1) // Fetch limit + 1 to detect next page

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list payments: %w", err)
	}
	defer rows.Close()

	var payments []domain.Payment
	for rows.Next() {
		var p domain.Payment
		if err := rows.Scan(
			&p.ID, &p.MerchantID, &p.CustomerID, &p.Amount, &p.Currency, &p.Status,
			&p.CapturedAmount, &p.RefundedAmount, &p.Description, &p.Metadata, &p.Version,
			&p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("failed scanning payment row: %w", err)
		}
		payments = append(payments, p)
	}

	var nextCursor *time.Time
	if len(payments) > limit {
		nextCursor = &payments[limit-1].CreatedAt
		payments = payments[:limit]
	}

	return payments, nextCursor, nil
}
