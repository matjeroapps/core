package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"core/modules/commerce"
)

type CreateConnectionParams struct {
	ActorType           ActorType       `json:"actor_type"`
	ActorID             string          `json:"actor_id"`
	Provider            Provider        `json:"provider"`
	Name                string          `json:"name"`
	CredentialsVaultRef string          `json:"credentials_vault_ref,omitempty"`
	Settings            json.RawMessage `json:"settings,omitempty"`
}

type UpsertEntityMappingParams struct {
	ConnectionID    string          `json:"connection_id"`
	EntityType      EntityType      `json:"entity_type"`
	InternalID      string          `json:"internal_id"`
	ExternalID      string          `json:"external_id"`
	ExternalVersion string          `json:"external_version,omitempty"`
	MappingStatus   MappingStatus   `json:"mapping_status,omitempty"`
	SyncDirection   SyncDirection   `json:"sync_direction,omitempty"`
	ConflictStatus  string          `json:"conflict_status,omitempty"`
	Metadata        json.RawMessage `json:"metadata,omitempty"`
}

type UpdateSyncCursorParams struct {
	ConnectionID       string     `json:"connection_id"`
	EntityType         EntityType `json:"entity_type"`
	CursorToken        string     `json:"cursor_token,omitempty"`
	LastSuccessfulSync *time.Time `json:"last_successful_sync,omitempty"`
	LastReconciledAt   *time.Time `json:"last_reconciled_at,omitempty"`
}

type PersistWebhookInboxParams struct {
	ConnectionID   string          `json:"connection_id,omitempty"`
	Provider       Provider        `json:"provider"`
	EventType      string          `json:"event_type"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload"`
}

type Service interface {
	CreateConnection(ctx context.Context, params CreateConnectionParams) (*Connection, error)
	GetConnection(ctx context.Context, id string) (*Connection, error)
	ListConnectionsByActor(ctx context.Context, actorType ActorType, actorID string, page commerce.Page) ([]Connection, error)
	UpdateConnectionStatus(ctx context.Context, id string, status ConnectionStatus) (*Connection, error)

	UpsertEntityMapping(ctx context.Context, params UpsertEntityMappingParams) (*EntityMapping, error)
	GetMappingByExternalID(ctx context.Context, connectionID string, entityType EntityType, externalID string) (*EntityMapping, error)
	GetMappingByInternalID(ctx context.Context, connectionID string, entityType EntityType, internalID string) (*EntityMapping, error)
	ListEntityMappings(ctx context.Context, connectionID string, entityType EntityType, page commerce.Page) ([]EntityMapping, error)

	UpdateSyncCursor(ctx context.Context, params UpdateSyncCursorParams) (*SyncCursor, error)
	GetSyncCursor(ctx context.Context, connectionID string, entityType EntityType) (*SyncCursor, error)

	PersistWebhookInbox(ctx context.Context, params PersistWebhookInboxParams) (*WebhookInboxItem, bool, error)

	CreateSyncJob(ctx context.Context, connectionID, supplierID string) (*SupplierSyncJob, error)
	GetSyncJob(ctx context.Context, id string) (*SupplierSyncJob, error)
	UpdateSyncJobStatus(ctx context.Context, id string, status SyncJobStatus, processed, failed int, errSummary string) (*SupplierSyncJob, error)
	ListSyncJobsBySupplier(ctx context.Context, supplierID string, page commerce.Page) ([]SupplierSyncJob, error)

	CreateSellerSyncJob(ctx context.Context, connectionID, storeID, syncType string) (*SellerSyncJob, error)
	GetSellerSyncJob(ctx context.Context, id string) (*SellerSyncJob, error)
	UpdateSellerSyncJobStatus(ctx context.Context, id string, status SyncJobStatus, processed, failed int, errSummary string) (*SellerSyncJob, error)
	ListSellerSyncJobsByStore(ctx context.Context, storeID string, page commerce.Page) ([]SellerSyncJob, error)

	CreateAPIKey(ctx context.Context, input CreateAPIKeyInput) (*CreateAPIKeyOutput, error)
	AuthenticateAPIKey(ctx context.Context, rawKey string) (*APIKey, error)
	ListAPIKeys(ctx context.Context, actorType ActorType, actorID string) ([]APIKey, error)
	RevokeAPIKey(ctx context.Context, keyID, actorID string) error
	RevokeAPIKeyWithReason(ctx context.Context, keyID, actorID, reason string) error
	RotateAPIKey(ctx context.Context, keyID, actorID string, gracePeriod time.Duration) (*CreateAPIKeyOutput, error)

	CreateWebhookSubscription(ctx context.Context, input CreateWebhookSubscriptionInput) (*WebhookSubscription, error)
	ListWebhookSubscriptions(ctx context.Context, actorType ActorType, actorID string) ([]WebhookSubscription, error)
	DeleteWebhookSubscription(ctx context.Context, subID, actorID string) error
	GetWebhookSubscription(ctx context.Context, subID string) (*WebhookSubscription, error)
	DispatchWebhookEvent(ctx context.Context, actorType ActorType, actorID, eventType string, payload json.RawMessage) ([]WebhookOutboxItem, error)
	DeliverWebhookOutboxItem(ctx context.Context, item WebhookOutboxItem, secret string, client *http.Client) error
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) CreateConnection(ctx context.Context, params CreateConnectionParams) (*Connection, error) {
	if strings.TrimSpace(params.ActorID) == "" {
		return nil, fmt.Errorf("actor_id is required")
	}
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	if params.Settings == nil {
		params.Settings = json.RawMessage("{}")
	}

	now := time.Now().UTC()
	conn := Connection{
		ID:                  uuid.NewString(),
		ActorType:           params.ActorType,
		ActorID:             params.ActorID,
		Provider:            params.Provider,
		Name:                name,
		Status:              ConnectionStatusActive,
		CredentialsVaultRef: params.CredentialsVaultRef,
		Settings:            params.Settings,
		CreatedAt:           now,
		UpdatedAt:           now,
	}

	if err := s.repo.CreateConnection(ctx, nil, conn); err != nil {
		return nil, err
	}
	return &conn, nil
}

