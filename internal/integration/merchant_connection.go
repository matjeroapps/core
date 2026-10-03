package integration

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

type MerchantConnectionType string

const (
	ConnectionTypeSupplySource  MerchantConnectionType = "SUPPLY_SOURCE"
	ConnectionTypeRetailChannel MerchantConnectionType = "RETAIL_CHANNEL"
)

type MerchantConnectionStatus string

const (
	MerchantStatusDraft       MerchantConnectionStatus = "DRAFT"
	MerchantStatusAuthorizing MerchantConnectionStatus = "AUTHORIZING"
	MerchantStatusConfiguring MerchantConnectionStatus = "CONFIGURING"
	MerchantStatusActive      MerchantConnectionStatus = "ACTIVE"
	MerchantStatusPaused      MerchantConnectionStatus = "PAUSED"
	MerchantStatusError       MerchantConnectionStatus = "ERROR"
	MerchantStatusRevoked     MerchantConnectionStatus = "REVOKED"
	MerchantStatusRetired     MerchantConnectionStatus = "RETIRED"
)

type MerchantJobIntentStatus string

const (
	JobStatusRequested      MerchantJobIntentStatus = "REQUESTED"
	JobStatusQueued         MerchantJobIntentStatus = "QUEUED"
	JobStatusRunning        MerchantJobIntentStatus = "RUNNING"
	JobStatusSucceeded      MerchantJobIntentStatus = "SUCCEEDED"
	JobStatusFailed         MerchantJobIntentStatus = "FAILED"
	JobStatusReviewRequired MerchantJobIntentStatus = "REVIEW_REQUIRED"
	JobStatusCancelled      MerchantJobIntentStatus = "CANCELLED"
)

type MigrationClassification string

const (
	MigrationClassMigrated    MigrationClassification = "MIGRATED"
	MigrationClassQuarantined MigrationClassification = "QUARANTINED"
	MigrationClassDeferred    MigrationClassification = "DEFERRED"
	MigrationClassFailed      MigrationClassification = "FAILED"
)

const (
	QuarantineMissingMerchantLink    = "MISSING_MERCHANT_LINK"
	QuarantineAmbiguousOwnerMismatch = "AMBIGUOUS_OWNER_MISMATCH"
	QuarantineMissingExternalAccount = "MISSING_EXTERNAL_ACCOUNT_ID"
	QuarantineInvalidStoreBinding    = "INVALID_STORE_BINDING"
)

type MerchantIntegrationConnection struct {
	ID                    uuid.UUID                `json:"id" db:"id"`
	MerchantID            uuid.UUID                `json:"merchant_id" db:"merchant_id"`
	ConnectionType        MerchantConnectionType   `json:"connection_type" db:"connection_type"`
	Provider              Provider                 `json:"provider" db:"provider"`
	ExternalAccountID     *string                  `json:"external_account_id,omitempty" db:"external_account_id"`
	Name                  string                   `json:"name" db:"name"`
	StoreID               *uuid.UUID               `json:"store_id,omitempty" db:"store_id"`
	Status                MerchantConnectionStatus `json:"status" db:"status"`
	GrantedScopes         json.RawMessage          `json:"granted_scopes" db:"granted_scopes"`
	HealthStatus          string                   `json:"health_status" db:"health_status"`
	LastHealthCheckAt     *time.Time               `json:"last_health_check_at,omitempty" db:"last_health_check_at"`
	FirstSuccessfulSyncAt *time.Time               `json:"first_successful_sync_at,omitempty" db:"first_successful_sync_at"`
	LegacyActorType       *string                  `json:"legacy_actor_type,omitempty" db:"legacy_actor_type"`
	LegacyActorID         *string                  `json:"legacy_actor_id,omitempty" db:"legacy_actor_id"`
	LegacyConnectionID    *string                  `json:"legacy_connection_id,omitempty" db:"legacy_connection_id"`
	Settings              json.RawMessage          `json:"settings" db:"settings"`
	CreatedAt             time.Time                `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time                `json:"updated_at" db:"updated_at"`
}

type CreateMerchantConnectionInput struct {
	MerchantID         uuid.UUID              `json:"merchant_id"`
	ConnectionType     MerchantConnectionType `json:"connection_type"`
	Provider           Provider               `json:"provider"`
	ExternalAccountID  *string                `json:"external_account_id,omitempty"`
	Name               string                 `json:"name"`
	StoreID            *uuid.UUID             `json:"store_id,omitempty"`
	GrantedScopes      json.RawMessage        `json:"granted_scopes,omitempty"`
	Settings           json.RawMessage        `json:"settings,omitempty"`
	LegacyActorType    *string                `json:"legacy_actor_type,omitempty"`
	LegacyActorID      *string                `json:"legacy_actor_id,omitempty"`
	LegacyConnectionID *string                `json:"legacy_connection_id,omitempty"`
}

type MerchantIntegrationJobIntent struct {
	ID              uuid.UUID               `json:"id" db:"id"`
	MerchantID      uuid.UUID               `json:"merchant_id" db:"merchant_id"`
	ConnectionID    uuid.UUID               `json:"connection_id" db:"connection_id"`
	PipelineType    string                  `json:"pipeline_type" db:"pipeline_type"` // SUPPLY or RETAIL
	JobType         string                  `json:"job_type" db:"job_type"`
	Status          MerchantJobIntentStatus `json:"status" db:"status"`
	IdempotencyKey  string                  `json:"idempotency_key" db:"idempotency_key"`
	CursorOrVersion *string                 `json:"cursor_or_version,omitempty" db:"cursor_or_version"`
	CorrelationID   *string                 `json:"correlation_id,omitempty" db:"correlation_id"`
	CausationID     *string                 `json:"causation_id,omitempty" db:"causation_id"`
	TotalItems      int                     `json:"total_items" db:"total_items"`
	ProcessedItems  int                     `json:"processed_items" db:"processed_items"`
	FailedItems     int                     `json:"failed_items" db:"failed_items"`
	ErrorSummary    *string                 `json:"error_summary,omitempty" db:"error_summary"`
	StartedAt       *time.Time              `json:"started_at,omitempty" db:"started_at"`
	CompletedAt     *time.Time              `json:"completed_at,omitempty" db:"completed_at"`
	CreatedAt       time.Time               `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time               `json:"updated_at" db:"updated_at"`
}

