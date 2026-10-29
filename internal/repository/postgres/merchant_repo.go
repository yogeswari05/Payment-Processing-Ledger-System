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

type MerchantRepo struct{}

func NewMerchantRepo() *MerchantRepo {
	return &MerchantRepo{}
}

func (r *MerchantRepo) CreateMerchant(ctx context.Context, tx repository.DBTX, m *domain.Merchant) error {
	query := `
		INSERT INTO merchants (id, name, status, api_key_hash, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := tx.Exec(ctx, query, m.ID, m.Name, m.Status, m.APIKeyHash, m.CreatedAt, m.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert merchant: %w", err)
	}
	return nil
}

func (r *MerchantRepo) GetMerchantByID(ctx context.Context, tx repository.DBTX, id uuid.UUID) (*domain.Merchant, error) {
	query := `
		SELECT id, name, status, api_key_hash, created_at, updated_at
		FROM merchants
		WHERE id = $1
	`
	m := &domain.Merchant{}
	err := tx.QueryRow(ctx, query, id).Scan(
		&m.ID, &m.Name, &m.Status, &m.APIKeyHash, &m.CreatedAt, &m.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMerchantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query merchant by id: %w", err)
	}
	return m, nil
}

func (r *MerchantRepo) GetMerchantByAPIKeyHash(ctx context.Context, tx repository.DBTX, hash string) (*domain.Merchant, error) {
	query := `
		SELECT id, name, status, api_key_hash, created_at, updated_at
		FROM merchants
		WHERE api_key_hash = $1
	`
	m := &domain.Merchant{}
	err := tx.QueryRow(ctx, query, hash).Scan(
		&m.ID, &m.Name, &m.Status, &m.APIKeyHash, &m.CreatedAt, &m.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrMerchantNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query merchant by api key: %w", err)
	}
	return m, nil
}

func (r *MerchantRepo) CreateCustomer(ctx context.Context, tx repository.DBTX, c *domain.Customer) error {
	query := `
		INSERT INTO customers (id, merchant_id, email, name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	_, err := tx.Exec(ctx, query, c.ID, c.MerchantID, c.Email, c.Name, c.CreatedAt, c.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to insert customer: %w", err)
	}
	return nil
}

func (r *MerchantRepo) GetCustomerByID(ctx context.Context, tx repository.DBTX, merchantID, id uuid.UUID) (*domain.Customer, error) {
	query := `
		SELECT id, merchant_id, email, name, created_at, updated_at
		FROM customers
		WHERE merchant_id = $1 AND id = $2
	`
	c := &domain.Customer{}
	err := tx.QueryRow(ctx, query, merchantID, id).Scan(
		&c.ID, &c.MerchantID, &c.Email, &c.Name, &c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrCustomerNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query customer: %w", err)
	}
	return c, nil
}
