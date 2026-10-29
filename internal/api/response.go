package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/payflow/payflow/internal/domain"
)

type ErrorEnvelope struct {
	Error ErrorDetails `json:"error"`
}

type ErrorDetails struct {
	Type    string `json:"type"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type PaginatedList[T any] struct {
	Data       []T     `json:"data"`
	HasMore    bool    `json:"has_more"`
	NextCursor *string `json:"next_cursor,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func WriteError(w http.ResponseWriter, status int, errType, errCode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorEnvelope{
		Error: ErrorDetails{
			Type:    errType,
			Code:    errCode,
			Message: message,
		},
	})
}

// MapDomainError inspects domain error types and translates them into uniform HTTP responses.
func MapDomainError(w http.ResponseWriter, err error) {
	switch {
	// 400 Bad Request
	case errors.Is(err, domain.ErrInvalidAmount):
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "invalid_amount", err.Error())
	case errors.Is(err, domain.ErrUnsupportedCurrency):
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "unsupported_currency", err.Error())
	case errors.Is(err, domain.ErrInvalidID):
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "invalid_id", err.Error())
	case errors.Is(err, domain.ErrInvalidRequestPayload):
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "invalid_payload", err.Error())
	case errors.Is(err, domain.ErrZeroRefundAmount):
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "zero_refund_amount", err.Error())
	case errors.Is(err, domain.ErrMissingIdempotencyKey):
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "missing_idempotency_key", err.Error())

	// 401 Unauthorized
	case errors.Is(err, domain.ErrUnauthorized):
		WriteError(w, http.StatusUnauthorized, "authentication_error", "unauthorized", err.Error())

	// 403 Forbidden
	case errors.Is(err, domain.ErrForbidden):
		WriteError(w, http.StatusForbidden, "authorization_error", "forbidden", err.Error())

	// 404 Not Found
	case errors.Is(err, domain.ErrMerchantNotFound):
		WriteError(w, http.StatusNotFound, "invalid_request_error", "merchant_not_found", "The specified merchant does not exist.")
	case errors.Is(err, domain.ErrCustomerNotFound):
		WriteError(w, http.StatusNotFound, "invalid_request_error", "customer_not_found", "The specified customer does not exist.")
	case errors.Is(err, domain.ErrPaymentNotFound):
		WriteError(w, http.StatusNotFound, "invalid_request_error", "payment_not_found", "The specified payment does not exist.")
	case errors.Is(err, domain.ErrRefundNotFound):
		WriteError(w, http.StatusNotFound, "invalid_request_error", "refund_not_found", "The specified refund does not exist.")
	case errors.Is(err, domain.ErrLedgerAccountNotFound):
		WriteError(w, http.StatusNotFound, "invalid_request_error", "account_not_found", "The specified ledger account does not exist.")
	case errors.Is(err, domain.ErrLedgerTxNotFound):
		WriteError(w, http.StatusNotFound, "invalid_request_error", "ledger_tx_not_found", "The specified ledger transaction does not exist.")
	case errors.Is(err, domain.ErrReconciliationNotFound):
		WriteError(w, http.StatusNotFound, "invalid_request_error", "reconciliation_not_found", "The specified reconciliation run does not exist.")

	// 409 Conflict
	case errors.Is(err, domain.ErrIdempotencyKeyConflict):
		w.Header().Set("Retry-After", "1")
		WriteError(w, http.StatusConflict, "idempotency_error", "concurrent_request_in_progress", "A concurrent request with the same idempotency key is currently processing. Please retry.")

	// 422 Unprocessable Entity
	case errors.Is(err, domain.ErrIdempotencyMismatch):
		WriteError(w, http.StatusUnprocessableEntity, "idempotency_error", "idempotency_key_mismatch", "The idempotency key was previously used with different request parameters.")
	case errors.Is(err, domain.ErrInvalidStateTransition):
		WriteError(w, http.StatusUnprocessableEntity, "invalid_request_error", "invalid_state_transition", err.Error())
	case errors.Is(err, domain.ErrPaymentAlreadyCaptured):
		WriteError(w, http.StatusUnprocessableEntity, "invalid_request_error", "payment_already_captured", "Payment has already been captured.")
	case errors.Is(err, domain.ErrPaymentNotAuthorized):
		WriteError(w, http.StatusUnprocessableEntity, "invalid_request_error", "payment_not_authorized", "Payment must be in authorized status.")
	case errors.Is(err, domain.ErrPaymentNotCaptured):
		WriteError(w, http.StatusUnprocessableEntity, "invalid_request_error", "payment_not_captured", "Payment must be captured before it can be refunded.")
	case errors.Is(err, domain.ErrExceedsAuthorizedAmount):
		WriteError(w, http.StatusUnprocessableEntity, "invalid_request_error", "exceeds_authorized_amount", err.Error())
	case errors.Is(err, domain.ErrExceedsRefundableAmount):
		WriteError(w, http.StatusUnprocessableEntity, "invalid_request_error", "exceeds_refundable_amount", err.Error())

	// 500 Internal Error (Default Fallback, avoid leaking SQL/stack trace)
	default:
		WriteError(w, http.StatusInternalServerError, "api_error", "internal_server_error", "An error occurred while processing your request.")
	}
}

// FormatTimeCursor formats a time.Time as an RFC3339 string for cursor pagination.
func FormatTimeCursor(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339Nano)
	return &s
}
