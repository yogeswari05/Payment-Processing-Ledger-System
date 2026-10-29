package api_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/payflow/payflow/internal/api"
	"github.com/payflow/payflow/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMapDomainError(t *testing.T) {
	tests := []struct {
		name           string
		err            error
		expectedStatus int
		expectedType   string
		expectedCode   string
	}{
		{
			name:           "Invalid Amount -> 400",
			err:            domain.ErrInvalidAmount,
			expectedStatus: http.StatusBadRequest,
			expectedType:   "invalid_request_error",
			expectedCode:   "invalid_amount",
		},
		{
			name:           "Unsupported Currency -> 400",
			err:            domain.ErrUnsupportedCurrency,
			expectedStatus: http.StatusBadRequest,
			expectedType:   "invalid_request_error",
			expectedCode:   "unsupported_currency",
		},
		{
			name:           "Unauthorized -> 401",
			err:            domain.ErrUnauthorized,
			expectedStatus: http.StatusUnauthorized,
			expectedType:   "authentication_error",
			expectedCode:   "unauthorized",
		},
		{
			name:           "Payment Not Found -> 404",
			err:            domain.ErrPaymentNotFound,
			expectedStatus: http.StatusNotFound,
			expectedType:   "invalid_request_error",
			expectedCode:   "payment_not_found",
		},
		{
			name:           "Idempotency In Progress -> 409",
			err:            domain.ErrIdempotencyKeyConflict,
			expectedStatus: http.StatusConflict,
			expectedType:   "idempotency_error",
			expectedCode:   "concurrent_request_in_progress",
		},
		{
			name:           "Idempotency Mismatch -> 422",
			err:            domain.ErrIdempotencyMismatch,
			expectedStatus: http.StatusUnprocessableEntity,
			expectedType:   "idempotency_error",
			expectedCode:   "idempotency_key_mismatch",
		},
		{
			name:           "Invalid State Transition -> 422",
			err:            domain.ErrInvalidStateTransition,
			expectedStatus: http.StatusUnprocessableEntity,
			expectedType:   "invalid_request_error",
			expectedCode:   "invalid_state_transition",
		},
		{
			name:           "Unknown internal error -> 500 (safe message, no leak)",
			err:            errors.New("raw postgres internal connection error: failed to dial 10.0.0.1"),
			expectedStatus: http.StatusInternalServerError,
			expectedType:   "api_error",
			expectedCode:   "internal_server_error",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			api.MapDomainError(rec, tc.err)

			assert.Equal(t, tc.expectedStatus, rec.Code)

			var env api.ErrorEnvelope
			err := json.Unmarshal(rec.Body.Bytes(), &env)
			require.NoError(t, err)

			assert.Equal(t, tc.expectedType, env.Error.Type)
			assert.Equal(t, tc.expectedCode, env.Error.Code)
			assert.NotEmpty(t, env.Error.Message)
		})
	}
}
