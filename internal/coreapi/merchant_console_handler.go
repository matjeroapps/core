package coreapi

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"core/internal/integration"
	"core/internal/merchants"
	"core/internal/serviceauth"
	"core/packages/httpx"
)

// MerchantConsoleHandler serves the subject-oriented Merchant bootstrap. It is
// mounted behind service auth for the seller and supplier service callers only;
// the subject always arrives through the trusted forwarded-subject boundary
// (X-Matjero-Subject, set by the actor service from its verified principal).
// The browser never calls Core.
type MerchantConsoleHandler struct {
	bootstrap *merchants.BootstrapService
}

func NewMerchantConsoleHandler(bootstrap *merchants.BootstrapService) *MerchantConsoleHandler {
	return &MerchantConsoleHandler{bootstrap: bootstrap}
}

// GetBootstrap handles GET /internal/v1/merchants/bootstrap.
func (h *MerchantConsoleHandler) GetBootstrap(w http.ResponseWriter, r *http.Request) {
	if h.bootstrap == nil {
		writeError(w, CodeUnavailable)
		return
	}
	subject := serviceauth.SubjectFrom(r)
	bs, err := h.bootstrap.Build(r.Context(), subject)
	if err != nil {
		writeError(w, CodeInternalError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, bs)
}

// MerchantSupplyReadHandler serves the Merchant-authorized Supply read
// surfaces under /merchants/{merchantID}/integrations/supply/.... Every
// request enforces: active Merchant, active membership, active SUPPLY
// capability, the required Supply permission, and Merchant ownership of every
// returned row (a foreign ID is indistinguishable from a missing one).
type MerchantSupplyReadHandler struct {
	service    integration.SupplyService
	authorizer *merchants.Authorizer
	delegate   merchantSupplyDelegate
}

// merchantSupplyDelegate carries the underlying decision handlers so the
// authorized wrappers reuse their exact request/response contracts.
type merchantSupplyDelegate struct {
	IntegrationSupplyHandler *IntegrationSupplyHandler
}

func NewMerchantSupplyReadHandler(service integration.SupplyService, authorizer *merchants.Authorizer) *MerchantSupplyReadHandler {
	return &MerchantSupplyReadHandler{
		service:    service,
		authorizer: authorizer,
		delegate:   merchantSupplyDelegate{IntegrationSupplyHandler: NewIntegrationSupplyHandler(service)},
	}
}

// supplyPermissionForSelects the permission that governs a supply read group.
// Catalog-domain reads (batches, review cases, mappings, cursors) are governed
// by supply.catalog.manage; fulfillment/tracking reads by supply.fulfillment.manage.
const (
	supplyReadPermissionCatalog     = merchants.PermissionSupplyCatalogManage
	supplyReadPermissionFulfillment = merchants.PermissionSupplyFulfillment
)

// ImportBatchDetailResponse is the batch detail payload (batch + staged records).
type ImportBatchDetailResponse struct {
	Batch   *integration.SupplyImportBatch   `json:"batch"`
	Records []integration.SupplyImportRecord `json:"records"`
}

// FulfillmentRequestDetailResponse is the fulfillment detail payload (request
// + imported tracking events).
type FulfillmentRequestDetailResponse struct {
	Request        *integration.MerchantSupplyFulfillmentRequest `json:"request"`
	TrackingEvents []integration.MerchantSupplyTrackingEvent     `json:"tracking_events"`
}

// supplyAvailable fails closed when the supply capability is not wired.
func (h *MerchantSupplyReadHandler) supplyAvailable(w http.ResponseWriter) bool {
	if h.service == nil {
		writeError(w, CodeUnavailable)
		return false
	}
	return true
}

// authorizeSupply enforces the merchant-scoped authorization chain for one
// supply read. It returns false and writes the response on denial.
func (h *MerchantSupplyReadHandler) authorizeSupply(w http.ResponseWriter, r *http.Request, merchantID uuid.UUID, permission string) bool {
	if h.authorizer == nil {
		return true
	}
	subject := subjectFromContext(r)
	if err := h.authorizer.Authorize(r.Context(), merchantID, subject, merchants.CapabilityTypeSupply, permission); err != nil {
		if errors.Is(err, merchants.ErrMerchantNotFound) {
			writeError(w, CodeNotFound)
			return false
		}
		writeError(w, CodeForbidden)
		return false
	}
	return true
}

func supplyConnectionFilter(w http.ResponseWriter, r *http.Request) *uuid.UUID {
	raw := r.URL.Query().Get("connection_id")
	if raw == "" {
		return nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		writeError(w, CodeInvalidArgument)
		return nil
	}
	return &id
}

func supplyPage(r *http.Request) integration.SupplyListPage {
	limit, offset := 0, 0
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		offset = (p - 1) * defaultSupplyQueryLimit
	}
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 {
		limit = l
	}
	return integration.NormalizeSupplyListPage(limit, offset)
}

