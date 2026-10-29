package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/payflow/payflow/internal/domain"
	"github.com/payflow/payflow/internal/repository"
	"github.com/payflow/payflow/internal/service"
)

type LedgerHandler struct {
	pool          *pgxpool.Pool
	ledgerService *service.LedgerService
}

func NewLedgerHandler(pool *pgxpool.Pool, ls *service.LedgerService) *LedgerHandler {
	return &LedgerHandler{
		pool:          pool,
		ledgerService: ls,
	}
}

func (h *LedgerHandler) ListAccounts(w http.ResponseWriter, r *http.Request) {
	merchant, err := MerchantFromContext(r.Context())
	if err != nil {
		MapDomainError(w, err)
		return
	}

	accounts, err := h.ledgerService.ListAccounts(r.Context(), h.pool, &merchant.ID)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, accounts)
}

func (h *LedgerHandler) GetTransaction(w http.ResponseWriter, r *http.Request) {
	// Authenticate merchant
	if _, err := MerchantFromContext(r.Context()); err != nil {
		MapDomainError(w, err)
		return
	}

	txID, err := uuid.Parse(r.PathValue("transaction_id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request_error", "invalid_id", "Transaction ID must be a valid UUID.")
		return
	}

	tx, err := h.ledgerService.GetTransaction(r.Context(), h.pool, txID)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, tx)
}

func (h *LedgerHandler) ListEntries(w http.ResponseWriter, r *http.Request) {
	if _, err := MerchantFromContext(r.Context()); err != nil {
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

	filter := repository.ListEntriesFilter{
		Limit: limit,
	}

	if accIDStr := q.Get("account_id"); accIDStr != "" {
		if aid, err := uuid.Parse(accIDStr); err == nil {
			filter.AccountID = &aid
		}
	}

	if txIDStr := q.Get("transaction_id"); txIDStr != "" {
		if tid, err := uuid.Parse(txIDStr); err == nil {
			filter.TransactionID = &tid
		}
	}

	if cursorStr := q.Get("starting_after"); cursorStr != "" {
		if t, err := time.Parse(time.RFC3339Nano, cursorStr); err == nil {
			filter.Cursor = &t
		}
	}

	entries, nextCursor, err := h.ledgerService.ListEntries(r.Context(), h.pool, filter)
	if err != nil {
		MapDomainError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, PaginatedList[domain.LedgerEntry]{
		Data:       entries,
		HasMore:    nextCursor != nil,
		NextCursor: FormatTimeCursor(nextCursor),
	})
}
