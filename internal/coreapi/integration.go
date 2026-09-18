package coreapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"core/internal/integration"
	"core/packages/httpx"
)

func (s *server) handleCreateConnection(w http.ResponseWriter, r *http.Request) {
	var req CreateConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	conn, err := s.deps.Integration.CreateConnection(r.Context(), integration.CreateConnectionParams{
		ActorType:           integration.ActorType(req.ActorType),
		ActorID:             req.ActorID,
		Provider:            integration.Provider(req.Provider),
		Name:                req.Name,
		CredentialsVaultRef: req.CredentialsVaultRef,
		Settings:            req.Settings,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, toConnectionResponse(conn))
}

func (s *server) handleGetConnection(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, CodeValidationError)
		return
	}

	conn, err := s.deps.Integration.GetConnection(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toConnectionResponse(conn))
}

func (s *server) handleListConnections(w http.ResponseWriter, r *http.Request) {
	actorType := r.URL.Query().Get("actor_type")
	actorID := r.URL.Query().Get("actor_id")
	page := parsePage(r)

	conns, err := s.deps.Integration.ListConnectionsByActor(r.Context(), integration.ActorType(actorType), actorID, page)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]ConnectionResponse, 0, len(conns))
	for _, c := range conns {
		items = append(items, toConnectionResponse(&c))
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[ConnectionResponse]{Items: items})
}

func (s *server) handleUpdateConnectionStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req UpdateConnectionStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	conn, err := s.deps.Integration.UpdateConnectionStatus(r.Context(), id, integration.ConnectionStatus(req.Status))
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toConnectionResponse(conn))
}

func (s *server) handleUpsertEntityMapping(w http.ResponseWriter, r *http.Request) {
	var req UpsertEntityMappingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	mapping, err := s.deps.Integration.UpsertEntityMapping(r.Context(), integration.UpsertEntityMappingParams{
		ConnectionID:    req.ConnectionID,
		EntityType:      integration.EntityType(req.EntityType),
		InternalID:      req.InternalID,
		ExternalID:      req.ExternalID,
		ExternalVersion: req.ExternalVersion,
		MappingStatus:   integration.MappingStatus(req.MappingStatus),
		SyncDirection:   integration.SyncDirection(req.SyncDirection),
		ConflictStatus:  req.ConflictStatus,
		Metadata:        req.Metadata,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toEntityMappingResponse(mapping))
}

func (s *server) handleGetEntityMappingByExternalID(w http.ResponseWriter, r *http.Request) {
	connID := r.URL.Query().Get("connection_id")
	entityType := r.URL.Query().Get("entity_type")
	externalID := r.URL.Query().Get("external_id")

	mapping, err := s.deps.Integration.GetMappingByExternalID(r.Context(), connID, integration.EntityType(entityType), externalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toEntityMappingResponse(mapping))
}

func (s *server) handleGetEntityMappingByInternalID(w http.ResponseWriter, r *http.Request) {
	connID := r.URL.Query().Get("connection_id")
	entityType := r.URL.Query().Get("entity_type")
	internalID := r.URL.Query().Get("internal_id")

	mapping, err := s.deps.Integration.GetMappingByInternalID(r.Context(), connID, integration.EntityType(entityType), internalID)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toEntityMappingResponse(mapping))
}

func (s *server) handleListEntityMappings(w http.ResponseWriter, r *http.Request) {
	connID := r.URL.Query().Get("connection_id")
	entityType := r.URL.Query().Get("entity_type")
	page := parsePage(r)

	mappings, err := s.deps.Integration.ListEntityMappings(r.Context(), connID, integration.EntityType(entityType), page)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]EntityMappingResponse, 0, len(mappings))
	for _, m := range mappings {
		items = append(items, toEntityMappingResponse(&m))
	}

	httpx.WriteJSON(w, http.StatusOK, CollectionResponse[EntityMappingResponse]{Items: items})
}

