package integration

import (
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrConnectionNotFound    = errors.New("integration connection not found")
	ErrMappingNotFound       = errors.New("external entity mapping not found")
	ErrSyncCursorNotFound    = errors.New("sync cursor not found")
	ErrWebhookInboxNotFound  = errors.New("webhook inbox item not found")
	ErrDuplicateMapping      = errors.New("duplicate external entity mapping")
	ErrSyncJobNotFound       = errors.New("supplier sync job not found")
	ErrSellerSyncJobNotFound = errors.New("seller sync job not found")
)

type SyncJobStatus string

const (
	SyncJobStatusQueued     SyncJobStatus = "queued"
	SyncJobStatusProcessing SyncJobStatus = "processing"
	SyncJobStatusCompleted  SyncJobStatus = "completed"
	SyncJobStatusFailed     SyncJobStatus = "failed"
)

type SupplierSyncJob struct {
	ID             string        `json:"id"`
	ConnectionID   string        `json:"connection_id"`
	SupplierID     string        `json:"supplier_id"`
	Status         SyncJobStatus `json:"status"`
	TotalItems     int           `json:"total_items"`
	ProcessedItems int           `json:"processed_items"`
	FailedItems    int           `json:"failed_items"`
	ErrorSummary   string        `json:"error_summary,omitempty"`
	StartedAt      *time.Time    `json:"started_at,omitempty"`
	CompletedAt    *time.Time    `json:"completed_at,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type SellerSyncJob struct {
	ID             string        `json:"id"`
	StoreID        string        `json:"store_id"`
	ConnectionID   string        `json:"connection_id"`
	SyncType       string        `json:"sync_type"`
	Status         SyncJobStatus `json:"status"`
	TotalItems     int           `json:"total_items"`
	ProcessedItems int           `json:"processed_items"`
	FailedItems    int           `json:"failed_items"`
	ErrorSummary   string        `json:"error_summary,omitempty"`
	StartedAt      *time.Time    `json:"started_at,omitempty"`
	CompletedAt    *time.Time    `json:"completed_at,omitempty"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type ActorType string

const (
	ActorTypeSeller   ActorType = "seller"
	ActorTypeSupplier ActorType = "supplier"
)

type Provider string

const (
	ProviderSalla       Provider = "salla"
	ProviderShopify     Provider = "shopify"
	ProviderWooCommerce Provider = "woocommerce"
	ProviderEasyOrders  Provider = "easyorders"
	ProviderCustomAPI   Provider = "custom_api"
)

type ConnectionStatus string

const (
	ConnectionStatusActive       ConnectionStatus = "active"
	ConnectionStatusPaused       ConnectionStatus = "paused"
	ConnectionStatusError        ConnectionStatus = "error"
	ConnectionStatusDisconnected ConnectionStatus = "disconnected"
)

type Connection struct {
	ID                  string           `json:"id"`
	ActorType           ActorType        `json:"actor_type"`
	ActorID             string           `json:"actor_id"`
	Provider            Provider         `json:"provider"`
	Name                string           `json:"name"`
	Status              ConnectionStatus `json:"status"`
	CredentialsVaultRef string           `json:"credentials_vault_ref,omitempty"`
	Settings            json.RawMessage  `json:"settings"`
	CreatedAt           time.Time        `json:"created_at"`
	UpdatedAt           time.Time        `json:"updated_at"`
}

type EntityType string

const (
	EntityTypeProduct     EntityType = "product"
	EntityTypeVariant     EntityType = "variant"
	EntityTypeInventory   EntityType = "inventory"
	EntityTypeOrder       EntityType = "order"
	EntityTypeFulfillment EntityType = "fulfillment"
	EntityTypeCustomer    EntityType = "customer"
)

type MappingStatus string

const (
	MappingStatusSynced   MappingStatus = "synced"
	MappingStatusPending  MappingStatus = "pending"
	MappingStatusConflict MappingStatus = "conflict"
	MappingStatusError    MappingStatus = "error"
)

type SyncDirection string

const (
	SyncDirectionInbound       SyncDirection = "inbound"
	SyncDirectionOutbound      SyncDirection = "outbound"
	SyncDirectionBidirectional SyncDirection = "bidirectional"
)

type EntityMapping struct {
	ID              string          `json:"id"`
	ConnectionID    string          `json:"connection_id"`
	EntityType      EntityType      `json:"entity_type"`
	InternalID      string          `json:"internal_id"`
	ExternalID      string          `json:"external_id"`
	ExternalVersion string          `json:"external_version,omitempty"`
	MappingStatus   MappingStatus   `json:"mapping_status"`
	SyncDirection   SyncDirection   `json:"sync_direction"`
	ConflictStatus  string          `json:"conflict_status,omitempty"`
	Metadata        json.RawMessage `json:"metadata"`
	LastSyncedAt    time.Time       `json:"last_synced_at"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

type SyncCursor struct {
	ID                 string     `json:"id"`
	ConnectionID       string     `json:"connection_id"`
	EntityType         EntityType `json:"entity_type"`
	CursorToken        string     `json:"cursor_token,omitempty"`
	LastSuccessfulSync *time.Time `json:"last_successful_sync,omitempty"`
	LastReconciledAt   *time.Time `json:"last_reconciled_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type WebhookInboxItem struct {
	ID             string          `json:"id"`
	ConnectionID   string          `json:"connection_id,omitempty"`
	Provider       Provider        `json:"provider"`
	EventType      string          `json:"event_type"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload"`
	Status         string          `json:"status"`
	ErrorMessage   string          `json:"error_message,omitempty"`
	ReceivedAt     time.Time       `json:"received_at"`
	ProcessedAt    *time.Time      `json:"processed_at,omitempty"`
}