func (s *service) GetConnection(ctx context.Context, id string) (*Connection, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrConnectionNotFound
	}
	return s.repo.GetConnectionByID(ctx, nil, id)
}

func (s *service) ListConnectionsByActor(ctx context.Context, actorType ActorType, actorID string, page commerce.Page) ([]Connection, error) {
	return s.repo.ListConnectionsByActor(ctx, nil, actorType, actorID, page)
}

func (s *service) UpdateConnectionStatus(ctx context.Context, id string, status ConnectionStatus) (*Connection, error) {
	return s.repo.UpdateConnectionStatus(ctx, nil, id, status)
}

func (s *service) UpsertEntityMapping(ctx context.Context, params UpsertEntityMappingParams) (*EntityMapping, error) {
	if strings.TrimSpace(params.ConnectionID) == "" || strings.TrimSpace(params.InternalID) == "" || strings.TrimSpace(params.ExternalID) == "" {
		return nil, fmt.Errorf("connection_id, internal_id, and external_id are required")
	}
	if params.MappingStatus == "" {
		params.MappingStatus = MappingStatusSynced
	}
	if params.SyncDirection == "" {
		params.SyncDirection = SyncDirectionBidirectional
	}
	if params.Metadata == nil {
		params.Metadata = json.RawMessage("{}")
	}

	now := time.Now().UTC()
	mapping := EntityMapping{
		ID:              uuid.NewString(),
		ConnectionID:    params.ConnectionID,
		EntityType:      params.EntityType,
		InternalID:      params.InternalID,
		ExternalID:      params.ExternalID,
		ExternalVersion: params.ExternalVersion,
		MappingStatus:   params.MappingStatus,
		SyncDirection:   params.SyncDirection,
		ConflictStatus:  params.ConflictStatus,
		Metadata:        params.Metadata,
		LastSyncedAt:    now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	if err := s.repo.UpsertEntityMapping(ctx, nil, mapping); err != nil {
		return nil, err
	}
	return &mapping, nil
}

func (s *service) GetMappingByExternalID(ctx context.Context, connectionID string, entityType EntityType, externalID string) (*EntityMapping, error) {
	return s.repo.GetMappingByExternalID(ctx, nil, connectionID, entityType, externalID)
}

func (s *service) GetMappingByInternalID(ctx context.Context, connectionID string, entityType EntityType, internalID string) (*EntityMapping, error) {
	return s.repo.GetMappingByInternalID(ctx, nil, connectionID, entityType, internalID)
}

func (s *service) ListEntityMappings(ctx context.Context, connectionID string, entityType EntityType, page commerce.Page) ([]EntityMapping, error) {
	return s.repo.ListEntityMappings(ctx, nil, connectionID, entityType, page)
}

func (s *service) UpdateSyncCursor(ctx context.Context, params UpdateSyncCursorParams) (*SyncCursor, error) {
	now := time.Now().UTC()
	cursor := SyncCursor{
		ID:                 uuid.NewString(),
		ConnectionID:       params.ConnectionID,
		EntityType:         params.EntityType,
		CursorToken:        params.CursorToken,
		LastSuccessfulSync: params.LastSuccessfulSync,
		LastReconciledAt:   params.LastReconciledAt,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	if err := s.repo.UpsertSyncCursor(ctx, nil, cursor); err != nil {
		return nil, err
	}
	return &cursor, nil
}

func (s *service) GetSyncCursor(ctx context.Context, connectionID string, entityType EntityType) (*SyncCursor, error) {
	return s.repo.GetSyncCursor(ctx, nil, connectionID, entityType)
}

func (s *service) PersistWebhookInbox(ctx context.Context, params PersistWebhookInboxParams) (*WebhookInboxItem, bool, error) {
	if strings.TrimSpace(params.IdempotencyKey) == "" {
		return nil, false, fmt.Errorf("idempotency_key is required")
	}
	if params.Payload == nil {
		params.Payload = json.RawMessage("{}")
	}

	item := WebhookInboxItem{
		ID:             uuid.NewString(),
		ConnectionID:   params.ConnectionID,
		Provider:       params.Provider,
		EventType:      params.EventType,
		IdempotencyKey: params.IdempotencyKey,
		Payload:        params.Payload,
		Status:         "received",
		ReceivedAt:     time.Now().UTC(),
	}

	return s.repo.PersistWebhookInbox(ctx, nil, item)
}

func (s *service) CreateSyncJob(ctx context.Context, connectionID, supplierID string) (*SupplierSyncJob, error) {
	if strings.TrimSpace(connectionID) == "" || strings.TrimSpace(supplierID) == "" {
		return nil, fmt.Errorf("connection_id and supplier_id are required")
	}

	now := time.Now().UTC()
	job := SupplierSyncJob{
		ID:           uuid.NewString(),
		ConnectionID: connectionID,
		SupplierID:   supplierID,
		Status:       SyncJobStatusQueued,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	return s.repo.CreateSyncJob(ctx, nil, job)
}

func (s *service) GetSyncJob(ctx context.Context, id string) (*SupplierSyncJob, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrSyncJobNotFound
	}
	return s.repo.GetSyncJobByID(ctx, nil, id)
}

func (s *service) UpdateSyncJobStatus(ctx context.Context, id string, status SyncJobStatus, processed, failed int, errSummary string) (*SupplierSyncJob, error) {
	return s.repo.UpdateSyncJobStatus(ctx, nil, id, status, processed, failed, errSummary)
}

func (s *service) ListSyncJobsBySupplier(ctx context.Context, supplierID string, page commerce.Page) ([]SupplierSyncJob, error) {
	return s.repo.ListSyncJobsBySupplier(ctx, nil, supplierID, page)
}

func (s *service) CreateSellerSyncJob(ctx context.Context, connectionID, storeID, syncType string) (*SellerSyncJob, error) {
	if strings.TrimSpace(connectionID) == "" || strings.TrimSpace(storeID) == "" {
		return nil, fmt.Errorf("connection_id and store_id are required")
	}
	if strings.TrimSpace(syncType) == "" {
		syncType = "catalog_import"
	}

	now := time.Now().UTC()
	job := SellerSyncJob{
		ID:           uuid.NewString(),
		StoreID:      storeID,
		ConnectionID: connectionID,
		SyncType:     syncType,
		Status:       SyncJobStatusQueued,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	return s.repo.CreateSellerSyncJob(ctx, nil, job)
}

func (s *service) GetSellerSyncJob(ctx context.Context, id string) (*SellerSyncJob, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrSellerSyncJobNotFound
	}
	return s.repo.GetSellerSyncJobByID(ctx, nil, id)
}

func (s *service) UpdateSellerSyncJobStatus(ctx context.Context, id string, status SyncJobStatus, processed, failed int, errSummary string) (*SellerSyncJob, error) {
	return s.repo.UpdateSellerSyncJobStatus(ctx, nil, id, status, processed, failed, errSummary)
}

func (s *service) ListSellerSyncJobsByStore(ctx context.Context, storeID string, page commerce.Page) ([]SellerSyncJob, error) {
	return s.repo.ListSellerSyncJobsByStore(ctx, nil, storeID, page)
}