const defaultSupplyQueryLimit = 20

// ListImportBatches handles GET .../supply/import-batches.
func (h *MerchantSupplyReadHandler) ListImportBatches(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	if !h.authorizeSupply(w, r, merchantID, supplyReadPermissionCatalog) {
		return
	}
	if !h.supplyAvailable(w) {
		return
	}
	connectionID := supplyConnectionFilter(w, r)
	if connectionID == nil && r.URL.Query().Get("connection_id") != "" {
		return
	}
	batches, err := h.service.ListImportBatchesForMerchant(r.Context(), merchantID, connectionID, r.URL.Query().Get("status"), supplyPage(r))
	if err != nil {
		writeError(w, CodeInternalError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, batches)
}

// GetImportBatch handles GET .../supply/import-batches/{batchID}.
func (h *MerchantSupplyReadHandler) GetImportBatch(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	batchID, ok := parseUUIDParam(w, r, "batchID")
	if !ok {
		return
	}
	if !h.authorizeSupply(w, r, merchantID, supplyReadPermissionCatalog) {
		return
	}
	if !h.supplyAvailable(w) {
		return
	}
	batch, records, err := h.service.GetImportBatchForMerchant(r.Context(), merchantID, batchID)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, ImportBatchDetailResponse{Batch: batch, Records: records})
}

// GetReviewCase handles GET .../supply/review-cases/{caseID}.
func (h *MerchantSupplyReadHandler) GetReviewCase(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	caseID, ok := parseUUIDParam(w, r, "caseID")
	if !ok {
		return
	}
	if !h.authorizeSupply(w, r, merchantID, supplyReadPermissionCatalog) {
		return
	}
	if !h.supplyAvailable(w) {
		return
	}
	c, err := h.service.GetReviewCaseForMerchant(r.Context(), merchantID, caseID)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, c)
}

