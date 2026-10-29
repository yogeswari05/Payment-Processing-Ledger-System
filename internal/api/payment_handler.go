package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
	"github.com/payflow/payflow/internal/service"
)

type PaymentHandler struct {
	paymentService *service.PaymentService
}

func NewPaymentHandler(ps *service.PaymentService) *PaymentHandler {
	return &PaymentHandler{paymentService: ps}
}

type CreatePaymentRequest struct {
	CustomerID  string          `json:"customer_id"`
	Amount      int64           `json:"amount"`
	Currency    string          `json:"currency"`
	Description string          `json:"description"`
	Metadata    json.RawMessage `json:"metadata"`
}

func (h *PaymentHandler) CreatePayment(w http.ResponseWriter, r *http.Request) {
	merchant, err := MerchantFromContext(r.Context())
	if err != nil {
		MapDomainError(w, err)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "read_error", "Failed reading request body.")
		return
	}

	var req CreatePaymentRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "malformed_json", "Invalid JSON payload.")
		return
	}

	customerID, err := uuid.Parse(req.CustomerID)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "invalid_customer_id", "customer_id must be a valid UUID.")
		return
	}

	idemKey := r.Header.Get("Idempotency-Key")
	reqHash := domain.ComputeRequestHash(r.Method, r.URL.Path, bodyBytes)

	payment, isReplay, err := h.paymentService.CreatePayment(r.Context(), service.CreatePaymentParams{
		MerchantID:     merchant.ID,
		CustomerID:     customerID,
		Amount:         req.Amount,
		Currency:       req.Currency,
		Description:    req.Description,
		Metadata:       req.Metadata,
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

	WriteJSON(w, http.StatusCreated, payment)
}

func (h *PaymentHandler) GetPayment(w http.ResponseWriter, r *http.Request) {
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

	payment, err := h.paymentService.GetPayment(r.Context(), merchant.ID, pID)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, payment)
}

func (h *PaymentHandler) ListPayments(w http.ResponseWriter, r *http.Request) {
	merchant, err := MerchantFromContext(r.Context())
	if err != nil {
		MapDomainError(w, err)
		return
	}

	q := r.URL.Query()
	limit := 20
	if l := q.Get("limit"); l != "" {
		if parsedLimit, err := strconv.Atoi(l); err == nil && parsedLimit > 0 && parsedLimit <= 100 {
			limit = parsedLimit
		}
	}

	filter := repository.ListPaymentsFilter{
		MerchantID: merchant.ID,
		Limit:      limit,
	}

	if custIDStr := q.Get("customer_id"); custIDStr != "" {
		if cid, err := uuid.Parse(custIDStr); err == nil {
			filter.CustomerID = &cid
		}
	}

	if statusStr := q.Get("status"); statusStr != "" {
		st := domain.PaymentStatus(statusStr)
		filter.Status = &st
	}

	if cursorStr := q.Get("starting_after"); cursorStr != "" {
		if t, err := time.Parse(time.RFC3339Nano, cursorStr); err == nil {
			filter.Cursor = &t
		}
	}

	payments, nextCursor, err := h.paymentService.ListPayments(r.Context(), filter)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, PaginatedList[domain.Payment]{
		Data:       payments,
		HasMore:    nextCursor != nil,
		NextCursor: FormatTimeCursor(nextCursor),
	})
}

func (h *PaymentHandler) ConfirmPayment(w http.ResponseWriter, r *http.Request) {
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

	bodyBytes, _ := io.ReadAll(r.Body)
	idemKey := r.Header.Get("Idempotency-Key")
	reqHash := domain.ComputeRequestHash(r.Method, r.URL.Path, bodyBytes)

	payment, isReplay, err := h.paymentService.ConfirmPayment(r.Context(), merchant.ID, pID, idemKey, reqHash)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	if isReplay {
		w.Header().Set("Idempotent-Replayed", "true")
	}

	WriteJSON(w, http.StatusOK, payment)
}

type CapturePaymentRequest struct {
	Amount *int64 `json:"amount,omitempty"`
}

func (h *PaymentHandler) CapturePayment(w http.ResponseWriter, r *http.Request) {
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

	bodyBytes, _ := io.ReadAll(r.Body)
	var req CapturePaymentRequest
	if len(bodyBytes) > 0 {
		_ = json.Unmarshal(bodyBytes, &req)
	}

	idemKey := r.Header.Get("Idempotency-Key")
	reqHash := domain.ComputeRequestHash(r.Method, r.URL.Path, bodyBytes)

	payment, isReplay, err := h.paymentService.CapturePayment(r.Context(), merchant.ID, pID, req.Amount, idemKey, reqHash)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	if isReplay {
		w.Header().Set("Idempotent-Replayed", "true")
	}

	WriteJSON(w, http.StatusOK, payment)
}

func (h *PaymentHandler) CancelPayment(w http.ResponseWriter, r *http.Request) {
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

	bodyBytes, _ := io.ReadAll(r.Body)
	idemKey := r.Header.Get("Idempotency-Key")
	reqHash := domain.ComputeRequestHash(r.Method, r.URL.Path, bodyBytes)

	payment, isReplay, err := h.paymentService.CancelPayment(r.Context(), merchant.ID, pID, idemKey, reqHash)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	if isReplay {
		w.Header().Set("Idempotent-Replayed", "true")
	}

	WriteJSON(w, http.StatusOK, payment)
}
