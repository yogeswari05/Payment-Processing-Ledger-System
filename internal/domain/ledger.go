package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type AccountType string

const (
	AccountTypeAsset     AccountType = "asset"
	AccountTypeLiability AccountType = "liability"
	AccountTypeEquity    AccountType = "equity"
	AccountTypeRevenue   AccountType = "revenue"
	AccountTypeExpense   AccountType = "expense"
)

type EntryDirection string

const (
	DirectionDebit  EntryDirection = "DEBIT"
	DirectionCredit EntryDirection = "CREDIT"
)

type BusinessReferenceType string

const (
	RefTypePaymentCapture BusinessReferenceType = "PAYMENT_CAPTURE"
	RefTypePaymentRefund  BusinessReferenceType = "PAYMENT_REFUND"
	RefTypeAdjustment     BusinessReferenceType = "ADJUSTMENT"
)

// LedgerAccount represents a double-entry balance sheet or income statement account.
type LedgerAccount struct {
	ID          uuid.UUID   `json:"id"`
	AccountType AccountType `json:"account_type"`
	Name        string      `json:"name"`
	Currency    string      `json:"currency"`
	MerchantID  *uuid.UUID  `json:"merchant_id,omitempty"`
	CreatedAt   time.Time   `json:"created_at"`
}

// LedgerTransaction represents a balanced group of debit and credit postings.
type LedgerTransaction struct {
	ID                    uuid.UUID             `json:"id"`
	BusinessReferenceType BusinessReferenceType `json:"business_reference_type"`
	BusinessReferenceID   uuid.UUID             `json:"business_reference_id"`
	TransactionType       string                `json:"transaction_type"`
	Currency              string                `json:"currency"`
	Status                string                `json:"status"` // POSTED, VOIDED
	Description           string                `json:"description"`
	CreatedAt             time.Time             `json:"created_at"`
	Entries               []LedgerEntry         `json:"entries,omitempty"`
}

// LedgerEntry represents a single debit or credit movement against a ledger account.
type LedgerEntry struct {
	ID                  uuid.UUID      `json:"id"`
	LedgerTransactionID uuid.UUID      `json:"ledger_transaction_id"`
	LedgerAccountID     uuid.UUID      `json:"ledger_account_id"`
	Direction           EntryDirection `json:"direction"`
	Amount              int64          `json:"amount"` // Positive integer in minor units
	Currency            string         `json:"currency"`
	CreatedAt           time.Time      `json:"created_at"`
}

// ValidateBalancedEntries enforces the fundamental double-entry invariant:
// 1. At least two entries.
// 2. All amounts strictly positive.
// 3. All currencies match the transaction currency.
// 4. Sum of debits == Sum of credits.
func ValidateBalancedEntries(expectedCurrency string, entries []LedgerEntry) error {
	if len(entries) < 2 {
		return fmt.Errorf("%w: transaction must have at least 2 entries, got %d", ErrUnbalancedLedgerTx, len(entries))
	}

	var totalDebit int64
	var totalCredit int64

	for i, entry := range entries {
		if entry.Amount <= 0 {
			return fmt.Errorf("%w: entry %d has non-positive amount %d", ErrLedgerEntryZeroAmount, i, entry.Amount)
		}
		if entry.Currency != expectedCurrency {
			return fmt.Errorf("%w: entry %d has currency %s, expected %s", ErrLedgerCurrencyMismatch, i, entry.Currency, expectedCurrency)
		}

		switch entry.Direction {
		case DirectionDebit:
			totalDebit += entry.Amount
		case DirectionCredit:
			totalCredit += entry.Amount
		default:
			return fmt.Errorf("invalid entry direction: %s", entry.Direction)
		}
	}

	if totalDebit != totalCredit {
		return fmt.Errorf("%w: total debits (%d) != total credits (%d)", ErrUnbalancedLedgerTx, totalDebit, totalCredit)
	}

	return nil
}