// ListMappings handles GET .../supply/mappings.
func (h *MerchantSupplyReadHandler) ListMappings(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	if !h.authorizeSupply(w, r, merchantID, supplyReadPermissionCatalog) {
		return
	}
	connectionID := supplyConnectionFilter(w, r)
	if connectionID == nil && r.URL.Query().Get("connection_id") != "" {
		return
	}
	if !h.supplyAvailable(w) {
		return
	}
	mappings, err := h.service.ListMappingsForMerchant(r.Context(), merchantID, connectionID)
	if err != nil {
		writeError(w, CodeInternalError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, mappings)
}

// GetMapping handles GET .../supply/mappings/{mappingID}.
func (h *MerchantSupplyReadHandler) GetMapping(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	mappingID, ok := parseUUIDParam(w, r, "mappingID")
	if !ok {
		return
	}
	if !h.authorizeSupply(w, r, merchantID, supplyReadPermissionCatalog) {
		return
	}
	if !h.supplyAvailable(w) {
		return
	}
	mapping, err := h.service.GetMappingForMerchant(r.Context(), merchantID, mappingID)
	if err != nil {
		if errors.Is(err, integration.ErrMappingNotFound) {
			writeError(w, CodeNotFound)
			return
		}
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, mapping)
}

// ListSyncCursors handles GET .../supply/cursors.
func (h *MerchantSupplyReadHandler) ListSyncCursors(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	if !h.authorizeSupply(w, r, merchantID, supplyReadPermissionCatalog) {
		return
	}
	connectionID := supplyConnectionFilter(w, r)
	if connectionID == nil && r.URL.Query().Get("connection_id") != "" {
		return
	}
	if !h.supplyAvailable(w) {
		return
	}
	cursors, err := h.service.ListSyncCursorsForMerchant(r.Context(), merchantID, connectionID)
	if err != nil {
		writeError(w, CodeInternalError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, cursors)
}

// ListFulfillmentRequests handles GET .../supply/fulfillment-requests.
func (h *MerchantSupplyReadHandler) ListFulfillmentRequests(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	if !h.authorizeSupply(w, r, merchantID, supplyReadPermissionFulfillment) {
		return
	}
	connectionID := supplyConnectionFilter(w, r)
	if connectionID == nil && r.URL.Query().Get("connection_id") != "" {
		return
	}
	if !h.supplyAvailable(w) {
		return
	}
	requests, err := h.service.ListFulfillmentRequestsForMerchant(r.Context(), merchantID, connectionID, r.URL.Query().Get("status"), supplyPage(r))
	if err != nil {
		writeError(w, CodeInternalError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, requests)
}

// GetFulfillmentRequest handles GET .../supply/fulfillment-requests/{requestID}.
func (h *MerchantSupplyReadHandler) GetFulfillmentRequest(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	requestID, ok := parseUUIDParam(w, r, "requestID")
	if !ok {
		return
	}
	if !h.authorizeSupply(w, r, merchantID, supplyReadPermissionFulfillment) {
		return
	}
	if !h.supplyAvailable(w) {
		return
	}
	request, events, err := h.service.GetFulfillmentRequestForMerchant(r.Context(), merchantID, requestID)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, FulfillmentRequestDetailResponse{Request: request, TrackingEvents: events})
}

// ApproveImportBatch handles POST .../supply/import-batches/{batchID}/approval
// with the same merchant authorization chain as the read surfaces.
func (h *MerchantSupplyReadHandler) ApproveImportBatch(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	if !h.supplyAvailable(w) {
		return
	}
	if !h.authorizeSupply(w, r, merchantID, supplyReadPermissionCatalog) {
		return
	}
	h.delegate.IntegrationSupplyHandler.ApproveImportBatch(w, r)
}

// ResolveReviewCase handles POST .../supply/review-cases/{caseID}/resolution
// with the same merchant authorization chain as the read surfaces.
func (h *MerchantSupplyReadHandler) ResolveReviewCase(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	if !h.supplyAvailable(w) {
		return
	}
	if !h.authorizeSupply(w, r, merchantID, supplyReadPermissionCatalog) {
		return
	}
	h.delegate.IntegrationSupplyHandler.ResolveReviewCase(w, r)
}

// ListReviewCases handles GET .../supply/review-cases with the merchant
// authorization chain. The unmerchant-scoped variant no longer exists: the
// review queue is always resolved for the authorized Merchant.
func (h *MerchantSupplyReadHandler) ListReviewCases(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	if !h.supplyAvailable(w) {
		return
	}
	if !h.authorizeSupply(w, r, merchantID, supplyReadPermissionCatalog) {
		return
	}
	h.delegate.IntegrationSupplyHandler.ListReviewCases(w, r)
}

// ListTrackingEvents handles GET .../supply/fulfillment-requests/{requestID}/tracking-events.
func (h *MerchantSupplyReadHandler) ListTrackingEvents(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	requestID, ok := parseUUIDParam(w, r, "requestID")
	if !ok {
		return
	}
	if !h.authorizeSupply(w, r, merchantID, supplyReadPermissionFulfillment) {
		return
	}
	if !h.supplyAvailable(w) {
		return
	}
	events, err := h.service.ListTrackingEventsForMerchant(r.Context(), merchantID, requestID)
	if err != nil {
		writeSupplyError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, events)
}
