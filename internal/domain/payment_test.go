package domain_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaymentStateMachineTransitions(t *testing.T) {
	tests := []struct {
		current  domain.PaymentStatus
		target   domain.PaymentStatus
		expected bool
	}{
		// From Created
		{current: domain.PaymentStatusCreated, target: domain.PaymentStatusAuthorized, expected: true},
		{current: domain.PaymentStatusCreated, target: domain.PaymentStatusCanceled, expected: true},
		{current: domain.PaymentStatusCreated, target: domain.PaymentStatusFailed, expected: true},
		{current: domain.PaymentStatusCreated, target: domain.PaymentStatusCaptured, expected: false},
		{current: domain.PaymentStatusCreated, target: domain.PaymentStatusRefunded, expected: false},

		// From Authorized
		{current: domain.PaymentStatusAuthorized, target: domain.PaymentStatusCaptured, expected: true},
		{current: domain.PaymentStatusAuthorized, target: domain.PaymentStatusCanceled, expected: true},
		{current: domain.PaymentStatusAuthorized, target: domain.PaymentStatusAuthorized, expected: false},
		{current: domain.PaymentStatusAuthorized, target: domain.PaymentStatusRefunded, expected: false},
		{current: domain.PaymentStatusAuthorized, target: domain.PaymentStatusPartiallyRefunded, expected: false},

		// From Captured
		{current: domain.PaymentStatusCaptured, target: domain.PaymentStatusPartiallyRefunded, expected: true},
		{current: domain.PaymentStatusCaptured, target: domain.PaymentStatusRefunded, expected: true},
		{current: domain.PaymentStatusCaptured, target: domain.PaymentStatusCanceled, expected: false},
		{current: domain.PaymentStatusCaptured, target: domain.PaymentStatusAuthorized, expected: false},

		// From Partially Refunded
		{current: domain.PaymentStatusPartiallyRefunded, target: domain.PaymentStatusPartiallyRefunded, expected: true},
		{current: domain.PaymentStatusPartiallyRefunded, target: domain.PaymentStatusRefunded, expected: true},
		{current: domain.PaymentStatusPartiallyRefunded, target: domain.PaymentStatusCaptured, expected: false},
		{current: domain.PaymentStatusPartiallyRefunded, target: domain.PaymentStatusCanceled, expected: false},

		// Terminal States (Canceled, Refunded, Failed)
		{current: domain.PaymentStatusCanceled, target: domain.PaymentStatusCaptured, expected: false},
		{current: domain.PaymentStatusRefunded, target: domain.PaymentStatusCaptured, expected: false},
		{current: domain.PaymentStatusFailed, target: domain.PaymentStatusAuthorized, expected: false},
	}

	for _, tc := range tests {
		p := &domain.Payment{Status: tc.current}
		allowed := p.CanTransitionTo(tc.target)
		assert.Equal(t, tc.expected, allowed, "transition from %s to %s should be %v", tc.current, tc.target, tc.expected)
	}
}

func TestPaymentValidateCapture(t *testing.T) {
	p := &domain.Payment{
		ID:             uuid.New(),
		Amount:         10000, // $100.00
		CapturedAmount: 0,
		Status:         domain.PaymentStatusAuthorized,
	}

	// Full capture valid
	require.NoError(t, p.ValidateCapture(10000))

	// Partial capture valid
	require.NoError(t, p.ValidateCapture(5000))

	// Zero or negative capture invalid
	require.ErrorIs(t, p.ValidateCapture(0), domain.ErrInvalidAmount)
	require.ErrorIs(t, p.ValidateCapture(-100), domain.ErrInvalidAmount)

	// Capture exceeding authorized amount
	err := p.ValidateCapture(15000)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrExceedsAuthorizedAmount)

	// Capture when not in authorized status
	p.Status = domain.PaymentStatusCreated
	err = p.ValidateCapture(10000)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrPaymentNotAuthorized)
}

func TestPaymentValidateRefund(t *testing.T) {
	p := &domain.Payment{
		ID:             uuid.New(),
		Amount:         10000,
		CapturedAmount: 10000,
		RefundedAmount: 0,
		Status:         domain.PaymentStatusCaptured,
	}

	// Full refund valid
	require.NoError(t, p.ValidateRefund(10000))

	// Partial refund valid
	require.NoError(t, p.ValidateRefund(4000))

	// Refund exceeding captured amount
	err := p.ValidateRefund(12000)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrExceedsRefundableAmount)

	// Zero refund invalid
	require.ErrorIs(t, p.ValidateRefund(0), domain.ErrZeroRefundAmount)

	// Refund on uncaptured payment invalid
	p.Status = domain.PaymentStatusAuthorized
	err = p.ValidateRefund(1000)
	require.Error(t, err)
	assert.ErrorIs(t, err, domain.ErrPaymentNotCaptured)

	// Subsequent partial refund
	p.Status = domain.PaymentStatusPartiallyRefunded
	p.RefundedAmount = 4000
	assert.Equal(t, int64(6000), p.RefundableAmount())
	require.NoError(t, p.ValidateRefund(6000))
	require.ErrorIs(t, p.ValidateRefund(6001), domain.ErrExceedsRefundableAmount)
}
