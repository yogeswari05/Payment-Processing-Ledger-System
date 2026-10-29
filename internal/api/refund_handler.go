package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/service"
)

type RefundHandler struct {
	refundService *service.RefundService
}

func NewRefundHandler(rs *service.RefundService) *RefundHandler {
	return &RefundHandler{refundService: rs}
}

type CreateRefundRequest struct {
	Amount int64  `json:"amount"`
	Reason string `json:"reason"`
}

func (h *RefundHandler) CreateRefund(w http.ResponseWriter, r *http.Request) {
	merchant, err := MerchantFromContext(r.Context())
	if err != nil {
		MapDomainError(w, err)
		return
	}

	pID, err := uuid.Parse(r.PathValue("payment_id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "invalid_id", "Payment ID must be a valid UUID.")
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "read_error", "Failed reading request body.")
		return
	}

	var req CreateRefundRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "malformed_json", "Invalid JSON payload.")
		return
	}

	idemKey := r.Header.Get("Idempotency-Key")
	reqHash := domain.ComputeRequestHash(r.Method, r.URL.Path, bodyBytes)

	refund, isReplay, err := h.refundService.CreateRefund(r.Context(), service.CreateRefundParams{
		MerchantID:     merchant.ID,
		PaymentID:      pID,
		Amount:         req.Amount,
		Reason:         req.Reason,
		IdempotencyKey: idemKey,
		RequestHash:    reqHash,
	})
	if err != nil {
		MapDomainError(w, err)
		return
	}

	if isReplay {
		w.Header().Set("Idempotent-Replayed", "true")
	}

	WriteJSON(w, http.StatusCreated, refund)
}

func (h *RefundHandler) GetRefund(w http.ResponseWriter, r *http.Request) {
	merchant, err := MerchantFromContext(r.Context())
	if err != nil {
		MapDomainError(w, err)
		return
	}

	rID, err := uuid.Parse(r.PathValue("refund_id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "invalid_id", "Refund ID must be a valid UUID.")
		return
	}

	refund, err := h.refundService.GetRefund(r.Context(), merchant.ID, rID)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, refund)
}