type CreateJobIntentInput struct {
	MerchantID      uuid.UUID `json:"merchant_id"`
	ConnectionID    uuid.UUID `json:"connection_id"`
	PipelineType    string    `json:"pipeline_type"`
	JobType         string    `json:"job_type"`
	IdempotencyKey  string    `json:"idempotency_key"`
	CursorOrVersion *string   `json:"cursor_or_version,omitempty"`
	CorrelationID   *string   `json:"correlation_id,omitempty"`
	CausationID     *string   `json:"causation_id,omitempty"`
}

type MigrationCrosswalkRecord struct {
	ID                       uuid.UUID               `json:"id" db:"id"`
	LegacyConnectionID       string                  `json:"legacy_connection_id" db:"legacy_connection_id"`
	LegacyActorType          string                  `json:"legacy_actor_type" db:"legacy_actor_type"`
	LegacyActorID            string                  `json:"legacy_actor_id" db:"legacy_actor_id"`
	TargetConnectionID       *uuid.UUID              `json:"target_connection_id,omitempty" db:"target_connection_id"`
	MerchantID               *uuid.UUID              `json:"merchant_id,omitempty" db:"merchant_id"`
	Classification           MigrationClassification `json:"classification" db:"classification"`
	ReusedExistingConnection bool                    `json:"reused_existing_connection" db:"reused_existing_connection"`
	QuarantineReasonCode     *string                 `json:"quarantine_reason_code,omitempty" db:"quarantine_reason_code"`
	QuarantineDetails        json.RawMessage         `json:"quarantine_details,omitempty" db:"quarantine_details"`
	RunID                    uuid.UUID               `json:"run_id" db:"run_id"`
	CreatedAt                time.Time               `json:"created_at" db:"created_at"`
}

type MigrationSummary struct {
	RunID               uuid.UUID `json:"run_id"`
	DryRun              bool      `json:"dry_run"`
	ScannedCount        int       `json:"scanned_count"`
	MappedCount         int       `json:"mapped_count"`
	ReusedExistingCount int       `json:"reused_existing_count"`
	QuarantinedCount    int       `json:"quarantined_count"`
	DeferredCount       int       `json:"deferred_count"`
	FailedCount         int       `json:"failed_count"`
	RetriedCount        int       `json:"retried_count"`
}

func (c *MerchantIntegrationConnection) Validate() error {
	if c.MerchantID == uuid.Nil {
		return errors.New("merchant_id is required")
	}
	if c.ConnectionType != ConnectionTypeSupplySource && c.ConnectionType != ConnectionTypeRetailChannel {
		return errors.New("invalid connection_type")
	}
	// Store binding is NOT enforced here: migration-created RETAIL_CHANNEL rows
	// preserve legacy state and bind a store later. The API create path enforces
	// the binding explicitly (see merchant_service.CreateMerchantConnection).
	if c.Provider == "" {
		return errors.New("provider is required")
	}
	if c.Name == "" {
		return errors.New("name is required")
	}
	return nil
}
