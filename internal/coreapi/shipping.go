package coreapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/internal/shipping"
	"github.com/matjeroapps/core/packages/httpx"
)

func (s *server) handleCreateOrderShipment(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "orderID")
	if orderID == "" {
		writeError(w, CodeValidationError)
		return
	}

	var req CreateShipmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	items := make([]shipping.CreateShipmentItemParams, 0, len(req.Items))
	for _, item := range req.Items {
		items = append(items, shipping.CreateShipmentItemParams{
			OrderItemID: item.OrderItemID,
			Quantity:    item.Quantity,
		})
	}

	params := shipping.CreateShipmentParams{
		OrderID:               orderID,
		FulfillmentLocationID: req.FulfillmentLocationID,
		TrackingNumber:        req.TrackingNumber,
		ShippingCostMinor:     req.ShippingCostMinor,
		CodAmountMinor:        req.CodAmountMinor,
		Currency:              req.Currency,
		Items:                 items,
		CorrelationID:         r.Header.Get("X-Correlation-ID"),
		CausationID:           serviceauth.SubjectFrom(r),
	}

	sh, err := s.deps.Shipping.CreateShipment(r.Context(), params)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toShipmentResponse(sh))
}

func (s *server) handleUpdateShipmentStatus(w http.ResponseWriter, r *http.Request) {
	shipmentID := chi.URLParam(r, "shipmentID")
	if shipmentID == "" {
		writeError(w, CodeValidationError)
		return
	}

	var req UpdateShipmentStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	params := shipping.UpdateStatusParams{
		ShipmentID:     shipmentID,
		NewStatus:      shipping.Status(req.Status),
		TrackingNumber: req.TrackingNumber,
		Notes:          req.Notes,
		CorrelationID:  r.Header.Get("X-Correlation-ID"),
		CausationID:    serviceauth.SubjectFrom(r),
	}

	sh, err := s.deps.Shipping.UpdateShipmentStatus(r.Context(), params)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toShipmentResponse(sh))
}

func (s *server) handleGetShipment(w http.ResponseWriter, r *http.Request) {
	shipmentID := chi.URLParam(r, "shipmentID")
	if shipmentID == "" {
		writeError(w, CodeValidationError)
		return
	}

	sh, err := s.deps.Shipping.GetShipment(r.Context(), shipmentID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toShipmentResponse(sh))
}

func toShipmentResponse(sh *shipping.Shipment) ShipmentResponse {
	items := make([]ShipmentItemResponse, 0, len(sh.Items))
	for _, item := range sh.Items {
		items = append(items, ShipmentItemResponse{
			ID:          item.ID,
			ShipmentID:  item.ShipmentID,
			OrderItemID: item.OrderItemID,
			Quantity:    item.Quantity,
			CreatedAt:   item.CreatedAt,
		})
	}

	events := make([]ShipmentEventResponse, 0, len(sh.Events))
	for _, ev := range sh.Events {
		events = append(events, ShipmentEventResponse{
			ID:         ev.ID,
			ShipmentID: ev.ShipmentID,
			Status:     string(ev.Status),
			Notes:      ev.Notes,
			OccurredAt: ev.OccurredAt,
		})
	}

	return ShipmentResponse{
		ID:                    sh.ID,
		OrderID:               sh.OrderID,
		FulfillmentLocationID: sh.FulfillmentLocationID,
		Status:                string(sh.Status),
		TrackingNumber:        sh.TrackingNumber,
		ShippingCostMinor:     sh.ShippingCostMinor,
		CodAmountMinor:        sh.CodAmountMinor,
		Currency:              sh.Currency,
		Items:                 items,
		Events:                events,
		CreatedAt:             sh.CreatedAt,
		UpdatedAt:             sh.UpdatedAt,
	}
}
