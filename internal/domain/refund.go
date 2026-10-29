package domain

import (
	"time"

	"github.com/google/uuid"
)

type RefundStatus string

const (
	RefundStatusPending   RefundStatus = "pending"
	RefundStatusSucceeded RefundStatus = "succeeded"
	RefundStatusFailed    RefundStatus = "failed"
)

// Refund represents a reversal of captured payment funds.
type Refund struct {
	ID         uuid.UUID    `json:"id"`
	PaymentID  uuid.UUID    `json:"payment_id"`
	MerchantID uuid.UUID    `json:"merchant_id"`
	Amount     int64        `json:"amount"` // Minor units (e.g., cents)
	Currency   string       `json:"currency"`
	Status     RefundStatus `json:"status"`
	Reason     string       `json:"reason"`
	CreatedAt  time.Time    `json:"created_at"`
	UpdatedAt  time.Time    `json:"updated_at"`
}
