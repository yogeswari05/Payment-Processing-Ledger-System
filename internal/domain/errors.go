package domain

import (
	"errors"
	"fmt"
)

var (
	// Validation errors
	ErrInvalidAmount          = errors.New("amount must be a positive integer in minor units")
	ErrUnsupportedCurrency    = errors.New("currency must be one of: USD, EUR, GBP")
	ErrInvalidID              = errors.New("invalid or malformed UUID")
	ErrInvalidRequestPayload  = errors.New("invalid request payload")
	ErrMissingIdempotencyKey  = errors.New("Idempotency-Key header is required for this operation")

	// Authentication & Authorization errors
	ErrUnauthorized           = errors.New("missing or invalid API key")
	ErrForbidden              = errors.New("access to resource is forbidden for this merchant")

	// Entity Not Found errors
	ErrMerchantNotFound       = errors.New("merchant not found")
	ErrCustomerNotFound       = errors.New("customer not found")
	ErrPaymentNotFound        = errors.New("payment not found")
	ErrRefundNotFound         = errors.New("refund not found")
	ErrLedgerAccountNotFound  = errors.New("ledger account not found")
	ErrLedgerTxNotFound       = errors.New("ledger transaction not found")
	ErrReconciliationNotFound = errors.New("reconciliation run not found")

	// State machine & Business rules
	ErrInvalidStateTransition = errors.New("payment cannot perform this transition from its current state")
	ErrPaymentAlreadyCaptured = errors.New("payment has already been captured")
	ErrPaymentNotAuthorized   = errors.New("payment is not in authorized state")
	ErrExceedsAuthorizedAmount = errors.New("capture amount exceeds authorized amount")
	ErrExceedsRefundableAmount = errors.New("refund amount exceeds available refundable amount")
	ErrPaymentNotCaptured     = errors.New("payment must be captured before it can be refunded")
	ErrZeroRefundAmount       = errors.New("refund amount must be greater than zero")

	// Idempotency errors
	ErrIdempotencyKeyConflict = errors.New("concurrent request with the same idempotency key is currently in progress")
	ErrIdempotencyMismatch    = errors.New("idempotency key reused with different request parameters")

	// Ledger errors
	ErrUnbalancedLedgerTx     = errors.New("ledger transaction debits do not equal credits")
	ErrDuplicateLedgerPosting = errors.New("ledger transaction already posted for this business reference")
	ErrLedgerEntryZeroAmount  = errors.New("ledger entry amount must be strictly greater than zero")
	ErrLedgerCurrencyMismatch = errors.New("ledger transaction contains mixed currencies")
)

// DomainError wraps standard errors with a machine-readable code.
type DomainError struct {
	Code    string
	Message string
	Err     error
}

func (e *DomainError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func (e *DomainError) Unwrap() error {
	return e.Err
}

func NewDomainError(code, message string, underlying error) *DomainError {
	return &DomainError{
		Code:    code,
		Message: message,
		Err:     underlying,
	}
}
