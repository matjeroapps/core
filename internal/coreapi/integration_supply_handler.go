package coreapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"core/internal/integration"
	"core/packages/httpx"
)

// IntegrationSupplyHandler serves the I2 Supply pipeline contracts.
//
// Two route groups use it:
//   - Hub-facing routes mounted under /integrations/supply: only the platform
//     Integration Hub service caller may reach them (router enforced).
//   - Merchant decision routes mounted under /merchants/{merchantID}/...:
//     Merchant membership/capability/permission authorization applies.
type IntegrationSupplyHandler struct {
	service integration.SupplyService
}

func NewIntegrationSupplyHandler(service integration.SupplyService) *IntegrationSupplyHandler {
	return &IntegrationSupplyHandler{service: service}
}

type createImportBatchHTTPRequest struct {
	MerchantID     uuid.UUID                       `json:"merchant_id"`
	ConnectionID   uuid.UUID                       `json:"connection_id"`
	BatchType      string                          `json:"batch_type,omitempty"`
	CursorToken    *string                         `json:"cursor_token,omitempty"`
	IdempotencyKey string                          `json:"idempotency_key"`
	Records        []integration.StagedRecordInput `json:"records"`
}

// CreateImportBatch handles POST /integrations/supply/import-batches.
func (h *IntegrationSupplyHandler) CreateImportBatch(w http.ResponseWriter, r *http.Request) {
	var req createImportBatchHTTPRequest
	if !decodeStrict(w, r, &req) {
		return
	}

	correlationID := httpx.CorrelationID(r.Context())
	causationID := httpx.RequestID(r.Context())

	batch, records, err := h.service.CreateImportBatch(r.Context(), integration.CreateSupplyImportBatchInput{
		MerchantID:     req.MerchantID,
		ConnectionID:   req.ConnectionID,
		BatchType:      req.BatchType,
		CursorToken:    req.CursorToken,
		IdempotencyKey: req.IdempotencyKey,
		Records:        req.Records,
	}, correlationID, causationID)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, map[string]any{
		"batch":   batch,
		"records": records,
	})
}

type approveImportBatchHTTPRequest struct {
	Decisions []integration.RecordDecision `json:"decisions"`
}

// ApproveImportBatch handles POST /merchants/{merchantID}/integrations/supply/import-batches/{id}/approval.
func (h *IntegrationSupplyHandler) ApproveImportBatch(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	batchID, ok := parseUUIDParam(w, r, "batchID")
	if !ok {
		return
	}
	var req approveImportBatchHTTPRequest
	if !decodeStrict(w, r, &req) {
		return
	}

	correlationID := httpx.CorrelationID(r.Context())
	causationID := httpx.RequestID(r.Context())

	batch, err := h.service.ApproveImportBatch(r.Context(), integration.ApproveSupplyImportBatchInput{
		BatchID:    batchID,
		MerchantID: merchantID,
		Decisions:  req.Decisions,
	}, correlationID, causationID)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, batch)
}

// GetImportBatch handles GET /integrations/supply/import-batches/{id}.
func (h *IntegrationSupplyHandler) GetImportBatch(w http.ResponseWriter, r *http.Request) {
	batchID, ok := parseUUIDParam(w, r, "batchID")
	if !ok {
		return
	}
	batch, records, err := h.service.GetImportBatch(r.Context(), batchID)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"batch": batch, "records": records})
}

// ListReviewCases handles GET /merchants/{merchantID}/integrations/supply/review-cases.
func (h *IntegrationSupplyHandler) ListReviewCases(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	var connectionID *uuid.UUID
	if v := r.URL.Query().Get("connection_id"); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			writeError(w, CodeInvalidArgument)
			return
		}
		connectionID = &id
	}
	status := r.URL.Query().Get("status")
	cases, err := h.service.ListReviewCases(r.Context(), merchantID, connectionID, status)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	if cases == nil {
		cases = []integration.MerchantReviewCase{}
	}
	httpx.WriteJSON(w, http.StatusOK, cases)
}

type resolveReviewCaseHTTPRequest struct {
	Resolution string `json:"resolution"`
}

