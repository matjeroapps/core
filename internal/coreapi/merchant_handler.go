package coreapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"core/internal/merchants"
	"core/packages/httpx"

	"github.com/google/uuid"
)

type MerchantHandler struct {
	service *merchants.Service
}

func NewMerchantHandler(service *merchants.Service) *MerchantHandler {
	return &MerchantHandler{service: service}
}

type CreateMerchantRequest struct {
	Code              string                   `json:"code"`
	LegalName         string                   `json:"legal_name"`
	InitialCapability merchants.CapabilityType `json:"initial_capability"`
}

func (h *MerchantHandler) CreateMerchant(w http.ResponseWriter, r *http.Request) {
	var req CreateMerchantRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	merchant, err := h.service.CreateMerchant(r.Context(), req.Code, req.LegalName, req.InitialCapability)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, merchant)
}

func (h *MerchantHandler) GetMerchant(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/internal/v1/merchants/")
	id, err := uuid.Parse(path)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid merchant_id"})
		return
	}

	merchant, err := h.service.GetMerchant(r.Context(), id)
	if err != nil {
		httpx.WriteJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, merchant)
}

type ActivateCapabilityRequest struct {
	CapabilityType merchants.CapabilityType `json:"capability_type"`
}

func (h *MerchantHandler) ActivateCapability(w http.ResponseWriter, r *http.Request) {
	// Path: /internal/v1/merchants/{merchant_id}/capabilities/{capability_type}/activate
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 6 {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	merchantID, err := uuid.Parse(parts[3])
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid merchant_id"})
		return
	}

	capType := merchants.CapabilityType(strings.ToUpper(parts[5]))

	cap, err := h.service.ActivateCapability(r.Context(), merchantID, capType)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	httpx.WriteJSON(w, http.StatusOK, cap)
}

type AddMemberRequest struct {
	PrincipalSubject string   `json:"principal_subject"`
	Permissions      []string `json:"permissions"`
}

func (h *MerchantHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	// Path: /internal/v1/merchants/{merchant_id}/memberships
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 5 {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid path"})
		return
	}

	merchantID, err := uuid.Parse(parts[3])
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid merchant_id"})
		return
	}

	var req AddMemberRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	mem, err := h.service.AddMember(r.Context(), merchantID, req.PrincipalSubject, req.Permissions)
	if err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, mem)
}
