package coreapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"core/internal/integration"
	"core/packages/httpx"
)

func (s *server) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var body CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	out, err := s.deps.Integration.CreateAPIKey(r.Context(), integration.CreateAPIKeyInput{
		ActorType: integration.ActorType(body.ActorType),
		ActorID:   body.ActorID,
		Name:      body.Name,
		Scopes:    body.Scopes,
		Live:      body.Live,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	res := CreateAPIKeyResponse{
		Record: APIKeyResponse{
			ID:        out.Record.ID,
			ActorType: string(out.Record.ActorType),
			ActorID:   out.Record.ActorID,
			Name:      out.Record.Name,
			KeyPrefix: out.Record.KeyPrefix,
			Scopes:    out.Record.Scopes,
			Status:    out.Record.Status,
			ExpiresAt: out.Record.ExpiresAt,
			CreatedAt: out.Record.CreatedAt,
			UpdatedAt: out.Record.UpdatedAt,
		},
		RawAPIKey: out.RawAPIKey,
	}

	httpx.WriteJSON(w, http.StatusCreated, res)
}

func (s *server) handleAuthenticateAPIKey(w http.ResponseWriter, r *http.Request) {
	rawKey := r.URL.Query().Get("raw_key")
	if rawKey == "" {
		writeError(w, CodeValidationError)
		return
	}

	key, err := s.deps.Integration.AuthenticateAPIKey(r.Context(), rawKey)
	if err == integration.ErrInvalidAPIKey {
		httpx.WriteError(w, http.StatusUnauthorized, "invalid_api_key", "API key is invalid or revoked")
		return
	} else if err != nil {
		writeDomainError(w, err)
		return
	}

	res := APIKeyResponse{
		ID:        key.ID,
		ActorType: string(key.ActorType),
		ActorID:   key.ActorID,
		Name:      key.Name,
		KeyPrefix: key.KeyPrefix,
		Scopes:    key.Scopes,
		Status:    key.Status,
		ExpiresAt: key.ExpiresAt,
		CreatedAt: key.CreatedAt,
		UpdatedAt: key.UpdatedAt,
	}

	httpx.WriteJSON(w, http.StatusOK, res)
}

func (s *server) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	actorType := r.URL.Query().Get("actor_type")
	actorID := r.URL.Query().Get("actor_id")

	keys, err := s.deps.Integration.ListAPIKeys(r.Context(), integration.ActorType(actorType), actorID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]APIKeyResponse, len(keys))
	for i, k := range keys {
		items[i] = APIKeyResponse{
			ID:        k.ID,
			ActorType: string(k.ActorType),
			ActorID:   k.ActorID,
			Name:      k.Name,
			KeyPrefix: k.KeyPrefix,
			Scopes:    k.Scopes,
			Status:    k.Status,
			ExpiresAt: k.ExpiresAt,
			CreatedAt: k.CreatedAt,
			UpdatedAt: k.UpdatedAt,
		}
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	keyID := chi.URLParam(r, "id")
	actorID := r.URL.Query().Get("actor_id")

	if err := s.deps.Integration.RevokeAPIKey(r.Context(), keyID, actorID); err == integration.ErrAPIKeyNotFound {
		writeError(w, CodeNotFound)
		return
	} else if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "revoked"})
}

func (s *server) handleCreateWebhookSubscription(w http.ResponseWriter, r *http.Request) {
	var body CreateWebhookSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	sub, err := s.deps.Integration.CreateWebhookSubscription(r.Context(), integration.CreateWebhookSubscriptionInput{
		ActorType:        integration.ActorType(body.ActorType),
		ActorID:          body.ActorID,
		TargetURL:        body.TargetURL,
		Secret:           body.Secret,
		SubscribedEvents: body.SubscribedEvents,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	res := WebhookSubscriptionResponse{
		ID:               sub.ID,
		ActorType:        string(sub.ActorType),
		ActorID:          sub.ActorID,
		TargetURL:        sub.TargetURL,
		SubscribedEvents: sub.SubscribedEvents,
		Status:           sub.Status,
		CreatedAt:        sub.CreatedAt,
		UpdatedAt:        sub.UpdatedAt,
	}

	httpx.WriteJSON(w, http.StatusCreated, res)
}

func (s *server) handleListWebhookSubscriptions(w http.ResponseWriter, r *http.Request) {
	actorType := r.URL.Query().Get("actor_type")
	actorID := r.URL.Query().Get("actor_id")

	subs, err := s.deps.Integration.ListWebhookSubscriptions(r.Context(), integration.ActorType(actorType), actorID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]WebhookSubscriptionResponse, len(subs))
	for i, sub := range subs {
		items[i] = WebhookSubscriptionResponse{
			ID:               sub.ID,
			ActorType:        string(sub.ActorType),
			ActorID:          sub.ActorID,
			TargetURL:        sub.TargetURL,
			SubscribedEvents: sub.SubscribedEvents,
			Status:           sub.Status,
			CreatedAt:        sub.CreatedAt,
			UpdatedAt:        sub.UpdatedAt,
		}
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *server) handleDeleteWebhookSubscription(w http.ResponseWriter, r *http.Request) {
	subID := chi.URLParam(r, "id")
	actorID := r.URL.Query().Get("actor_id")

	if err := s.deps.Integration.DeleteWebhookSubscription(r.Context(), subID, actorID); err == integration.ErrWebhookSubNotFound {
		writeError(w, CodeNotFound)
		return
	} else if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
