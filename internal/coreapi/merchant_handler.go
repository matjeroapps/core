package coreapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"core/internal/merchants"
	"core/internal/serviceauth"
	"core/packages/httpx"
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

type EnsureRetailWorkspaceRequest struct {
	Code      string `json:"code"`
	LegalName string `json:"legal_name"`
}

// EnsureRetailWorkspace bridges a newly authenticated Seller identity into
// Core's canonical Merchant model during first-run onboarding.
func (h *MerchantHandler) EnsureRetailWorkspace(w http.ResponseWriter, r *http.Request) {
	if h.service == nil {
		writeError(w, CodeUnavailable)
		return
	}
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}
	if r.Body == nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	var req EnsureRetailWorkspaceRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil || strings.TrimSpace(req.Code) == "" || strings.TrimSpace(req.LegalName) == "" {
		writeError(w, CodeInvalidArgument)
		return
	}
	merchant, err := h.service.EnsureRetailWorkspaceWithMetadata(
		r.Context(),
		subject,
		strings.TrimSpace(req.Code),
		strings.TrimSpace(req.LegalName),
		metadataFromRequest(r),
	)
	if err != nil {
		writeError(w, CodeValidationError)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, merchant)
}

func (h *MerchantHandler) CreateMerchant(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	var req CreateMerchantRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	if req.Code == "" || req.LegalName == "" || !validCapability(req.InitialCapability) {
		writeError(w, CodeValidationError)
		return
	}

	merchant, err := h.service.CreateMerchantWithMetadata(r.Context(), req.Code, req.LegalName, req.InitialCapability, metadataFromRequest(r))
	if err != nil {
		writeError(w, CodeValidationError)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, merchant)
}

func (h *MerchantHandler) GetMerchant(w http.ResponseWriter, r *http.Request) {
	id, ok := merchantIDParam(w, r)
	if !ok {
		return
	}

	merchant, err := h.service.GetMerchant(r.Context(), id)
	if err != nil {
		writeError(w, CodeNotFound)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, merchant)
}

func (h *MerchantHandler) ActivateCapability(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := merchantIDParam(w, r)
	if !ok {
		return
	}
	capType := merchants.CapabilityType(strings.ToUpper(chi.URLParam(r, "capabilityType")))
	if !validCapability(capType) {
		writeError(w, CodeInvalidArgument)
		return
	}

	cap, err := h.service.ActivateCapabilityWithMetadata(r.Context(), merchantID, capType, metadataFromRequest(r))
	if err != nil {
		writeError(w, CodeValidationError)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, cap)
}

type AddMemberRequest struct {
	PrincipalSubject string   `json:"principal_subject"`
	Permissions      []string `json:"permissions"`
}

func (h *MerchantHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	merchantID, ok := merchantIDParam(w, r)
	if !ok {
		return
	}
	if r.Body == nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	var req AddMemberRequest
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	if req.PrincipalSubject == "" {
		writeError(w, CodeValidationError)
		return
	}

	mem, err := h.service.AddMemberWithMetadata(r.Context(), merchantID, req.PrincipalSubject, req.Permissions, metadataFromRequest(r))
	if err != nil {
		writeError(w, CodeValidationError)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, mem)
}

func merchantIDParam(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "merchantID"))
	if err != nil {
		writeError(w, CodeInvalidArgument)
		return uuid.Nil, false
	}
	return id, true
}

func metadataFromRequest(r *http.Request) merchants.MutationMetadata {
	return merchants.MutationMetadata{
		CorrelationID:  httpx.CorrelationID(r.Context()),
		CausationID:    httpx.RequestID(r.Context()),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	}
}

func validCapability(capType merchants.CapabilityType) bool {
	return capType == merchants.CapabilityTypeRetail || capType == merchants.CapabilityTypeSupply
}
