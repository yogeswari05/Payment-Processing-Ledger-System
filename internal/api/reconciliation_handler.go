package api

import (
	"net/http"

	"github.com/google/uuid"
	"github.com/payflow/payflow/internal/service"
)

type ReconciliationHandler struct {
	reconService *service.ReconciliationService
}

func NewReconciliationHandler(rs *service.ReconciliationService) *ReconciliationHandler {
	return &ReconciliationHandler{reconService: rs}
}

func (h *ReconciliationHandler) TriggerRun(w http.ResponseWriter, r *http.Request) {
	run, err := h.reconService.RunAudit(r.Context())
	if err != nil {
		MapDomainError(w, err)
		return
	}

	WriteJSON(w, http.StatusCreated, run)
}

func (h *ReconciliationHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	runID, err := uuid.Parse(r.PathValue("run_id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "invalid_id", "Run ID must be a valid UUID.")
		return
	}

	run, err := h.reconService.GetRun(r.Context(), runID)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, run)
}
