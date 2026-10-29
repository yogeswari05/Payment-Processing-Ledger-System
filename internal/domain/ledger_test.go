package domain_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateBalancedEntries(t *testing.T) {
	acc1 := uuid.New()
	acc2 := uuid.New()
	acc3 := uuid.New()
	now := time.Now()

	t.Run("Valid balanced 2 entries (Capture 10000 cents)", func(t *testing.T) {
		entries := []domain.LedgerEntry{
			{
				ID:              uuid.New(),
				LedgerAccountID: acc1,
				Direction:       domain.DirectionDebit,
				Amount:          10000,
				Currency:        "USD",
				CreatedAt:       now,
			},
			{
				ID:              uuid.New(),
				LedgerAccountID: acc2,
				Direction:       domain.DirectionCredit,
				Amount:          10000,
				Currency:        "USD",
				CreatedAt:       now,
			},
		}
		require.NoError(t, domain.ValidateBalancedEntries("USD", entries))
	})

	t.Run("Valid balanced 3 entries with platform fee", func(t *testing.T) {
		entries := []domain.LedgerEntry{
			{
				ID:              uuid.New(),
				LedgerAccountID: acc1,
				Direction:       domain.DirectionDebit,
				Amount:          10000,
				Currency:        "USD",
				CreatedAt:       now,
			},
			{
				ID:              uuid.New(),
				LedgerAccountID: acc2,
				Direction:       domain.DirectionCredit,
				Amount:          9680, // Merchant net
				Currency:        "USD",
				CreatedAt:       now,
			},
			{
				ID:              uuid.New(),
				LedgerAccountID: acc3,
				Direction:       domain.DirectionCredit,
				Amount:          320, // Platform fee
				Currency:        "USD",
				CreatedAt:       now,
			},
		}
		require.NoError(t, domain.ValidateBalancedEntries("USD", entries))
	})

	t.Run("Unbalanced entries (Debits != Credits)", func(t *testing.T) {
		entries := []domain.LedgerEntry{
			{
				ID:              uuid.New(),
				LedgerAccountID: acc1,
				Direction:       domain.DirectionDebit,
				Amount:          10000,
				Currency:        "USD",
				CreatedAt:       now,
			},
			{
				ID:              uuid.New(),
				LedgerAccountID: acc2,
				Direction:       domain.DirectionCredit,
				Amount:          9000, // Missing 1000!
				Currency:        "USD",
				CreatedAt:       now,
			},
		}
		err := domain.ValidateBalancedEntries("USD", entries)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrUnbalancedLedgerTx)
	})

	t.Run("Fewer than 2 entries", func(t *testing.T) {
		entries := []domain.LedgerEntry{
			{
				ID:              uuid.New(),
				LedgerAccountID: acc1,
				Direction:       domain.DirectionDebit,
				Amount:          10000,
				Currency:        "USD",
				CreatedAt:       now,
			},
		}
		err := domain.ValidateBalancedEntries("USD", entries)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrUnbalancedLedgerTx)
	})

	t.Run("Currency mismatch", func(t *testing.T) {
		entries := []domain.LedgerEntry{
			{
				ID:              uuid.New(),
				LedgerAccountID: acc1,
				Direction:       domain.DirectionDebit,
				Amount:          10000,
				Currency:        "USD",
				CreatedAt:       now,
			},
			{
				ID:              uuid.New(),
				LedgerAccountID: acc2,
				Direction:       domain.DirectionCredit,
				Amount:          10000,
				Currency:        "EUR", // Mismatch!
				CreatedAt:       now,
			},
		}
		err := domain.ValidateBalancedEntries("USD", entries)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrLedgerCurrencyMismatch)
	})

	t.Run("Non-positive entry amount", func(t *testing.T) {
		entries := []domain.LedgerEntry{
			{
				ID:              uuid.New(),
				LedgerAccountID: acc1,
				Direction:       domain.DirectionDebit,
				Amount:          0,
				Currency:        "USD",
				CreatedAt:       now,
			},
			{
				ID:              uuid.New(),
				LedgerAccountID: acc2,
				Direction:       domain.DirectionCredit,
				Amount:          0,
				Currency:        "USD",
				CreatedAt:       now,
			},
		}
		err := domain.ValidateBalancedEntries("USD", entries)
		require.Error(t, err)
		assert.ErrorIs(t, err, domain.ErrLedgerEntryZeroAmount)
	})
}
