package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/payflow/payflow/internal/domain"
)

// DBTX models the common interface between *pgxpool.Pool and pgx.Tx.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// TxManager executes units of work inside a database transaction.
type TxManager interface {
	WithTx(ctx context.Context, fn func(tx DBTX) error) error
}

type ListPaymentsFilter struct {
	MerchantID uuid.UUID
	CustomerID *uuid.UUID
	Status     *domain.PaymentStatus
	Limit      int
	Cursor     *time.Time // pagination based on created_at DESC
}

type ListEntriesFilter struct {
	AccountID     *uuid.UUID
	TransactionID *uuid.UUID
	Limit         int
	Cursor        *time.Time
}

type MerchantRepository interface {
	CreateMerchant(ctx context.Context, tx DBTX, m *domain.Merchant) error
	GetMerchantByID(ctx context.Context, tx DBTX, id uuid.UUID) (*domain.Merchant, error)
	GetMerchantByAPIKeyHash(ctx context.Context, tx DBTX, hash string) (*domain.Merchant, error)
	CreateCustomer(ctx context.Context, tx DBTX, c *domain.Customer) error
	GetCustomerByID(ctx context.Context, tx DBTX, merchantID, id uuid.UUID) (*domain.Customer, error)
}

type PaymentRepository interface {
	Create(ctx context.Context, tx DBTX, p *domain.Payment) error
	GetByID(ctx context.Context, tx DBTX, merchantID, id uuid.UUID) (*domain.Payment, error)
	GetByIDForUpdate(ctx context.Context, tx DBTX, merchantID, id uuid.UUID) (*domain.Payment, error)
	Update(ctx context.Context, tx DBTX, p *domain.Payment) error
	List(ctx context.Context, tx DBTX, filter ListPaymentsFilter) ([]domain.Payment, *time.Time, error)
}

type RefundRepository interface {
	Create(ctx context.Context, tx DBTX, r *domain.Refund) error
	GetByID(ctx context.Context, tx DBTX, merchantID, id uuid.UUID) (*domain.Refund, error)
	ListByPaymentID(ctx context.Context, tx DBTX, paymentID uuid.UUID) ([]domain.Refund, error)
}

type LedgerRepository interface {
	GetOrCreateSystemAccount(ctx context.Context, tx DBTX, accType domain.AccountType, name, currency string) (*domain.LedgerAccount, error)
	GetOrCreateMerchantAccount(ctx context.Context, tx DBTX, merchantID uuid.UUID, accType domain.AccountType, name, currency string) (*domain.LedgerAccount, error)
	CreateTransaction(ctx context.Context, tx DBTX, txRecord *domain.LedgerTransaction) error
	CreateEntries(ctx context.Context, tx DBTX, entries []domain.LedgerEntry) error
	GetTransactionByID(ctx context.Context, tx DBTX, id uuid.UUID) (*domain.LedgerTransaction, error)
	GetTransactionByBusinessRef(ctx context.Context, tx DBTX, refType domain.BusinessReferenceType, refID uuid.UUID) (*domain.LedgerTransaction, error)
	ListAccounts(ctx context.Context, tx DBTX, merchantID *uuid.UUID) ([]domain.LedgerAccount, error)
	ListEntries(ctx context.Context, tx DBTX, filter ListEntriesFilter) ([]domain.LedgerEntry, *time.Time, error)
}

type IdempotencyRepository interface {
	LockOrGet(ctx context.Context, tx DBTX, rec *domain.IdempotencyRecord) (*domain.IdempotencyRecord, error)
	SaveResponse(ctx context.Context, tx DBTX, rec *domain.IdempotencyRecord) error
}

type OutboxRepository interface {
	Create(ctx context.Context, tx DBTX, evt *domain.OutboxEvent) error
	ClaimBatch(ctx context.Context, pool *pgxpool.Pool, workerID string, batchSize int, lockTimeout time.Duration) ([]domain.OutboxEvent, error)
	MarkPublished(ctx context.Context, tx DBTX, eventID uuid.UUID) error
	RecordFailure(ctx context.Context, tx DBTX, eventID uuid.UUID, errMsg string, nextRetry time.Time, isDeadLetter bool) error
	RecordConsumed(ctx context.Context, tx DBTX, eventID uuid.UUID, consumerName string) (bool, error)
}

type ReconciliationRepository interface {
	CreateRun(ctx context.Context, tx DBTX, run *domain.ReconciliationRun) error
	UpdateRun(ctx context.Context, tx DBTX, run *domain.ReconciliationRun) error
	GetRunByID(ctx context.Context, tx DBTX, id uuid.UUID) (*domain.ReconciliationRun, error)
	CreateFindings(ctx context.Context, tx DBTX, findings []domain.ReconciliationFinding) error
	CheckUnbalancedTransactions(ctx context.Context, tx DBTX) ([]domain.ReconciliationFinding, error)
	CheckMissingCapturePostings(ctx context.Context, tx DBTX) ([]domain.ReconciliationFinding, error)
	CheckDuplicateCapturePostings(ctx context.Context, tx DBTX) ([]domain.ReconciliationFinding, error)
	CheckMissingRefundPostings(ctx context.Context, tx DBTX) ([]domain.ReconciliationFinding, error)
	CheckRefundExceedsCapture(ctx context.Context, tx DBTX) ([]domain.ReconciliationFinding, error)
	CheckPaymentStatusMismatches(ctx context.Context, tx DBTX) ([]domain.ReconciliationFinding, error)
	CheckOrphanedLedgerTransactions(ctx context.Context, tx DBTX) ([]domain.ReconciliationFinding, error)
}

// Type aliases for convenient reference
type MerchantRepo = MerchantRepository
type PaymentRepo = PaymentRepository
type RefundRepo = RefundRepository
type LedgerRepo = LedgerRepository
type IdempotencyRepo = IdempotencyRepository
type OutboxRepo = OutboxRepository
type ReconciliationRepo = ReconciliationRepository
