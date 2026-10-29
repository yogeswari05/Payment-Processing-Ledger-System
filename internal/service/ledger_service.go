package service

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
)

type LedgerService struct {
	ledgerRepo repository.LedgerRepo
}

func NewLedgerService(repo repository.LedgerRepo) *LedgerService {
	return &LedgerService{
		ledgerRepo: repo,
	}
}

// PostPaymentCapture creates a strictly balanced double-entry transaction for a payment capture:
// DEBIT:  customer_funds_clearing (Asset)       = captureAmount
// CREDIT: merchant_payable_{merchant} (Liability) = captureAmount - platformFee
// CREDIT: platform_fee_revenue (Revenue)          = platformFee
func (s *LedgerService) PostPaymentCapture(
	ctx context.Context,
	tx repository.DBTX,
	payment *domain.Payment,
	feeBps int64,
	feeFixed int64,
) (*domain.LedgerTransaction, error) {
	// 1. Calculate fee and net merchant amount
	var fee int64
	if feeBps > 0 || feeFixed > 0 {
		fee = (payment.CapturedAmount * feeBps / 10000) + feeFixed
		if fee > payment.CapturedAmount {
			fee = payment.CapturedAmount
		}
	}
	merchantNet := payment.CapturedAmount - fee

	// 2. Fetch or create the necessary accounts
	clearingAccount, err := s.ledgerRepo.GetOrCreateSystemAccount(
		ctx, tx, domain.AccountTypeAsset, "customer_funds_clearing", payment.Currency,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get customer clearing account: %w", err)
	}

	merchantPayable, err := s.ledgerRepo.GetOrCreateMerchantAccount(
		ctx, tx, payment.MerchantID, domain.AccountTypeLiability, "merchant_payable", payment.Currency,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get merchant payable account: %w", err)
	}

	var feeRevenue *domain.LedgerAccount
	if fee > 0 {
		feeRevenue, err = s.ledgerRepo.GetOrCreateSystemAccount(
			ctx, tx, domain.AccountTypeRevenue, "platform_fee_revenue", payment.Currency,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to get platform fee account: %w", err)
		}
	}

	// 3. Assemble entries
	txID := uuid.New()
	now := time.Now()

	entries := []domain.LedgerEntry{
		{
			ID:                  uuid.New(),
			LedgerTransactionID: txID,
			LedgerAccountID:     clearingAccount.ID,
			Direction:           domain.DirectionDebit,
			Amount:              payment.CapturedAmount,
			Currency:            payment.Currency,
			CreatedAt:           now,
		},
		{
			ID:                  uuid.New(),
			LedgerTransactionID: txID,
			LedgerAccountID:     merchantPayable.ID,
			Direction:           domain.DirectionCredit,
			Amount:              merchantNet,
			Currency:            payment.Currency,
			CreatedAt:           now,
		},
	}

	if fee > 0 && feeRevenue != nil {
		entries = append(entries, domain.LedgerEntry{
			ID:                  uuid.New(),
			LedgerTransactionID: txID,
			LedgerAccountID:     feeRevenue.ID,
			Direction:           domain.DirectionCredit,
			Amount:              fee,
			Currency:            payment.Currency,
			CreatedAt:           now,
		})
	}

	// 4. Verify double-entry equation (Total Debits == Total Credits)
	if err := domain.ValidateBalancedEntries(payment.Currency, entries); err != nil {
		return nil, fmt.Errorf("ledger balance validation failed: %w", err)
	}

	// 5. Persist transaction & entries
	lt := &domain.LedgerTransaction{
		ID:                    txID,
		BusinessReferenceType: domain.RefTypePaymentCapture,
		BusinessReferenceID:   payment.ID,
		TransactionType:       "CAPTURE",
		Currency:              payment.Currency,
		Status:                "POSTED",
		Description:           fmt.Sprintf("Payment capture for payment %s", payment.ID),
		CreatedAt:             now,
		Entries:               entries,
	}

	if err := s.ledgerRepo.CreateTransaction(ctx, tx, lt); err != nil {
		return nil, fmt.Errorf("failed creating ledger transaction: %w", err)
	}

	if err := s.ledgerRepo.CreateEntries(ctx, tx, entries); err != nil {
		return nil, fmt.Errorf("failed creating ledger entries: %w", err)
	}

	return lt, nil
}

