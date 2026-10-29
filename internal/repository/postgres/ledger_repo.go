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

type LedgerRepo struct{}

func NewLedgerRepo() *LedgerRepo {
	return &LedgerRepo{}
}

func (r *LedgerRepo) GetOrCreateSystemAccount(ctx context.Context, tx repository.DBTX, accType domain.AccountType, name, currency string) (*domain.LedgerAccount, error) {
	query := `
		INSERT INTO ledger_accounts (id, account_type, name, currency, merchant_id, created_at)
		VALUES ($1, $2, $3, $4, NULL, NOW())
		ON CONFLICT (account_type, name, currency, merchant_id) DO UPDATE
		SET name = EXCLUDED.name
		RETURNING id, account_type, name, currency, merchant_id, created_at
	`
	acc := &domain.LedgerAccount{}
	err := tx.QueryRow(ctx, query, uuid.New(), accType, name, currency).Scan(
		&acc.ID, &acc.AccountType, &acc.Name, &acc.Currency, &acc.MerchantID, &acc.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create system ledger account: %w", err)
	}
	return acc, nil
}

func (r *LedgerRepo) GetOrCreateMerchantAccount(ctx context.Context, tx repository.DBTX, merchantID uuid.UUID, accType domain.AccountType, name, currency string) (*domain.LedgerAccount, error) {
	query := `
		INSERT INTO ledger_accounts (id, account_type, name, currency, merchant_id, created_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (account_type, name, currency, merchant_id) DO UPDATE
		SET name = EXCLUDED.name
		RETURNING id, account_type, name, currency, merchant_id, created_at
	`
	acc := &domain.LedgerAccount{}
	err := tx.QueryRow(ctx, query, uuid.New(), accType, name, currency, merchantID).Scan(
		&acc.ID, &acc.AccountType, &acc.Name, &acc.Currency, &acc.MerchantID, &acc.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get or create merchant ledger account: %w", err)
	}
	return acc, nil
}

