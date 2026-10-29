package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ReconciliationStatus string

const (
	ReconStatusRunning   ReconciliationStatus = "RUNNING"
	ReconStatusCompleted ReconciliationStatus = "COMPLETED"
	ReconStatusFailed    ReconciliationStatus = "FAILED"
)

type FindingSeverity string

const (
	SeverityLow      FindingSeverity = "LOW"
	SeverityMedium   FindingSeverity = "MEDIUM"
	SeverityHigh     FindingSeverity = "HIGH"
	SeverityCritical FindingSeverity = "CRITICAL"
)

const (
	AnomalyUnbalancedLedgerTransaction = "UNBALANCED_LEDGER_TRANSACTION"
	AnomalyMissingCapturePosting       = "MISSING_CAPTURE_LEDGER_POSTING"
	AnomalyDuplicateCapturePosting     = "DUPLICATE_CAPTURE_POSTING"
	AnomalyMissingRefundPosting        = "MISSING_REFUND_LEDGER_POSTING"
	AnomalyRefundExceedsCapture        = "REFUND_EXCEEDS_CAPTURE"
	AnomalyPaymentStatusMismatch       = "PAYMENT_STATUS_AMOUNT_MISMATCH"
	AnomalyOrphanedLedgerTransaction   = "ORPHANED_LEDGER_TRANSACTION"
)

// ReconciliationRun records metadata and outcome of an independent audit run.
type ReconciliationRun struct {
	ID             uuid.UUID               `json:"id"`
	Status         ReconciliationStatus    `json:"status"`
	StartedAt      time.Time               `json:"started_at"`
	CompletedAt    *time.Time              `json:"completed_at,omitempty"`
	TotalChecks    int                     `json:"total_checks"`
	TotalAnomalies int                     `json:"total_anomalies"`
	Summary        json.RawMessage         `json:"summary"`
	Findings       []ReconciliationFinding `json:"findings,omitempty"`
}

// ReconciliationFinding represents an identified invariant violation or data mismatch.
type ReconciliationFinding struct {
	ID          uuid.UUID       `json:"id"`
	RunID       uuid.UUID       `json:"run_id"`
	AnomalyType string          `json:"anomaly_type"`
	Severity    FindingSeverity `json:"severity"`
	EntityType  string          `json:"entity_type"`
	EntityID    uuid.UUID       `json:"entity_id"`
	Description string          `json:"description"`
	Details     json.RawMessage `json:"details"`
	DetectedAt  time.Time       `json:"detected_at"`
}
