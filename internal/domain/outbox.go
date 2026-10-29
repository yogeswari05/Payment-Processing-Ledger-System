package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type OutboxStatus string

const (
	OutboxStatusPending    OutboxStatus = "PENDING"
	OutboxStatusProcessing OutboxStatus = "PROCESSING"
	OutboxStatusPublished  OutboxStatus = "PUBLISHED"
	OutboxStatusDeadLetter OutboxStatus = "DEAD_LETTER"
)

const (
	EventPaymentCreated    = "payment.created"
	EventPaymentAuthorized = "payment.authorized"
	EventPaymentCaptured   = "payment.captured"
	EventPaymentRefunded   = "payment.refunded"
	EventPaymentCanceled   = "payment.canceled"
	EventPaymentFailed     = "payment.failed"
	EventLedgerPosted      = "ledger.posted"
)

// OutboxEvent represents a transactional outbox event committed atomically with business changes.
type OutboxEvent struct {
	ID            uuid.UUID       `json:"id"`
	EventType     string          `json:"event_type"`
	AggregateType string          `json:"aggregate_type"`
	AggregateID   uuid.UUID       `json:"aggregate_id"`
	Payload       json.RawMessage `json:"payload"`
	Status        OutboxStatus    `json:"status"`
	Attempts      int             `json:"attempts"`
	MaxAttempts   int             `json:"max_attempts"`
	NextRetryAt   time.Time       `json:"next_retry_at"`
	LockedAt      *time.Time      `json:"locked_at,omitempty"`
	LockedBy      *string         `json:"locked_by,omitempty"`
	LastError     *string         `json:"last_error,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	PublishedAt   *time.Time      `json:"published_at,omitempty"`
}
