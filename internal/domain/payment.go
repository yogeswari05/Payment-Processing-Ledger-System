package domain

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type PaymentStatus string

const (
	PaymentStatusCreated           PaymentStatus = "created"
	PaymentStatusAuthorized        PaymentStatus = "authorized"
	PaymentStatusCaptured          PaymentStatus = "captured"
	PaymentStatusPartiallyRefunded PaymentStatus = "partially_refunded"
	PaymentStatusRefunded          PaymentStatus = "refunded"
	PaymentStatusCanceled          PaymentStatus = "canceled"
	PaymentStatusFailed            PaymentStatus = "failed"
)

// Payment represents a customer payment lifecycle entity.
type Payment struct {
	ID             uuid.UUID       `json:"id"`
	MerchantID     uuid.UUID       `json:"merchant_id"`
	CustomerID     uuid.UUID       `json:"customer_id"`
	Amount         int64           `json:"amount"` // Minor units (e.g., cents)
	Currency       string          `json:"currency"`
	Status         PaymentStatus   `json:"status"`
	CapturedAmount int64           `json:"captured_amount"`
	RefundedAmount int64           `json:"refunded_amount"`
	Description    string          `json:"description"`
	Metadata       json.RawMessage `json:"metadata"`
	Version        int             `json:"version"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// CanTransitionTo validates whether a proposed state transition is valid per the state machine.
func (p *Payment) CanTransitionTo(next PaymentStatus) bool {
	switch p.Status {
	case PaymentStatusCreated:
		return next == PaymentStatusAuthorized || next == PaymentStatusCanceled || next == PaymentStatusFailed
	case PaymentStatusAuthorized:
		return next == PaymentStatusCaptured || next == PaymentStatusCanceled
	case PaymentStatusCaptured:
		return next == PaymentStatusPartiallyRefunded || next == PaymentStatusRefunded
	case PaymentStatusPartiallyRefunded:
		return next == PaymentStatusPartiallyRefunded || next == PaymentStatusRefunded
	default:
		// Terminal states: refunded, canceled, failed cannot transition to anything
		return false
	}
}

// RefundableAmount calculates the remaining captured funds that can be refunded.
func (p *Payment) RefundableAmount() int64 {
	if p.Status != PaymentStatusCaptured && p.Status != PaymentStatusPartiallyRefunded {
		return 0
	}
	remaining := p.CapturedAmount - p.RefundedAmount
	if remaining < 0 {
		return 0
	}
	return remaining
}

// ValidateCapture verifies if capture can proceed with the requested amount.
func (p *Payment) ValidateCapture(requestedAmount int64) error {
	if p.Status != PaymentStatusAuthorized {
		return fmt.Errorf("%w: payment is in '%s' status, expected 'authorized'", ErrPaymentNotAuthorized, p.Status)
	}
	if requestedAmount <= 0 {
		return ErrInvalidAmount
	}
	if requestedAmount > p.Amount {
		return fmt.Errorf("%w: requested capture %d > authorized amount %d", ErrExceedsAuthorizedAmount, requestedAmount, p.Amount)
	}
	return nil
}

// ValidateRefund verifies if refund can proceed with the requested amount.
func (p *Payment) ValidateRefund(requestedAmount int64) error {
	if p.Status != PaymentStatusCaptured && p.Status != PaymentStatusPartiallyRefunded {
		return fmt.Errorf("%w: payment is in '%s' status", ErrPaymentNotCaptured, p.Status)
	}
	if requestedAmount <= 0 {
		return ErrZeroRefundAmount
	}
	available := p.RefundableAmount()
	if requestedAmount > available {
		return fmt.Errorf("%w: requested %d exceeds available %d", ErrExceedsRefundableAmount, requestedAmount, available)
	}
	return nil
}