// PostPaymentRefund creates a strictly balanced double-entry transaction for a refund:
// DEBIT:  merchant_payable_{merchant} (Liability) = refundAmount
// CREDIT: customer_funds_clearing (Asset)         = refundAmount
func (s *LedgerService) PostPaymentRefund(
	ctx context.Context,
	tx repository.DBTX,
	refund *domain.Refund,
) (*domain.LedgerTransaction, error) {
	clearingAccount, err := s.ledgerRepo.GetOrCreateSystemAccount(
		ctx, tx, domain.AccountTypeAsset, "customer_funds_clearing", refund.Currency,
	)
	if err != nil {
		return nil, fmt.Errorf("failed getting customer clearing account: %w", err)
	}

	merchantPayable, err := s.ledgerRepo.GetOrCreateMerchantAccount(
		ctx, tx, refund.MerchantID, domain.AccountTypeLiability, "merchant_payable", refund.Currency,
	)
	if err != nil {
		return nil, fmt.Errorf("failed getting merchant payable account: %w", err)
	}

	txID := uuid.New()
	now := time.Now()

	entries := []domain.LedgerEntry{
		{
			ID:                  uuid.New(),
			LedgerTransactionID: txID,
			LedgerAccountID:     merchantPayable.ID,
			Direction:           domain.DirectionDebit,
			Amount:              refund.Amount,
			Currency:            refund.Currency,
			CreatedAt:           now,
		},
		{
			ID:                  uuid.New(),
			LedgerTransactionID: txID,
			LedgerAccountID:     clearingAccount.ID,
			Direction:           domain.DirectionCredit,
			Amount:              refund.Amount,
			Currency:            refund.Currency,
			CreatedAt:           now,
		},
	}

	// Verify double-entry balance
	if err := domain.ValidateBalancedEntries(refund.Currency, entries); err != nil {
		return nil, fmt.Errorf("refund ledger balance validation failed: %w", err)
	}

	lt := &domain.LedgerTransaction{
		ID:                    txID,
		BusinessReferenceType: domain.RefTypePaymentRefund,
		BusinessReferenceID:   refund.ID,
		TransactionType:       "REFUND",
		Currency:              refund.Currency,
		Status:                "POSTED",
		Description:           fmt.Sprintf("Refund for payment %s (refund %s)", refund.PaymentID, refund.ID),
		CreatedAt:             now,
		Entries:               entries,
	}

	if err := s.ledgerRepo.CreateTransaction(ctx, tx, lt); err != nil {
		return nil, fmt.Errorf("failed creating refund ledger transaction: %w", err)
	}

	if err := s.ledgerRepo.CreateEntries(ctx, tx, entries); err != nil {
		return nil, fmt.Errorf("failed creating refund ledger entries: %w", err)
	}

	return lt, nil
}

func (s *LedgerService) GetTransaction(ctx context.Context, tx repository.DBTX, id uuid.UUID) (*domain.LedgerTransaction, error) {
	return s.ledgerRepo.GetTransactionByID(ctx, tx, id)
}

func (s *LedgerService) ListAccounts(ctx context.Context, tx repository.DBTX, merchantID *uuid.UUID) ([]domain.LedgerAccount, error) {
	return s.ledgerRepo.ListAccounts(ctx, tx, merchantID)
}

func (s *LedgerService) ListEntries(ctx context.Context, tx repository.DBTX, filter repository.ListEntriesFilter) ([]domain.LedgerEntry, *time.Time, error) {
	return s.ledgerRepo.ListEntries(ctx, tx, filter)
}