func (s *server) handleUpdateSyncCursor(w http.ResponseWriter, r *http.Request) {
	var req UpdateSyncCursorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	cursor, err := s.deps.Integration.UpdateSyncCursor(r.Context(), integration.UpdateSyncCursorParams{
		ConnectionID:       req.ConnectionID,
		EntityType:         integration.EntityType(req.EntityType),
		CursorToken:        req.CursorToken,
		LastSuccessfulSync: req.LastSuccessfulSync,
		LastReconciledAt:   req.LastReconciledAt,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toSyncCursorResponse(cursor))
}

func (s *server) handleGetSyncCursor(w http.ResponseWriter, r *http.Request) {
	connID := r.URL.Query().Get("connection_id")
	entityType := r.URL.Query().Get("entity_type")

	cursor, err := s.deps.Integration.GetSyncCursor(r.Context(), connID, integration.EntityType(entityType))
	if err != nil {
		writeDomainError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toSyncCursorResponse(cursor))
}

func (s *server) handlePersistIntegrationWebhookInbox(w http.ResponseWriter, r *http.Request) {
	var req PersistIntegrationWebhookInboxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, CodeValidationError)
		return
	}

	item, created, err := s.deps.Integration.PersistWebhookInbox(r.Context(), integration.PersistWebhookInboxParams{
		ConnectionID:   req.ConnectionID,
		Provider:       integration.Provider(req.Provider),
		EventType:      req.EventType,
		IdempotencyKey: req.IdempotencyKey,
		Payload:        req.Payload,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}

	httpx.WriteJSON(w, status, toWebhookInboxItemResponse(item))
}

func toConnectionResponse(c *integration.Connection) ConnectionResponse {
	return ConnectionResponse{
		ID:                  c.ID,
		ActorType:           string(c.ActorType),
		ActorID:             c.ActorID,
		Provider:            string(c.Provider),
		Name:                c.Name,
		Status:              string(c.Status),
		CredentialsVaultRef: c.CredentialsVaultRef,
		Settings:            c.Settings,
		CreatedAt:           c.CreatedAt,
		UpdatedAt:           c.UpdatedAt,
	}
}

func toEntityMappingResponse(m *integration.EntityMapping) EntityMappingResponse {
	return EntityMappingResponse{
		ID:              m.ID,
		ConnectionID:    m.ConnectionID,
		EntityType:      string(m.EntityType),
		InternalID:      m.InternalID,
		ExternalID:      m.ExternalID,
		ExternalVersion: m.ExternalVersion,
		MappingStatus:   string(m.MappingStatus),
		SyncDirection:   string(m.SyncDirection),
		ConflictStatus:  m.ConflictStatus,
		Metadata:        m.Metadata,
		LastSyncedAt:    m.LastSyncedAt,
		CreatedAt:       m.CreatedAt,
		UpdatedAt:       m.UpdatedAt,
	}
}

func toSyncCursorResponse(sc *integration.SyncCursor) SyncCursorResponse {
	return SyncCursorResponse{
		ID:                 sc.ID,
		ConnectionID:       sc.ConnectionID,
		EntityType:         string(sc.EntityType),
		CursorToken:        sc.CursorToken,
		LastSuccessfulSync: sc.LastSuccessfulSync,
		LastReconciledAt:   sc.LastReconciledAt,
		CreatedAt:          sc.CreatedAt,
		UpdatedAt:          sc.UpdatedAt,
	}
}

func toWebhookInboxItemResponse(w *integration.WebhookInboxItem) WebhookInboxItemResponse {
	return WebhookInboxItemResponse{
		ID:             w.ID,
		ConnectionID:   w.ConnectionID,
		Provider:       string(w.Provider),
		EventType:      w.EventType,
		IdempotencyKey: w.IdempotencyKey,
		Payload:        w.Payload,
		Status:         w.Status,
		ErrorMessage:   w.ErrorMessage,
		ReceivedAt:     w.ReceivedAt,
		ProcessedAt:    w.ProcessedAt,
	}
}
