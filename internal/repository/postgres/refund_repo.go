package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
)

type RefundRepo struct{}

func NewRefundRepo() *RefundRepo {
	return &RefundRepo{}
}

func (r *RefundRepo) Create(ctx context.Context, tx repository.DBTX, ref *domain.Refund) error {
	query := `
		INSERT INTO refunds (
			id, payment_id, merchant_id, amount, currency, status, reason,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := tx.Exec(
		ctx, query,
		ref.ID, ref.PaymentID, ref.MerchantID, ref.Amount, ref.Currency, ref.Status,
		ref.Reason, ref.CreatedAt, ref.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert refund: %w", err)
	}
	return nil
}

func (r *RefundRepo) GetByID(ctx context.Context, tx repository.DBTX, merchantID, id uuid.UUID) (*domain.Refund, error) {
	query := `
		SELECT id, payment_id, merchant_id, amount, currency, status, reason, created_at, updated_at
		FROM refunds
		WHERE merchant_id = $1 AND id = $2
	`
	ref := &domain.Refund{}
	err := tx.QueryRow(ctx, query, merchantID, id).Scan(
		&ref.ID, &ref.PaymentID, &ref.MerchantID, &ref.Amount, &ref.Currency,
		&ref.Status, &ref.Reason, &ref.CreatedAt, &ref.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrRefundNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get refund: %w", err)
	}
	return ref, nil
}

func (r *RefundRepo) ListByPaymentID(ctx context.Context, tx repository.DBTX, paymentID uuid.UUID) ([]domain.Refund, error) {
	query := `
		SELECT id, payment_id, merchant_id, amount, currency, status, reason, created_at, updated_at
		FROM refunds
		WHERE payment_id = $1
		ORDER BY created_at ASC
	`
	rows, err := tx.Query(ctx, query, paymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list refunds: %w", err)
	}
	defer rows.Close()

	var refunds []domain.Refund
	for rows.Next() {
		var ref domain.Refund
		if err := rows.Scan(
			&ref.ID, &ref.PaymentID, &ref.MerchantID, &ref.Amount, &ref.Currency,
			&ref.Status, &ref.Reason, &ref.CreatedAt, &ref.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan refund: %w", err)
		}
		refunds = append(refunds, ref)
	}
	return refunds, nil
}