func (r *LedgerRepo) CreateTransaction(ctx context.Context, tx repository.DBTX, t *domain.LedgerTransaction) error {
	query := `
		INSERT INTO ledger_transactions (
			id, business_reference_type, business_reference_id, transaction_type,
			currency, status, description, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := tx.Exec(
		ctx, query,
		t.ID, t.BusinessReferenceType, t.BusinessReferenceID, t.TransactionType,
		t.Currency, t.Status, t.Description, t.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert ledger transaction: %w", err)
	}
	return nil
}

func (r *LedgerRepo) CreateEntries(ctx context.Context, tx repository.DBTX, entries []domain.LedgerEntry) error {
	for _, e := range entries {
		query := `
			INSERT INTO ledger_entries (
				id, ledger_transaction_id, ledger_account_id, direction,
				amount, currency, created_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7)
		`
		_, err := tx.Exec(
			ctx, query,
			e.ID, e.LedgerTransactionID, e.LedgerAccountID, e.Direction,
			e.Amount, e.Currency, e.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("failed to insert ledger entry: %w", err)
		}
	}
	return nil
}

func (r *LedgerRepo) GetTransactionByID(ctx context.Context, tx repository.DBTX, id uuid.UUID) (*domain.LedgerTransaction, error) {
	query := `
		SELECT
			id, business_reference_type, business_reference_id, transaction_type,
			currency, status, description, created_at
		FROM ledger_transactions
		WHERE id = $1
	`
	t := &domain.LedgerTransaction{}
	err := tx.QueryRow(ctx, query, id).Scan(
		&t.ID, &t.BusinessReferenceType, &t.BusinessReferenceID, &t.TransactionType,
		&t.Currency, &t.Status, &t.Description, &t.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrLedgerTxNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query ledger transaction: %w", err)
	}

	entriesQuery := `
		SELECT id, ledger_transaction_id, ledger_account_id, direction, amount, currency, created_at
		FROM ledger_entries
		WHERE ledger_transaction_id = $1
		ORDER BY created_at ASC
	`
	rows, err := tx.Query(ctx, entriesQuery, id)
	if err != nil {
		return nil, fmt.Errorf("failed to query ledger entries: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(
			&e.ID, &e.LedgerTransactionID, &e.LedgerAccountID, &e.Direction,
			&e.Amount, &e.Currency, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan ledger entry: %w", err)
		}
		t.Entries = append(t.Entries, e)
	}

	return t, nil
}

func (r *LedgerRepo) GetTransactionByBusinessRef(ctx context.Context, tx repository.DBTX, refType domain.BusinessReferenceType, refID uuid.UUID) (*domain.LedgerTransaction, error) {
	query := `
		SELECT
			id, business_reference_type, business_reference_id, transaction_type,
			currency, status, description, created_at
		FROM ledger_transactions
		WHERE business_reference_type = $1 AND business_reference_id = $2
	`
	t := &domain.LedgerTransaction{}
	err := tx.QueryRow(ctx, query, refType, refID).Scan(
		&t.ID, &t.BusinessReferenceType, &t.BusinessReferenceID, &t.TransactionType,
		&t.Currency, &t.Status, &t.Description, &t.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrLedgerTxNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query ledger transaction by business ref: %w", err)
	}
	return t, nil
}

func (r *LedgerRepo) ListAccounts(ctx context.Context, tx repository.DBTX, merchantID *uuid.UUID) ([]domain.LedgerAccount, error) {
	query := `
		SELECT id, account_type, name, currency, merchant_id, created_at
		FROM ledger_accounts
	`
	var args []any
	if merchantID != nil {
		query += " WHERE merchant_id = $1 OR merchant_id IS NULL"
		args = append(args, *merchantID)
	}
	query += " ORDER BY created_at ASC"

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list ledger accounts: %w", err)
	}
	defer rows.Close()

	var accounts []domain.LedgerAccount
	for rows.Next() {
		var a domain.LedgerAccount
		if err := rows.Scan(
			&a.ID, &a.AccountType, &a.Name, &a.Currency, &a.MerchantID, &a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan ledger account: %w", err)
		}
		accounts = append(accounts, a)
	}
	return accounts, nil
}

func (r *LedgerRepo) ListEntries(ctx context.Context, tx repository.DBTX, filter repository.ListEntriesFilter) ([]domain.LedgerEntry, *time.Time, error) {
	limit := filter.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	query := `
		SELECT id, ledger_transaction_id, ledger_account_id, direction, amount, currency, created_at
		FROM ledger_entries
		WHERE 1=1
	`
	var args []any
	argIdx := 1

	if filter.AccountID != nil {
		query += fmt.Sprintf(" AND ledger_account_id = $%d", argIdx)
		args = append(args, *filter.AccountID)
		argIdx++
	}

	if filter.TransactionID != nil {
		query += fmt.Sprintf(" AND ledger_transaction_id = $%d", argIdx)
		args = append(args, *filter.TransactionID)
		argIdx++
	}

	if filter.Cursor != nil {
		query += fmt.Sprintf(" AND created_at < $%d", argIdx)
		args = append(args, *filter.Cursor)
		argIdx++
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d", argIdx)
	args = append(args, limit+1)

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var e domain.LedgerEntry
		if err := rows.Scan(
			&e.ID, &e.LedgerTransactionID, &e.LedgerAccountID, &e.Direction,
			&e.Amount, &e.Currency, &e.CreatedAt,
		); err != nil {
			return nil, nil, fmt.Errorf("failed to scan ledger entry: %w", err)
		}
		entries = append(entries, e)
	}

	var nextCursor *time.Time
	if len(entries) > limit {
		nextCursor = &entries[limit-1].CreatedAt
		entries = entries[:limit]
	}

	return entries, nextCursor, nil
}
