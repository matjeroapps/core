package coreapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/matjeroapps/core/internal/payments"
	"github.com/matjeroapps/core/internal/serviceauth"
	"github.com/matjeroapps/core/packages/httpx"
)

func (s *server) handleInitializePayment(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "orderID")
	if orderID == "" {
		writeError(w, CodeValidationError)
		return
	}

	var req InitializePaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	params := payments.InitializePaymentParams{
		OrderID:           orderID,
		AmountMinor:       req.AmountMinor,
		Currency:          req.Currency,
		PaymentMethod:     req.PaymentMethod,
		Provider:          req.Provider,
		ProviderReference: req.ProviderReference,
		CorrelationID:     r.Header.Get("X-Correlation-ID"),
		CausationID:       serviceauth.SubjectFrom(r),
	}

	p, err := s.deps.Payments.InitializePayment(r.Context(), params)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toPaymentResponse(p))
}

func (s *server) handleUpdatePaymentStatus(w http.ResponseWriter, r *http.Request) {
	paymentID := chi.URLParam(r, "paymentID")
	if paymentID == "" {
		writeError(w, CodeValidationError)
		return
	}

	var req UpdatePaymentStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	params := payments.UpdateStatusParams{
		PaymentID:         paymentID,
		NewStatus:         payments.Status(req.Status),
		Provider:          req.Provider,
		ProviderReference: req.ProviderReference,
		ErrorMessage:      req.ErrorMessage,
		CorrelationID:     r.Header.Get("X-Correlation-ID"),
		CausationID:       serviceauth.SubjectFrom(r),
	}

	p, err := s.deps.Payments.UpdatePaymentStatus(r.Context(), params)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toPaymentResponse(p))
}

func (s *server) handlePersistWebhookInbox(w http.ResponseWriter, r *http.Request) {
	var req PersistWebhookInboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	params := payments.PersistWebhookInboxParams{
		Provider:          req.Provider,
		ConnectionID:      req.ConnectionID,
		ProviderEventID:   req.ProviderEventID,
		EventType:         req.EventType,
		PayloadJSON:       req.PayloadJSON,
		SignatureVerified: req.SignatureVerified,
	}

	inbox, deduplicated, err := s.deps.Payments.PersistWebhookInbox(r.Context(), params)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	status := http.StatusCreated
	if deduplicated {
		status = http.StatusOK
	}

	httpx.WriteJSON(w, status, toWebhookInboxResponse(inbox, deduplicated))
}

func toPaymentResponse(p *payments.Payment) PaymentResponse {
	attempts := make([]PaymentAttemptResponse, 0, len(p.Attempts))
	for _, a := range p.Attempts {
		attempts = append(attempts, PaymentAttemptResponse{
			ID:                a.ID,
			PaymentID:         a.PaymentID,
			Provider:          a.Provider,
			ProviderReference: a.ProviderReference,
			Status:            a.Status,
			ErrorMessage:      a.ErrorMessage,
			CreatedAt:         a.CreatedAt,
			UpdatedAt:         a.UpdatedAt,
		})
	}

	return PaymentResponse{
		ID:            p.ID,
		OrderID:       p.OrderID,
		AmountMinor:   p.AmountMinor,
		Currency:      p.Currency,
		PaymentMethod: p.PaymentMethod,
		Status:        string(p.Status),
		Attempts:      attempts,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

func toWebhookInboxResponse(w *payments.WebhookInbox, deduplicated bool) WebhookInboxResponse {
	return WebhookInboxResponse{
		ID:                w.ID,
		Provider:          w.Provider,
		ConnectionID:      w.ConnectionID,
		ProviderEventID:   w.ProviderEventID,
		EventType:         w.EventType,
		PayloadJSON:       w.PayloadJSON,
		SignatureVerified: w.SignatureVerified,
		Status:            w.Status,
		AttemptCount:      w.AttemptCount,
		Deduplicated:      deduplicated,
		ReceivedAt:        w.ReceivedAt,
		ProcessedAt:       w.ProcessedAt,
	}
}
