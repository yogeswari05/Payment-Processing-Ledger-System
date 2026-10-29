package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/service"
)

type MerchantHandler struct {
	merchantService *service.MerchantService
}

func NewMerchantHandler(ms *service.MerchantService) *MerchantHandler {
	return &MerchantHandler{merchantService: ms}
}

type CreateMerchantRequest struct {
	Name string `json:"name"`
}

type CreateMerchantResponse struct {
	ID        uuid.UUID             `json:"id"`
	Name      string                `json:"name"`
	Status    domain.MerchantStatus `json:"status"`
	APIKey    string                `json:"api_key"`
	CreatedAt string                `json:"created_at"`
}

func (h *MerchantHandler) CreateMerchant(w http.ResponseWriter, r *http.Request) {
	var req CreateMerchantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "malformed_json", "Invalid JSON payload.")
		return
	}

	merchant, rawKey, err := h.merchantService.CreateMerchant(r.Context(), req.Name)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	WriteJSON(w, http.StatusCreated, CreateMerchantResponse{
		ID:        merchant.ID,
		Name:      merchant.Name,
		Status:    merchant.Status,
		APIKey:    rawKey,
		CreatedAt: merchant.CreatedAt.Format(http.TimeFormat),
	})
}

type CreateCustomerRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (h *MerchantHandler) CreateCustomer(w http.ResponseWriter, r *http.Request) {
	merchant, err := MerchantFromContext(r.Context())
	if err != nil {
		MapDomainError(w, err)
		return
	}

	var req CreateCustomerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "malformed_json", "Invalid JSON payload.")
		return
	}

	customer, err := h.merchantService.CreateCustomer(r.Context(), merchant.ID, req.Email, req.Name)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	WriteJSON(w, http.StatusCreated, customer)
}

func (h *MerchantHandler) GetCustomer(w http.ResponseWriter, r *http.Request) {
	merchant, err := MerchantFromContext(r.Context())
	if err != nil {
		MapDomainError(w, err)
		return
	}

	customerIDStr := r.PathValue("customer_id")
	customerID, err := uuid.Parse(customerIDStr)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "invalid_id", "Customer ID must be a valid UUID.")
		return
	}

	customer, err := h.merchantService.GetCustomer(r.Context(), merchant.ID, customerID)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, customer)
}