// ResolveReviewCase handles POST /merchants/{merchantID}/integrations/supply/review-cases/{caseID}/resolution.
func (h *IntegrationSupplyHandler) ResolveReviewCase(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	caseID, ok := parseUUIDParam(w, r, "caseID")
	if !ok {
		return
	}
	var req resolveReviewCaseHTTPRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	resolvedBy := subjectFromContext(r)
	c, err := h.service.ResolveReviewCase(r.Context(), caseID, merchantID, req.Resolution, resolvedBy)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

type upsertCursorHTTPRequest struct {
	ConnectionID uuid.UUID `json:"connection_id"`
	EntityType   string    `json:"entity_type"`
	CursorToken  *string   `json:"cursor_token,omitempty"`
}

// UpsertSyncCursor handles PUT /integrations/supply/cursors.
func (h *IntegrationSupplyHandler) UpsertSyncCursor(w http.ResponseWriter, r *http.Request) {
	var req upsertCursorHTTPRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	cursor, err := h.service.UpsertSyncCursor(r.Context(), integration.MerchantSyncCursor{
		ConnectionID: req.ConnectionID,
		EntityType:   req.EntityType,
		CursorToken:  req.CursorToken,
	})
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, cursor)
}

// GetSyncCursor handles GET /integrations/supply/cursors.
func (h *IntegrationSupplyHandler) GetSyncCursor(w http.ResponseWriter, r *http.Request) {
	connectionID, ok := parseUUIDParam(w, r, "connectionID")
	if !ok {
		return
	}
	entityType := r.URL.Query().Get("entity_type")
	if entityType == "" {
		writeError(w, CodeInvalidArgument)
		return
	}
	cursor, err := h.service.GetSyncCursor(r.Context(), connectionID, entityType)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	if cursor == nil {
		writeError(w, CodeNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, cursor)
}

// GetMapping handles GET /integrations/supply/mappings/single.
func (h *IntegrationSupplyHandler) GetMapping(w http.ResponseWriter, r *http.Request) {
	connectionID, ok := parseUUIDParam(w, r, "connectionID")
	if !ok {
		return
	}
	entityType := r.URL.Query().Get("entity_type")
	externalProductID := r.URL.Query().Get("external_product_id")
	if entityType == "" || externalProductID == "" {
		writeError(w, CodeInvalidArgument)
		return
	}
	var externalVariantID *string
	if v := r.URL.Query().Get("external_variant_id"); v != "" {
		externalVariantID = &v
	}
	mapping, err := h.service.GetMappingByExternalID(r.Context(), connectionID, entityType, externalProductID, externalVariantID)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	if mapping == nil {
		writeError(w, CodeNotFound)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, mapping)
}

// ListMappings handles GET /integrations/supply/mappings.
func (h *IntegrationSupplyHandler) ListMappings(w http.ResponseWriter, r *http.Request) {
	connectionID, ok := parseUUIDParam(w, r, "connectionID")
	if !ok {
		return
	}
	mappings, err := h.service.ListMappings(r.Context(), connectionID)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	if mappings == nil {
		mappings = []integration.MerchantEntityMapping{}
	}
	httpx.WriteJSON(w, http.StatusOK, mappings)
}

type createFulfillmentRequestHTTPRequest struct {
	MerchantID     uuid.UUID       `json:"merchant_id"`
	ConnectionID   uuid.UUID       `json:"connection_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload,omitempty"`
}

// CreateFulfillmentRequest handles POST /integrations/supply/fulfillment-requests.
func (h *IntegrationSupplyHandler) CreateFulfillmentRequest(w http.ResponseWriter, r *http.Request) {
	var req createFulfillmentRequestHTTPRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	correlationID := httpx.CorrelationID(r.Context())
	causationID := httpx.RequestID(r.Context())

	reqResult, err := h.service.CreateFulfillmentRequest(r.Context(), integration.CreateFulfillmentRequestInput{
		MerchantID:     req.MerchantID,
		ConnectionID:   req.ConnectionID,
		IdempotencyKey: req.IdempotencyKey,
		Payload:        req.Payload,
		CorrelationID:  correlationID,
		CausationID:    causationID,
	})
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, reqResult)
}

// GetFulfillmentRequest handles GET /integrations/supply/fulfillment-requests/{requestID}.
func (h *IntegrationSupplyHandler) GetFulfillmentRequest(w http.ResponseWriter, r *http.Request) {
	requestID, ok := parseUUIDParam(w, r, "requestID")
	if !ok {
		return
	}
	reqResult, err := h.service.GetFulfillmentRequest(r.Context(), requestID)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, reqResult)
}

