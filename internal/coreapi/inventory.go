package coreapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"core/internal/inventory"
	"core/internal/serviceauth"
	"core/packages/httpx"
)

type StoreInventoryDualModeAdjustmentRequest struct {
	FulfillmentLocationID string `json:"fulfillment_location_id"`
	SKUID                 string `json:"sku_id"`
	QtyDelta              *int64 `json:"qty_delta,omitempty"`
	TargetQty             *int64 `json:"target_qty,omitempty"`
	ReasonCode            string `json:"reason_code"`
	Note                  string `json:"note,omitempty"`
}

func (s *server) handleAdjustStoreInventoryDualMode(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	if storeID == "" {
		writeError(w, CodeValidationError)
		return
	}
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}

	var req StoreInventoryDualModeAdjustmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	correlationID := httpx.CorrelationID(r.Context())
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		idempotencyKey = r.Header.Get("X-Idempotency-Key")
	}

	if s.deps.Inventory == nil {
		writeError(w, CodeUnavailable)
		return
	}

	res, err := s.deps.Inventory.AdjustStoreInventory(r.Context(), inventory.AdjustParams{
		StoreID:        storeID,
		LocationID:     req.FulfillmentLocationID,
		SKUID:          req.SKUID,
		QtyDelta:       req.QtyDelta,
		TargetQty:      req.TargetQty,
		ReasonCode:     req.ReasonCode,
		Note:           req.Note,
		Subject:        subject,
		CorrelationID:  correlationID,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		switch {
		case errors.Is(err, inventory.ErrNotFound):
			writeError(w, CodeNotFound)
		case errors.Is(err, inventory.ErrInvalidInput):
			writeError(w, CodeInvalidArgument)
		case errors.Is(err, inventory.ErrInsufficientInventory):
			writeError(w, CodeInsufficientInventory)
		case errors.Is(err, inventory.ErrIdempotencyConflict):
			writeError(w, CodeIdempotencyConflict)
		default:
			writeDomainError(w, err)
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, res)
}
