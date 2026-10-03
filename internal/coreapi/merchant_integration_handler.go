package coreapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"core/internal/integration"
	"core/internal/merchants"
	"core/internal/serviceauth"
	"core/modules/commerce"
	"core/packages/httpx"
)

type MerchantIntegrationHandler struct {
	service    integration.MerchantService
	authorizer *merchants.Authorizer
}

func NewMerchantIntegrationHandler(service integration.MerchantService, authorizer *merchants.Authorizer) *MerchantIntegrationHandler {
	return &MerchantIntegrationHandler{
		service:    service,
		authorizer: authorizer,
	}
}

type CreateMerchantConnectionHTTPRequest struct {
	ConnectionType      integration.MerchantConnectionType `json:"connection_type"`
	Provider            integration.Provider               `json:"provider"`
	ExternalAccountID   *string                            `json:"external_account_id,omitempty"`
	Name                string                             `json:"name"`
	StoreID             *uuid.UUID                         `json:"store_id,omitempty"`
	CredentialsVaultRef string                             `json:"credentials_vault_ref,omitempty"`
	GrantedScopes       json.RawMessage                    `json:"granted_scopes,omitempty"`
	Settings            json.RawMessage                    `json:"settings,omitempty"`
}

type UpdateMerchantConnectionStatusHTTPRequest struct {
	Status integration.MerchantConnectionStatus `json:"status"`
}

func (h *MerchantIntegrationHandler) CreateConnection(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}

	var req CreateMerchantConnectionHTTPRequest
	if r.Body == nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	requiredCap := merchants.CapabilityTypeRetail
	requiredPerm := merchants.PermissionRetailStoresManage
	if req.ConnectionType == integration.ConnectionTypeSupplySource {
		requiredCap = merchants.CapabilityTypeSupply
		requiredPerm = merchants.PermissionSupplyCatalogManage
	}

	subject := subjectFromContext(r)
	if h.authorizer != nil {
		if err := h.authorizer.Authorize(r.Context(), merchantID, subject, requiredCap, requiredPerm); err != nil {
			if errors.Is(err, merchants.ErrMerchantNotFound) {
				writeError(w, CodeNotFound)
				return
			}
			writeError(w, CodeForbidden)
			return
		}
	}

	input := integration.CreateMerchantConnectionInput{
		MerchantID:          merchantID,
		ConnectionType:      req.ConnectionType,
		Provider:            req.Provider,
		ExternalAccountID:   req.ExternalAccountID,
		Name:                req.Name,
		StoreID:             req.StoreID,
		CredentialsVaultRef: req.CredentialsVaultRef,
		GrantedScopes:       req.GrantedScopes,
		Settings:            req.Settings,
	}

	correlationID := httpx.CorrelationID(r.Context())
	causationID := httpx.RequestID(r.Context())

	conn, err := h.service.CreateMerchantConnection(r.Context(), input, correlationID, causationID)
	if err != nil {
		if errors.Is(err, integration.ErrDuplicateAccountConnection) {
			writeError(w, CodeConflict)
			return
		}
		writeError(w, CodeValidationError)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, conn)
}

func (h *MerchantIntegrationHandler) GetConnection(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	connID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	subject := subjectFromContext(r)
	if h.authorizer != nil {
		if err := h.authorizer.Authorize(r.Context(), merchantID, subject, "", ""); err != nil {
			if errors.Is(err, merchants.ErrMerchantNotFound) {
				writeError(w, CodeNotFound)
				return
			}
			writeError(w, CodeForbidden)
			return
		}
	}

	conn, err := h.service.GetMerchantConnection(r.Context(), connID)
	if err != nil || conn.MerchantID != merchantID {
		writeError(w, CodeNotFound)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, conn)
}

func (h *MerchantIntegrationHandler) ListConnections(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}

	subject := subjectFromContext(r)
	if h.authorizer != nil {
		if err := h.authorizer.Authorize(r.Context(), merchantID, subject, "", ""); err != nil {
			if errors.Is(err, merchants.ErrMerchantNotFound) {
				writeError(w, CodeNotFound)
				return
			}
			writeError(w, CodeForbidden)
			return
		}
	}

	var connType *integration.MerchantConnectionType
	rawType := r.URL.Query().Get("connection_type")
	if rawType != "" {
		ct := integration.MerchantConnectionType(strings.ToUpper(rawType))
		connType = &ct
	}

	page := parsePageQuery(r)

	conns, err := h.service.ListMerchantConnections(r.Context(), merchantID, connType, page)
	if err != nil {
		writeError(w, CodeInternalError)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, conns)
}

func (h *MerchantIntegrationHandler) UpdateConnectionStatus(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := parseUUIDParam(w, r, "merchantID")
	if !ok {
		return
	}
	connID, ok := parseUUIDParam(w, r, "id")
	if !ok {
		return
	}

	var req UpdateMerchantConnectionStatusHTTPRequest
	if r.Body == nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}

	conn, err := h.service.GetMerchantConnection(r.Context(), connID)
	if err != nil || conn.MerchantID != merchantID {
		writeError(w, CodeNotFound)
		return
	}

	requiredCap := merchants.CapabilityTypeRetail
	requiredPerm := merchants.PermissionRetailStoresManage
	if conn.ConnectionType == integration.ConnectionTypeSupplySource {
		requiredCap = merchants.CapabilityTypeSupply
		requiredPerm = merchants.PermissionSupplyCatalogManage
	}

	subject := subjectFromContext(r)
	if h.authorizer != nil {
		if err := h.authorizer.Authorize(r.Context(), merchantID, subject, requiredCap, requiredPerm); err != nil {
			writeError(w, CodeForbidden)
			return
		}
	}

	correlationID := httpx.CorrelationID(r.Context())
	causationID := httpx.RequestID(r.Context())

	updated, err := h.service.UpdateMerchantConnectionStatus(r.Context(), connID, req.Status, correlationID, causationID)
	if err != nil {
		writeError(w, CodeValidationError)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, updated)
}

func parseUUIDParam(w http.ResponseWriter, r *http.Request, param string) (uuid.UUID, bool) {
	idStr := chi.URLParam(r, param)
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeError(w, CodeInvalidArgument)
		return uuid.Nil, false
	}
	return id, true
}

func subjectFromContext(r *http.Request) string {
	if sub := r.Header.Get(serviceauth.HeaderSubject); sub != "" {
		return sub
	}
	if sub := r.Header.Get("X-Subject"); sub != "" {
		return sub
	}
	return "test-subject"
}

func parsePageQuery(r *http.Request) commerce.Page {
	page := 1
	limit := 20
	if pStr := r.URL.Query().Get("page"); pStr != "" {
		if p, err := strconv.Atoi(pStr); err == nil && p > 0 {
			page = p
		}
	}
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
			limit = l
		}
	}
	offset := (page - 1) * limit
	return commerce.Page{Limit: limit, Offset: offset}
}
