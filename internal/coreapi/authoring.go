package coreapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"core/internal/serviceauth"
	"core/modules/commerce"
	"core/packages/httpx"

	"github.com/go-chi/chi/v5"
)

type createAIJobRequest struct {
	Brief               string   `json:"brief"`
	SourceAssetIDs      []string `json:"source_asset_ids"`
	RequestedImageCount int      `json:"requested_image_count"`
}

type updateAIFieldRequest struct {
	Decision string `json:"decision"`
	Value    string `json:"value"`
}

type retryAIJobRequest struct {
	Parts        []string `json:"parts"`
	CandidateIDs []string `json:"candidate_ids"`
}

type acceptAIImageRequest struct {
	IsVisible bool   `json:"is_visible"`
	AltText   string `json:"alt_text"`
}

func (s *server) handleAILimits(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}
	limits, err := s.deps.Commerce.GetAuthoringLimitsForSubject(r.Context(), subject, storeID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, limits)
}

func (s *server) handleCreateAIJob(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}
	var req createAIJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	job, err := s.deps.Commerce.CreateAuthoringJobForSubject(r.Context(), subject, storeID, commerce.AuthoringCreateRequest{Brief: req.Brief, SourceAssetIDs: req.SourceAssetIDs, RequestedImageCount: req.RequestedImageCount, IDempotencyKey: r.Header.Get("Idempotency-Key")})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, job)
}

func (s *server) handleGetAIJob(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	jobID := chi.URLParam(r, "jobID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}
	job, err := s.deps.Commerce.GetAuthoringJobForSubject(r.Context(), subject, storeID, jobID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, job)
}

func (s *server) handleUpdateAIJobField(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	jobID := chi.URLParam(r, "jobID")
	fieldID := chi.URLParam(r, "fieldID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}
	var req updateAIFieldRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	field, err := s.deps.Commerce.UpdateAuthoringFieldForSubject(r.Context(), subject, storeID, jobID, fieldID, commerce.AuthoringFieldDecisionRequest{Decision: req.Decision, Value: req.Value})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, field)
}

func (s *server) handleRetryAIJob(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	jobID := chi.URLParam(r, "jobID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}
	var req retryAIJobRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	job, err := s.deps.Commerce.RetryAuthoringJobForSubject(r.Context(), subject, storeID, jobID, commerce.AuthoringRetryRequest{Parts: req.Parts, CandidateIDs: req.CandidateIDs})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, job)
}

func (s *server) handleAcceptAIJobImage(w http.ResponseWriter, r *http.Request) {
	storeID := chi.URLParam(r, "storeID")
	jobID := chi.URLParam(r, "jobID")
	candidateID := chi.URLParam(r, "candidateID")
	subject := serviceauth.SubjectFrom(r)
	if subject == "" {
		writeError(w, CodeUnauthorized)
		return
	}
	var req acceptAIImageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeInvalidArgument)
		return
	}
	candidate, err := s.deps.Commerce.AcceptAuthoringCandidateForSubject(r.Context(), subject, storeID, jobID, candidateID, commerce.AuthoringAcceptImageRequest{IsVisible: req.IsVisible, AltText: strings.TrimSpace(req.AltText)})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, candidate)
}