type updateFulfillmentStatusHTTPRequest struct {
	Status                string  `json:"status"`
	ExternalFulfillmentID *string `json:"external_fulfillment_id,omitempty"`
}

// UpdateFulfillmentRequestStatus handles PATCH /integrations/supply/fulfillment-requests/{requestID}/status.
func (h *IntegrationSupplyHandler) UpdateFulfillmentRequestStatus(w http.ResponseWriter, r *http.Request) {
	requestID, ok := parseUUIDParam(w, r, "requestID")
	if !ok {
		return
	}
	var req updateFulfillmentStatusHTTPRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	reqResult, err := h.service.UpdateFulfillmentRequestStatus(r.Context(), requestID, req.Status, req.ExternalFulfillmentID)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, reqResult)
}

type recordTrackingEventHTTPRequest struct {
	RequestID       uuid.UUID       `json:"request_id"`
	ConnectionID    uuid.UUID       `json:"connection_id"`
	ExternalEventID string          `json:"external_event_id"`
	Status          string          `json:"status"`
	Carrier         *string         `json:"carrier,omitempty"`
	TrackingNumber  *string         `json:"tracking_number,omitempty"`
	OccurredAt      *int64          `json:"occurred_at,omitempty"`
	Payload         json.RawMessage `json:"payload,omitempty"`
}

// RecordTrackingEvent handles POST /integrations/supply/tracking-events.
func (h *IntegrationSupplyHandler) RecordTrackingEvent(w http.ResponseWriter, r *http.Request) {
	var req recordTrackingEventHTTPRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	input := integration.RecordTrackingEventInput{
		RequestID:       req.RequestID,
		ConnectionID:    req.ConnectionID,
		ExternalEventID: req.ExternalEventID,
		Status:          req.Status,
		Carrier:         req.Carrier,
		TrackingNumber:  req.TrackingNumber,
		Payload:         req.Payload,
	}
	if req.OccurredAt != nil {
		t := unixMillisToTime(*req.OccurredAt)
		input.OccurredAt = &t
	}
	correlationID := httpx.CorrelationID(r.Context())
	causationID := httpx.RequestID(r.Context())

	ev, isNew, err := h.service.RecordTrackingEvent(r.Context(), input, correlationID, causationID)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, statusForisNew(isNew), map[string]any{"event": ev, "new": isNew})
}

type recordWebhookInboxHTTPRequest struct {
	ConnectionID   *uuid.UUID      `json:"connection_id,omitempty"`
	Provider       string          `json:"provider"`
	EventType      string          `json:"event_type"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload,omitempty"`
}

// RecordWebhookInbox handles POST /integrations/supply/webhook-inbox.
func (h *IntegrationSupplyHandler) RecordWebhookInbox(w http.ResponseWriter, r *http.Request) {
	var req recordWebhookInboxHTTPRequest
	if !decodeStrict(w, r, &req) {
		return
	}
	item, isNew, err := h.service.RecordWebhookInbox(r.Context(), integration.RecordWebhookInboxInput{
		ConnectionID:   req.ConnectionID,
		Provider:       integration.Provider(req.Provider),
		EventType:      req.EventType,
		IdempotencyKey: req.IdempotencyKey,
		Payload:        req.Payload,
	})
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, statusForisNew(isNew), map[string]any{"item": item, "new": isNew})
}

func statusForisNew(isNew bool) int {
	if isNew {
		return http.StatusCreated
	}
	return http.StatusOK
}

func writeSupplyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, integration.ErrConnectionNotFound), errors.Is(err, integration.ErrBatchNotFound),
		errors.Is(err, integration.ErrReviewCaseNotFound), errors.Is(err, integration.ErrFulfillmentNotFound):
		writeError(w, CodeNotFound)
	case errors.Is(err, integration.ErrConnectionNotSupplySource), errors.Is(err, integration.ErrConnectionNotActive),
		errors.Is(err, integration.ErrBatchNotReviewable), errors.Is(err, integration.ErrRecordInDuplicateReview),
		errors.Is(err, integration.ErrInvalidMappingDecision), errors.Is(err, integration.ErrMappingNotFound):
		writeError(w, CodeUnprocessableEntity)
	default:
		writeError(w, CodeInternalError)
	}
}

func decodeStrict(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		writeError(w, CodeInvalidArgument)
		return false
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, CodeInvalidArgument)
		return false
	}
	return true
}

func unixMillisToTime(ms int64) time.Time {
	return time.UnixMilli(ms)
}
