package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"core/packages/events"
	"core/packages/outbox"
)

// I2 Supply pipeline outbox event types. Payloads carry identifiers and
// lifecycle metadata only; provider credentials and vault references never
// appear in any event (Feature 023 contract remains authoritative).
const (
	EventTypeSupplyBatchStaged          = "merchant.integration.supply.batch_staged.v1"
	EventTypeSupplyBatchApproved        = "merchant.integration.supply.batch_approved.v1"
	EventTypeSupplyFulfillmentRequested = "merchant.integration.supply.fulfillment_requested.v1"
	EventTypeSupplyTrackingRecorded     = "merchant.integration.supply.tracking_recorded.v1"
)

// Supply import batch lifecycle. FIRST_IMPORT batches always enter staging;
// approval is a separate Merchant-authorized decision.
const (
	SupplyBatchTypeFirstImport = "FIRST_IMPORT"
	SupplyBatchTypeFull        = "FULL"
	SupplyBatchTypeIncremental = "INCREMENTAL"

	SupplyBatchStatusStaged            = "STAGED"
	SupplyBatchStatusInReview          = "IN_REVIEW"
	SupplyBatchStatusApproved          = "APPROVED"
	SupplyBatchStatusRejected          = "REJECTED"
	SupplyBatchStatusPartiallyApproved = "PARTIALLY_APPROVED"

	SupplyRecordStatusStaged          = "STAGED"
	SupplyRecordStatusDuplicateReview = "DUPLICATE_REVIEW"
	SupplyRecordStatusApproved        = "APPROVED"
	SupplyRecordStatusRejected        = "REJECTED"
	SupplyRecordStatusReviewRequired  = "REVIEW_REQUIRED"

	SupplyEntityTypeProduct   = "PRODUCT"
	SupplyEntityTypeVariant   = "VARIANT"
	SupplyEntityTypePrice     = "PRICE"
	SupplyEntityTypeInventory = "INVENTORY"

	ReviewCaseStatusOpen      = "OPEN"
	ReviewCaseStatusResolved  = "RESOLVED"
	ReviewCaseStatusDismissed = "DISMISSED"

	ReviewReasonDuplicateSKU     = "DUPLICATE_SKU"
	ReviewReasonDuplicateBarcode = "DUPLICATE_BARCODE"
	ReviewReasonVersionConflict  = "VERSION_CONFLICT"
	ReviewReasonStructuralChange = "STRUCTURAL_CHANGE"

	MappingAuthorityExternal  = "EXTERNAL"
	MappingAuthorityMatjerHub = "MATJERHUB"
	MappingAuthorityManual    = "MANUAL"

	MappingStatusActive         = "ACTIVE"
	MappingStatusReviewRequired = "REVIEW_REQUIRED"
	MappingStatusRetired        = "RETIRED"
)

var (
	// ErrConnectionNotSupplySource: the connection is not a SUPPLY_SOURCE.
	ErrConnectionNotSupplySource = errors.New("connection is not a SUPPLY_SOURCE")
	// ErrConnectionNotActive: work may only proceed on ACTIVE connections.
	ErrConnectionNotActive = errors.New("connection is not ACTIVE")
	// ErrBatchNotFound: the import batch does not exist.
	ErrBatchNotFound = errors.New("import batch not found")
	// ErrBatchNotReviewable: the batch is not in a reviewable state.
	ErrBatchNotReviewable = errors.New("import batch is not reviewable")
	// ErrRecordInDuplicateReview: a duplicate-review record cannot be approved
	// until its review case is resolved.
	ErrRecordInDuplicateReview = errors.New("record is in duplicate review; resolve the review case first")
	// ErrReviewCaseNotFound: the review case does not exist.
	ErrReviewCaseNotFound = errors.New("review case not found")
	// ErrFulfillmentNotFound: the fulfillment request does not exist.
	ErrFulfillmentNotFound = errors.New("fulfillment request not found")
	// ErrInvalidMappingDecision: approval payload is semantically invalid.
	ErrInvalidMappingDecision = errors.New("invalid mapping decision")
)

// SupplyImportBatch is one provider import pass for a SUPPLY_SOURCE connection.
type SupplyImportBatch struct {
	ID             uuid.UUID `json:"id" db:"id"`
	MerchantID     uuid.UUID `json:"merchant_id" db:"merchant_id"`
	ConnectionID   uuid.UUID `json:"connection_id" db:"connection_id"`
	Provider       Provider  `json:"provider" db:"provider"`
	BatchType      string    `json:"batch_type" db:"batch_type"`
	Status         string    `json:"status" db:"status"`
	FirstImport    bool      `json:"first_import" db:"first_import"`
	CursorToken    *string   `json:"cursor_token,omitempty" db:"cursor_token"`
	RecordCount    int       `json:"record_count" db:"record_count"`
	ApprovedCount  int       `json:"approved_count" db:"approved_count"`
	RejectedCount  int       `json:"rejected_count" db:"rejected_count"`
	DuplicateCount int       `json:"duplicate_count" db:"duplicate_count"`
	IdempotencyKey string    `json:"idempotency_key" db:"idempotency_key"`
	CorrelationID  *string   `json:"correlation_id,omitempty" db:"correlation_id"`
	CausationID    *string   `json:"causation_id,omitempty" db:"causation_id"`
	CreatedAt      time.Time `json:"created_at" db:"created_at"`
	UpdatedAt      time.Time `json:"updated_at" db:"updated_at"`
}

// SupplyImportRecord is one staged external fact inside a batch.
type SupplyImportRecord struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	BatchID           uuid.UUID       `json:"batch_id" db:"batch_id"`
	MerchantID        uuid.UUID       `json:"merchant_id" db:"merchant_id"`
	ConnectionID      uuid.UUID       `json:"connection_id" db:"connection_id"`
	EntityType        string          `json:"entity_type" db:"entity_type"`
	ExternalProductID string          `json:"external_product_id" db:"external_product_id"`
	ExternalVariantID *string         `json:"external_variant_id,omitempty" db:"external_variant_id"`
	SKU               *string         `json:"sku,omitempty" db:"sku"`
	Barcode           *string         `json:"barcode,omitempty" db:"barcode"`
	Title             *string         `json:"title,omitempty" db:"title"`
	Currency          *string         `json:"currency,omitempty" db:"currency"`
	ExternalVersion   *string         `json:"external_version,omitempty" db:"external_version"`
	ContentDigest     string          `json:"content_digest" db:"content_digest"`
	Payload           json.RawMessage `json:"payload" db:"payload"`
	Status            string          `json:"status" db:"status"`
	DuplicateOfConn   *uuid.UUID      `json:"duplicate_of_connection,omitempty" db:"duplicate_of_connection"`
	ReviewCaseID      *uuid.UUID      `json:"review_case_id,omitempty" db:"review_case_id"`
	MappingDecision   json.RawMessage `json:"mapping_decision,omitempty" db:"mapping_decision"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
}

// StagedRecordInput is one record submitted with a batch. Zero-value optional
// fields stay null; the content digest guards against re-staging identical
// facts with different identities.
type StagedRecordInput struct {
	EntityType        string          `json:"entity_type"`
	ExternalProductID string          `json:"external_product_id"`
	ExternalVariantID *string         `json:"external_variant_id,omitempty"`
	SKU               *string         `json:"sku,omitempty"`
	Barcode           *string         `json:"barcode,omitempty"`
	Title             *string         `json:"title,omitempty"`
	Currency          *string         `json:"currency,omitempty"`
	ExternalVersion   *string         `json:"external_version,omitempty"`
	ContentDigest     string          `json:"content_digest"`
	Payload           json.RawMessage `json:"payload,omitempty"`
}

// CreateSupplyImportBatchInput carries one staging submission.
type CreateSupplyImportBatchInput struct {
	MerchantID     uuid.UUID           `json:"merchant_id"`
	ConnectionID   uuid.UUID           `json:"connection_id"`
	BatchType      string              `json:"batch_type"`
	CursorToken    *string             `json:"cursor_token,omitempty"`
	IdempotencyKey string              `json:"idempotency_key"`
	Records        []StagedRecordInput `json:"records"`
}

// RecordDecision is the Merchant's review decision for one staged record.
type RecordDecision struct {
	RecordID        uuid.UUID `json:"record_id"`
	Decision        string    `json:"decision"` // APPROVE or REJECT
	InternalID      string    `json:"internal_id,omitempty"`
	AuthoritySource string    `json:"authority_source,omitempty"`
}

// ApproveSupplyImportBatchInput carries the batch review decision set.
type ApproveSupplyImportBatchInput struct {
	BatchID    uuid.UUID        `json:"batch_id"`
	MerchantID uuid.UUID        `json:"merchant_id"`
	Decisions  []RecordDecision `json:"decisions"`
}

// MerchantEntityMapping is the approved correlation between a canonical
// MatjerHub entity and an external identity, with authority and provenance.
type MerchantEntityMapping struct {
	ID                uuid.UUID       `json:"id" db:"id"`
	MerchantID        uuid.UUID       `json:"merchant_id" db:"merchant_id"`
	ConnectionID      uuid.UUID       `json:"connection_id" db:"connection_id"`
	EntityType        string          `json:"entity_type" db:"entity_type"`
	InternalID        string          `json:"internal_id" db:"internal_id"`
	ExternalProductID string          `json:"external_product_id" db:"external_product_id"`
	ExternalVariantID *string         `json:"external_variant_id,omitempty" db:"external_variant_id"`
	ExternalVersion   *string         `json:"external_version,omitempty" db:"external_version"`
	ContentDigest     *string         `json:"content_digest,omitempty" db:"content_digest"`
	AuthoritySource   string          `json:"authority_source" db:"authority_source"`
	Provenance        json.RawMessage `json:"provenance" db:"provenance"`
	Status            string          `json:"status" db:"status"`
	ApprovedBatchID   *uuid.UUID      `json:"approved_batch_id,omitempty" db:"approved_batch_id"`
	LastSyncedAt      *time.Time      `json:"last_synced_at,omitempty" db:"last_synced_at"`
	CreatedAt         time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at" db:"updated_at"`
}

// MerchantSyncCursor is the durable per-connection/per-entity sync state.
type MerchantSyncCursor struct {
	ID                 uuid.UUID  `json:"id" db:"id"`
	ConnectionID       uuid.UUID  `json:"connection_id" db:"connection_id"`
	EntityType         string     `json:"entity_type" db:"entity_type"`
	CursorToken        *string    `json:"cursor_token,omitempty" db:"cursor_token"`
	LastSuccessfulSync *time.Time `json:"last_successful_sync,omitempty" db:"last_successful_sync"`
	LastReconciledAt   *time.Time `json:"last_reconciled_at,omitempty" db:"last_reconciled_at"`
	CreatedAt          time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at" db:"updated_at"`
}

// MerchantReviewCase is one operational review case.
type MerchantReviewCase struct {
	ID              uuid.UUID       `json:"id" db:"id"`
	MerchantID      uuid.UUID       `json:"merchant_id" db:"merchant_id"`
	ConnectionID    uuid.UUID       `json:"connection_id" db:"connection_id"`
	CaseType        string          `json:"case_type" db:"case_type"`
	Status          string          `json:"status" db:"status"`
	ReasonCode      string          `json:"reason_code" db:"reason_code"`
	SubjectBatchID  *uuid.UUID      `json:"subject_batch_id,omitempty" db:"subject_batch_id"`
	SubjectRecordID *uuid.UUID      `json:"subject_record_id,omitempty" db:"subject_record_id"`
	Details         json.RawMessage `json:"details" db:"details"`
	Resolution      *string         `json:"resolution,omitempty" db:"resolution"`
	ResolvedBy      *string         `json:"resolved_by,omitempty" db:"resolved_by"`
	ResolvedAt      *time.Time      `json:"resolved_at,omitempty" db:"resolved_at"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
}

// MerchantSupplyFulfillmentRequest is one outbound supplier fulfillment work item.
type MerchantSupplyFulfillmentRequest struct {
	ID                    uuid.UUID       `json:"id" db:"id"`
	MerchantID            uuid.UUID       `json:"merchant_id" db:"merchant_id"`
	ConnectionID          uuid.UUID       `json:"connection_id" db:"connection_id"`
	IdempotencyKey        string          `json:"idempotency_key" db:"idempotency_key"`
	Status                string          `json:"status" db:"status"`
	Payload               json.RawMessage `json:"payload" db:"payload"`
	ExternalFulfillmentID *string         `json:"external_fulfillment_id,omitempty" db:"external_fulfillment_id"`
	Provider              Provider        `json:"provider" db:"provider"`
	CorrelationID         *string         `json:"correlation_id,omitempty" db:"correlation_id"`
	CausationID           *string         `json:"causation_id,omitempty" db:"causation_id"`
	CreatedAt             time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at" db:"updated_at"`
}

// MerchantSupplyTrackingEvent is one imported external fulfillment/tracking state.
type MerchantSupplyTrackingEvent struct {
	ID              uuid.UUID       `json:"id" db:"id"`
	RequestID       uuid.UUID       `json:"request_id" db:"request_id"`
	ConnectionID    uuid.UUID       `json:"connection_id" db:"connection_id"`
	ExternalEventID string          `json:"external_event_id" db:"external_event_id"`
	Status          string          `json:"status" db:"status"`
	Carrier         *string         `json:"carrier,omitempty" db:"carrier"`
	TrackingNumber  *string         `json:"tracking_number,omitempty" db:"tracking_number"`
	OccurredAt      *time.Time      `json:"occurred_at,omitempty" db:"occurred_at"`
	Payload         json.RawMessage `json:"payload" db:"payload"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
}

// MerchantWebhookInboxItem is one deduplicated inbound provider webhook.
type MerchantWebhookInboxItem struct {
	ID             uuid.UUID       `json:"id" db:"id"`
	ConnectionID   *uuid.UUID      `json:"connection_id,omitempty" db:"connection_id"`
	Provider       Provider        `json:"provider" db:"provider"`
	EventType      string          `json:"event_type" db:"event_type"`
	IdempotencyKey string          `json:"idempotency_key" db:"idempotency_key"`
	Payload        json.RawMessage `json:"payload" db:"payload"`
	Status         string          `json:"status" db:"status"`
	ErrorMessage   *string         `json:"error_message,omitempty" db:"error_message"`
	ReceivedAt     time.Time       `json:"received_at" db:"received_at"`
	ProcessedAt    *time.Time      `json:"processed_at,omitempty" db:"processed_at"`
}

// CreateFulfillmentRequestInput is the Core-side creation of an outbound
// fulfillment request. Payload carries fulfillment-necessity facts only.
type CreateFulfillmentRequestInput struct {
	MerchantID     uuid.UUID       `json:"merchant_id"`
	ConnectionID   uuid.UUID       `json:"connection_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	CorrelationID  string          `json:"correlation_id,omitempty"`
	CausationID    string          `json:"causation_id,omitempty"`
}

// RecordTrackingEventInput is one inbound tracking update.
type RecordTrackingEventInput struct {
	RequestID       uuid.UUID       `json:"request_id"`
	ConnectionID    uuid.UUID       `json:"connection_id"`
	ExternalEventID string          `json:"external_event_id"`
	Status          string          `json:"status"`
	Carrier         *string         `json:"carrier,omitempty"`
	TrackingNumber  *string         `json:"tracking_number,omitempty"`
	OccurredAt      *time.Time      `json:"occurred_at,omitempty"`
	Payload         json.RawMessage `json:"payload,omitempty"`
}

// RecordWebhookInboxInput is one inbound webhook for deduplicated storage.
type RecordWebhookInboxInput struct {
	ConnectionID   *uuid.UUID      `json:"connection_id,omitempty"`
	Provider       Provider        `json:"provider"`
	EventType      string          `json:"event_type"`
	IdempotencyKey string          `json:"idempotency_key"`
	Payload        json.RawMessage `json:"payload,omitempty"`
}

// SupplyService is the Core-owned authority for the I2 Supply pipeline state.
type SupplyService interface {
	CreateImportBatch(ctx context.Context, input CreateSupplyImportBatchInput, correlationID, causationID string) (*SupplyImportBatch, []SupplyImportRecord, error)
	ApproveImportBatch(ctx context.Context, input ApproveSupplyImportBatchInput, correlationID, causationID string) (*SupplyImportBatch, error)
	GetImportBatch(ctx context.Context, batchID uuid.UUID) (*SupplyImportBatch, []SupplyImportRecord, error)
	ListReviewCases(ctx context.Context, merchantID uuid.UUID, connectionID *uuid.UUID, status string) ([]MerchantReviewCase, error)
	ResolveReviewCase(ctx context.Context, caseID uuid.UUID, merchantID uuid.UUID, resolution string, resolvedBy string) (*MerchantReviewCase, error)
	UpsertSyncCursor(ctx context.Context, cursor MerchantSyncCursor) (*MerchantSyncCursor, error)
	GetSyncCursor(ctx context.Context, connectionID uuid.UUID, entityType string) (*MerchantSyncCursor, error)
	GetMappingByExternalID(ctx context.Context, connectionID uuid.UUID, entityType, externalProductID string, externalVariantID *string) (*MerchantEntityMapping, error)
	ListMappings(ctx context.Context, connectionID uuid.UUID) ([]MerchantEntityMapping, error)
	CreateFulfillmentRequest(ctx context.Context, input CreateFulfillmentRequestInput) (*MerchantSupplyFulfillmentRequest, error)
	GetFulfillmentRequest(ctx context.Context, id uuid.UUID) (*MerchantSupplyFulfillmentRequest, error)
	UpdateFulfillmentRequestStatus(ctx context.Context, id uuid.UUID, status string, externalFulfillmentID *string) (*MerchantSupplyFulfillmentRequest, error)
	RecordTrackingEvent(ctx context.Context, input RecordTrackingEventInput, correlationID, causationID string) (*MerchantSupplyTrackingEvent, bool, error)
	RecordWebhookInbox(ctx context.Context, input RecordWebhookInboxInput) (*MerchantWebhookInboxItem, bool, error)
	GetConnectionForWork(ctx context.Context, connectionID uuid.UUID) (*MerchantIntegrationConnection, error)
}

type supplyService struct {
	pool interface {
		Begin(ctx context.Context) (pgx.Tx, error)
	}
	store outbox.Store
}

// NewSupplyService builds the Supply pipeline authority on a pgx pool.
func NewSupplyService(pool Pool) SupplyService {
	return &supplyService{pool: pool, store: outbox.NewStore()}
}

// Pool is the minimal database surface the service needs (satisfied by
// *pgxpool.Pool); declared here to keep the service testable.
type Pool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

const qGetMerchantConnection = `
SELECT id, merchant_id, connection_type, provider, status
FROM merchant_integration_connections
WHERE id = $1`

type connRow struct {
	ID             uuid.UUID
	MerchantID     uuid.UUID
	ConnectionType MerchantConnectionType
	Provider       Provider
	Status         MerchantConnectionStatus
}

func (s *supplyService) getConnectionForWork(ctx context.Context, tx pgx.Tx, connectionID uuid.UUID) (*connRow, error) {
	row := tx.QueryRow(ctx, qGetMerchantConnection, connectionID)
	var c connRow
	if err := row.Scan(&c.ID, &c.MerchantID, &c.ConnectionType, &c.Provider, &c.Status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrConnectionNotFound
		}
		return nil, fmt.Errorf("query connection: %w", err)
	}
	return &c, nil
}

// GetConnectionForWork resolves a connection for pipeline work after verifying
// it is an ACTIVE SUPPLY_SOURCE owned by the expected Merchant scope.
func (s *supplyService) GetConnectionForWork(ctx context.Context, connectionID uuid.UUID) (*MerchantIntegrationConnection, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	c, err := s.getConnectionForWork(ctx, tx, connectionID)
	if err != nil {
		return nil, err
	}
	if c.ConnectionType != ConnectionTypeSupplySource {
		return nil, ErrConnectionNotSupplySource
	}
	if c.Status != MerchantStatusActive {
		return nil, ErrConnectionNotActive
	}
	full, err := s.getMerchantConnectionFull(ctx, tx, c.ID)
	if err != nil {
		return nil, err
	}
	return full, tx.Commit(ctx)
}

func (s *supplyService) getMerchantConnectionFull(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*MerchantIntegrationConnection, error) {
	rows, err := tx.Query(ctx, `
SELECT id, merchant_id, connection_type, provider, external_account_id, name, store_id, status,
       granted_scopes, health_status, last_health_check_at, first_successful_sync_at,
       legacy_actor_type, legacy_actor_id, legacy_connection_id, settings, created_at, updated_at
FROM merchant_integration_connections WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("query connection full: %w", err)
	}
	defer rows.Close()
	conns := make([]MerchantIntegrationConnection, 0, 1)
	for rows.Next() {
		c, err := scanMerchantConnectionFromRows(rows)
		if err != nil {
			return nil, err
		}
		conns = append(conns, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connections: %w", err)
	}
	if len(conns) == 0 {
		return nil, ErrConnectionNotFound
	}
	return &conns[0], nil
}

// CreateImportBatch stages one provider import. First-import batches always
// land in staging; records whose SKU or barcode already appears under a
// different connection of the same Merchant enter DUPLICATE_REVIEW with a
// review case. Nothing is published automatically.
func (s *supplyService) CreateImportBatch(ctx context.Context, input CreateSupplyImportBatchInput, correlationID, causationID string) (*SupplyImportBatch, []SupplyImportRecord, error) {
	if input.MerchantID == uuid.Nil || input.ConnectionID == uuid.Nil {
		return nil, nil, ErrInvalidMappingDecision
	}
	if input.IdempotencyKey == "" {
		return nil, nil, errors.New("idempotency_key is required")
	}
	batchType := input.BatchType
	if batchType == "" {
		batchType = SupplyBatchTypeFirstImport
	}
	for _, r := range input.Records {
		if r.EntityType != SupplyEntityTypeProduct && r.EntityType != SupplyEntityTypeVariant &&
			r.EntityType != SupplyEntityTypePrice && r.EntityType != SupplyEntityTypeInventory {
			return nil, nil, fmt.Errorf("invalid entity_type %q", r.EntityType)
		}
		if r.ExternalProductID == "" {
			return nil, nil, errors.New("external_product_id is required")
		}
		if r.ContentDigest == "" {
			return nil, nil, errors.New("content_digest is required")
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	c, err := s.getConnectionForWork(ctx, tx, input.ConnectionID)
	if err != nil {
		return nil, nil, err
	}
	if c.MerchantID != input.MerchantID {
		// Cross-merchant access is indistinguishable from a missing connection.
		return nil, nil, ErrConnectionNotFound
	}
	if c.ConnectionType != ConnectionTypeSupplySource {
		return nil, nil, ErrConnectionNotSupplySource
	}
	if c.Status != MerchantStatusActive {
		return nil, nil, ErrConnectionNotActive
	}

	// Idempotency: a retry of the same submission returns the existing batch.
	existing, recs, err := findBatchByIdempotency(ctx, tx, input.ConnectionID, input.IdempotencyKey)
	if err != nil {
		return nil, nil, err
	}
	if existing != nil {
		return existing, recs, tx.Commit(ctx)
	}

	firstImport := false
	var priorBatches int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM merchant_integration_supply_import_batches WHERE connection_id = $1`,
		input.ConnectionID).Scan(&priorBatches); err != nil {
		return nil, nil, fmt.Errorf("count prior batches: %w", err)
	}
	firstImport = priorBatches == 0

	var batch SupplyImportBatch
	err = tx.QueryRow(ctx, `
INSERT INTO merchant_integration_supply_import_batches
    (merchant_id, connection_id, provider, batch_type, status, first_import, cursor_token,
     record_count, idempotency_key, correlation_id, causation_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id, merchant_id, connection_id, provider, batch_type, status, first_import, cursor_token,
          record_count, approved_count, rejected_count, duplicate_count, idempotency_key,
          correlation_id, causation_id, created_at, updated_at`,
		input.MerchantID, input.ConnectionID, c.Provider, batchType, SupplyBatchStatusStaged, firstImport,
		input.CursorToken, len(input.Records), input.IdempotencyKey, correlationID, causationID,
	).Scan(&batch.ID, &batch.MerchantID, &batch.ConnectionID, &batch.Provider, &batch.BatchType,
		&batch.Status, &batch.FirstImport, &batch.CursorToken, &batch.RecordCount, &batch.ApprovedCount,
		&batch.RejectedCount, &batch.DuplicateCount, &batch.IdempotencyKey, &batch.CorrelationID,
		&batch.CausationID, &batch.CreatedAt, &batch.UpdatedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("insert batch: %w", err)
	}

	records := make([]SupplyImportRecord, 0, len(input.Records))
	duplicates := 0
	for _, r := range input.Records {
		rec, _, err := stageRecord(ctx, tx, batch, r)
		if err != nil {
			return nil, nil, err
		}
		if rec.Status == SupplyRecordStatusDuplicateReview && rec.ReviewCaseID == nil {
			duplicates++
			if err := s.createDuplicateReviewCase(ctx, tx, batch, rec, rec.DuplicateOfConn); err != nil {
				return nil, nil, err
			}
		}
		records = append(records, *rec)
	}

	if duplicates > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE merchant_integration_supply_import_batches SET duplicate_count = $1, updated_at = NOW() WHERE id = $2`,
			duplicates, batch.ID); err != nil {
			return nil, nil, fmt.Errorf("update duplicate count: %w", err)
		}
		batch.DuplicateCount = duplicates
	}

	envelope := events.EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypeSupplyBatchStaged,
		SchemaVersion:    1,
		AggregateType:    "merchant_integration_supply_import_batch",
		AggregateID:      batch.ID.String(),
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       time.Now().UTC(),
		Payload: map[string]any{
			"batch_id":        batch.ID.String(),
			"merchant_id":     batch.MerchantID.String(),
			"connection_id":   batch.ConnectionID.String(),
			"provider":        string(batch.Provider),
			"batch_type":      batch.BatchType,
			"first_import":    batch.FirstImport,
			"record_count":    batch.RecordCount,
			"duplicate_count": batch.DuplicateCount,
		},
	}
	if err := s.store.Enqueue(ctx, tx, envelope); err != nil {
		return nil, nil, fmt.Errorf("enqueue batch staged event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("commit batch: %w", err)
	}
	return &batch, records, nil
}

// stageRecord upserts one staged record and detects cross-connection SKU or
// barcode duplication within the same Merchant. Same-connection retries of an
// identical external identity upsert in place (restart safety); different
// content for an already-APPROVED identity returns to REVIEW_REQUIRED.
func stageRecord(ctx context.Context, tx pgx.Tx, batch SupplyImportBatch, r StagedRecordInput) (*SupplyImportRecord, *uuid.UUID, error) {
	// Cross-source duplicate detection: the same Merchant staged the same SKU
	// or barcode under a different supply connection.
	var dupConn *uuid.UUID
	var dupConnectionID uuid.UUID
	status := SupplyRecordStatusStaged
	if r.SKU != nil && *r.SKU != "" {
		err := tx.QueryRow(ctx, `
SELECT connection_id FROM merchant_integration_supply_import_records
WHERE merchant_id = $1 AND connection_id <> $2 AND sku = $3
LIMIT 1`, batch.MerchantID, batch.ConnectionID, *r.SKU).Scan(&dupConnectionID)
		switch {
		case err == nil:
			cc := dupConnectionID
			dupConn = &cc
			status = SupplyRecordStatusDuplicateReview
		case errors.Is(err, pgx.ErrNoRows):
			// no SKU duplicate
		default:
			return nil, nil, fmt.Errorf("detect sku duplicate: %w", err)
		}
	}
	if status == SupplyRecordStatusStaged && r.Barcode != nil && *r.Barcode != "" {
		err := tx.QueryRow(ctx, `
SELECT connection_id FROM merchant_integration_supply_import_records
WHERE merchant_id = $1 AND connection_id <> $2 AND barcode = $3
LIMIT 1`, batch.MerchantID, batch.ConnectionID, *r.Barcode).Scan(&dupConnectionID)
		switch {
		case err == nil:
			cc := dupConnectionID
			dupConn = &cc
			status = SupplyRecordStatusDuplicateReview
		case errors.Is(err, pgx.ErrNoRows):
			// no barcode duplicate
		default:
			return nil, nil, fmt.Errorf("detect barcode duplicate: %w", err)
		}
	}

	var rec SupplyImportRecord
	err := tx.QueryRow(ctx, `
INSERT INTO merchant_integration_supply_import_records
    (batch_id, merchant_id, connection_id, entity_type, external_product_id, external_variant_id,
     sku, barcode, title, currency, external_version, content_digest, payload, status, duplicate_of_connection)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT (connection_id, entity_type, external_product_id, COALESCE(external_variant_id, '')) DO UPDATE SET
    batch_id = EXCLUDED.batch_id,
    content_digest = EXCLUDED.content_digest,
    payload = EXCLUDED.payload,
    external_version = EXCLUDED.external_version,
    sku = EXCLUDED.sku,
    barcode = EXCLUDED.barcode,
    title = EXCLUDED.title,
    currency = EXCLUDED.currency,
    updated_at = NOW()
RETURNING id, batch_id, merchant_id, connection_id, entity_type, external_product_id, external_variant_id,
          sku, barcode, title, currency, external_version, content_digest, payload, status,
          duplicate_of_connection, review_case_id, mapping_decision, created_at, updated_at`,
		batch.ID, batch.MerchantID, batch.ConnectionID, r.EntityType, r.ExternalProductID, r.ExternalVariantID,
		r.SKU, r.Barcode, r.Title, r.Currency, r.ExternalVersion, r.ContentDigest, orEmptyJSON(r.Payload),
		status, dupConn,
	).Scan(&rec.ID, &rec.BatchID, &rec.MerchantID, &rec.ConnectionID, &rec.EntityType, &rec.ExternalProductID,
		&rec.ExternalVariantID, &rec.SKU, &rec.Barcode, &rec.Title, &rec.Currency, &rec.ExternalVersion,
		&rec.ContentDigest, &rec.Payload, &rec.Status, &rec.DuplicateOfConn, &rec.ReviewCaseID,
		&rec.MappingDecision, &rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		return nil, nil, fmt.Errorf("upsert staged record: %w", err)
	}
	return &rec, rec.DuplicateOfConn, nil
}

// createDuplicateReviewCase opens one review case per duplicate record.
func (s *supplyService) createDuplicateReviewCase(ctx context.Context, tx pgx.Tx, batch SupplyImportBatch, rec *SupplyImportRecord, dupConn *uuid.UUID) error {
	reason := ReviewReasonDuplicateSKU
	if rec.Barcode != nil && *rec.Barcode != "" && (rec.SKU == nil || *rec.SKU == "") {
		reason = ReviewReasonDuplicateBarcode
	}
	details := map[string]any{
		"entity_type":         rec.EntityType,
		"external_product_id": rec.ExternalProductID,
		"sku":                 rec.SKU,
		"barcode":             rec.Barcode,
	}
	if dupConn != nil {
		details["duplicate_of_connection"] = dupConn.String()
	}
	detailsJSON, _ := json.Marshal(details)

	var caseID uuid.UUID
	if err := tx.QueryRow(ctx, `
INSERT INTO merchant_integration_review_cases
    (merchant_id, connection_id, case_type, status, reason_code, subject_batch_id, subject_record_id, details)
VALUES ($1, $2, 'DUPLICATE_CANDIDATE', $3, $4, $5, $6, $7)
RETURNING id`,
		batch.MerchantID, batch.ConnectionID, ReviewCaseStatusOpen, reason, batch.ID, rec.ID, detailsJSON,
	).Scan(&caseID); err != nil {
		return fmt.Errorf("insert review case: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE merchant_integration_supply_import_records SET review_case_id = $1, updated_at = NOW() WHERE id = $2`,
		caseID, rec.ID); err != nil {
		return fmt.Errorf("link review case: %w", err)
	}
	rec.ReviewCaseID = &caseID
	return nil
}

// ApproveImportBatch applies the Merchant's per-record decisions. Approved
// records gain durable entity mappings with authority and provenance; records
// still in duplicate review cannot be approved until their case is resolved.
// Nothing here publishes to the canonical catalog — that projection belongs to
// the unified-catalog phase.
func (s *supplyService) ApproveImportBatch(ctx context.Context, input ApproveSupplyImportBatchInput, correlationID, causationID string) (*SupplyImportBatch, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	var batch SupplyImportBatch
	err = tx.QueryRow(ctx, `
SELECT id, merchant_id, connection_id, provider, batch_type, status, first_import, cursor_token,
       record_count, approved_count, rejected_count, duplicate_count, idempotency_key,
       correlation_id, causation_id, created_at, updated_at
FROM merchant_integration_supply_import_batches WHERE id = $1`, input.BatchID).
		Scan(&batch.ID, &batch.MerchantID, &batch.ConnectionID, &batch.Provider, &batch.BatchType,
			&batch.Status, &batch.FirstImport, &batch.CursorToken, &batch.RecordCount, &batch.ApprovedCount,
			&batch.RejectedCount, &batch.DuplicateCount, &batch.IdempotencyKey, &batch.CorrelationID,
			&batch.CausationID, &batch.CreatedAt, &batch.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBatchNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query batch: %w", err)
	}
	if input.MerchantID != batch.MerchantID {
		return nil, ErrBatchNotFound
	}
	if batch.Status != SupplyBatchStatusStaged && batch.Status != SupplyBatchStatusInReview {
		return nil, ErrBatchNotReviewable
	}

	approved, rejected := 0, 0
	for _, d := range input.Decisions {
		switch d.Decision {
		case "APPROVE":
			if d.InternalID == "" {
				return nil, fmt.Errorf("%w: internal_id is required to approve record %s", ErrInvalidMappingDecision, d.RecordID)
			}
			authority := d.AuthoritySource
			if authority == "" {
				authority = MappingAuthorityExternal
			}
			if authority != MappingAuthorityExternal && authority != MappingAuthorityMatjerHub && authority != MappingAuthorityManual {
				return nil, fmt.Errorf("%w: unknown authority_source %q", ErrInvalidMappingDecision, authority)
			}
			var recStatus string
			var reviewCase *uuid.UUID
			if err := tx.QueryRow(ctx,
				`SELECT status, review_case_id FROM merchant_integration_supply_import_records WHERE id = $1 AND batch_id = $2`,
				d.RecordID, batch.ID).Scan(&recStatus, &reviewCase); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, fmt.Errorf("%w: record %s not in batch", ErrInvalidMappingDecision, d.RecordID)
				}
				return nil, fmt.Errorf("query record: %w", err)
			}
			if recStatus == SupplyRecordStatusDuplicateReview && reviewCase != nil {
				return nil, ErrRecordInDuplicateReview
			}
			if _, err := tx.Exec(ctx,
				`UPDATE merchant_integration_supply_import_records SET status = $1, mapping_decision = $2, updated_at = NOW() WHERE id = $3`,
				SupplyRecordStatusApproved, mustJSON(map[string]any{
					"internal_id":      d.InternalID,
					"authority_source": authority,
				}), d.RecordID); err != nil {
				return nil, fmt.Errorf("approve record: %w", err)
			}
			if err := upsertApprovedMapping(ctx, tx, batch, d, authority); err != nil {
				return nil, err
			}
			approved++
		case "REJECT":
			if _, err := tx.Exec(ctx,
				`UPDATE merchant_integration_supply_import_records SET status = $1, updated_at = NOW() WHERE id = $2 AND batch_id = $3`,
				SupplyRecordStatusRejected, d.RecordID, batch.ID); err != nil {
				return nil, fmt.Errorf("reject record: %w", err)
			}
			rejected++
		default:
			return nil, fmt.Errorf("%w: unknown decision %q", ErrInvalidMappingDecision, d.Decision)
		}
	}

	newStatus := SupplyBatchStatusApproved
	if approved == 0 {
		newStatus = SupplyBatchStatusRejected
	} else if rejected > 0 || approved < batch.RecordCount {
		newStatus = SupplyBatchStatusPartiallyApproved
	}
	if _, err := tx.Exec(ctx,
		`UPDATE merchant_integration_supply_import_batches SET status = $1, approved_count = $2, rejected_count = $3, updated_at = NOW() WHERE id = $4`,
		newStatus, approved, rejected, batch.ID); err != nil {
		return nil, fmt.Errorf("update batch status: %w", err)
	}
	batch.Status = newStatus
	batch.ApprovedCount = approved
	batch.RejectedCount = rejected

	envelope := events.EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypeSupplyBatchApproved,
		SchemaVersion:    1,
		AggregateType:    "merchant_integration_supply_import_batch",
		AggregateID:      batch.ID.String(),
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       time.Now().UTC(),
		Payload: map[string]any{
			"batch_id":       batch.ID.String(),
			"merchant_id":    batch.MerchantID.String(),
			"connection_id":  batch.ConnectionID.String(),
			"provider":       string(batch.Provider),
			"status":         batch.Status,
			"approved_count": approved,
			"rejected_count": rejected,
		},
	}
	if err := s.store.Enqueue(ctx, tx, envelope); err != nil {
		return nil, fmt.Errorf("enqueue batch approved event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit approval: %w", err)
	}
	return &batch, nil
}

func upsertApprovedMapping(ctx context.Context, tx pgx.Tx, batch SupplyImportBatch, d RecordDecision, authority string) error {
	var entityType string
	var externalProductID string
	var externalVariantID *string
	var externalVersion *string
	var digest string
	if err := tx.QueryRow(ctx, `
SELECT entity_type, external_product_id, external_variant_id, external_version, content_digest
FROM merchant_integration_supply_import_records WHERE id = $1`, d.RecordID).
		Scan(&entityType, &externalProductID, &externalVariantID, &externalVersion, &digest); err != nil {
		return fmt.Errorf("query record for mapping: %w", err)
	}
	provenance, _ := json.Marshal(map[string]any{
		"approved_batch_id":   batch.ID.String(),
		"connection_id":       batch.ConnectionID.String(),
		"external_product_id": externalProductID,
	})
	if _, err := tx.Exec(ctx, `
INSERT INTO merchant_integration_entity_mappings
    (merchant_id, connection_id, entity_type, internal_id, external_product_id, external_variant_id,
     external_version, content_digest, authority_source, provenance, status, approved_batch_id, last_synced_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
ON CONFLICT (connection_id, entity_type, external_product_id, COALESCE(external_variant_id, '')) DO UPDATE SET
    internal_id = EXCLUDED.internal_id,
    authority_source = EXCLUDED.authority_source,
    external_version = EXCLUDED.external_version,
    content_digest = EXCLUDED.content_digest,
    status = 'ACTIVE',
    approved_batch_id = EXCLUDED.approved_batch_id,
    last_synced_at = NOW(),
    updated_at = NOW()`,
		batch.MerchantID, batch.ConnectionID, entityType, d.InternalID, externalProductID, externalVariantID,
		externalVersion, digest, authority, provenance, MappingStatusActive, batch.ID); err != nil {
		return fmt.Errorf("upsert approved mapping: %w", err)
	}
	return nil
}

// ResolveReviewCase resolves or dismisses one review case. Resolving returns
// the subject record to STAGED (reviewable) or REJECTED; approval still flows
// through ApproveImportBatch. Identity and authority decisions are never
// auto-applied by reconciliation or by this resolution path.
func (s *supplyService) ResolveReviewCase(ctx context.Context, caseID uuid.UUID, merchantID uuid.UUID, resolution string, resolvedBy string) (*MerchantReviewCase, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	var c MerchantReviewCase
	var subjectRecordID *uuid.UUID
	err = tx.QueryRow(ctx, `
SELECT id, merchant_id, connection_id, case_type, status, reason_code, subject_batch_id, subject_record_id,
       details, resolution, resolved_by, resolved_at, created_at, updated_at
FROM merchant_integration_review_cases WHERE id = $1`, caseID).
		Scan(&c.ID, &c.MerchantID, &c.ConnectionID, &c.CaseType, &c.Status, &c.ReasonCode, &c.SubjectBatchID,
			&subjectRecordID, &c.Details, &c.Resolution, &c.ResolvedBy, &c.ResolvedAt, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrReviewCaseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query review case: %w", err)
	}
	if c.MerchantID != merchantID {
		return nil, ErrReviewCaseNotFound
	}
	if c.Status != ReviewCaseStatusOpen {
		return nil, fmt.Errorf("review case is not open")
	}

	var caseStatus, recordStatus string
	switch resolution {
	case "KEEP_SEPARATE":
		caseStatus = ReviewCaseStatusResolved
		recordStatus = SupplyRecordStatusStaged
	case "REJECT_RECORD":
		caseStatus = ReviewCaseStatusResolved
		recordStatus = SupplyRecordStatusRejected
	case "DISMISS":
		caseStatus = ReviewCaseStatusDismissed
		recordStatus = SupplyRecordStatusStaged
	default:
		return nil, fmt.Errorf("%w: unknown resolution %q", ErrInvalidMappingDecision, resolution)
	}

	now := time.Now().UTC()
	if _, err := tx.Exec(ctx,
		`UPDATE merchant_integration_review_cases SET status = $1, resolution = $2, resolved_by = $3, resolved_at = $4, updated_at = NOW() WHERE id = $5`,
		caseStatus, resolution, resolvedBy, now, caseID); err != nil {
		return nil, fmt.Errorf("resolve review case: %w", err)
	}
	if subjectRecordID != nil {
		if _, err := tx.Exec(ctx,
			`UPDATE merchant_integration_supply_import_records SET status = $1, updated_at = NOW() WHERE id = $2`,
			recordStatus, *subjectRecordID); err != nil {
			return nil, fmt.Errorf("update subject record: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit resolution: %w", err)
	}

	c.Status = caseStatus
	c.Resolution = &resolution
	c.ResolvedBy = &resolvedBy
	c.ResolvedAt = &now
	return &c, nil
}

func (s *supplyService) GetImportBatch(ctx context.Context, batchID uuid.UUID) (*SupplyImportBatch, []SupplyImportRecord, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	var batch SupplyImportBatch
	err = tx.QueryRow(ctx, `
SELECT id, merchant_id, connection_id, provider, batch_type, status, first_import, cursor_token,
       record_count, approved_count, rejected_count, duplicate_count, idempotency_key,
       correlation_id, causation_id, created_at, updated_at
FROM merchant_integration_supply_import_batches WHERE id = $1`, batchID).
		Scan(&batch.ID, &batch.MerchantID, &batch.ConnectionID, &batch.Provider, &batch.BatchType,
			&batch.Status, &batch.FirstImport, &batch.CursorToken, &batch.RecordCount, &batch.ApprovedCount,
			&batch.RejectedCount, &batch.DuplicateCount, &batch.IdempotencyKey, &batch.CorrelationID,
			&batch.CausationID, &batch.CreatedAt, &batch.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrBatchNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("query batch: %w", err)
	}

	rows, err := tx.Query(ctx, `
SELECT id, batch_id, merchant_id, connection_id, entity_type, external_product_id, external_variant_id,
       sku, barcode, title, currency, external_version, content_digest, payload, status,
       duplicate_of_connection, review_case_id, mapping_decision, created_at, updated_at
FROM merchant_integration_supply_import_records WHERE batch_id = $1 ORDER BY created_at, id`, batchID)
	if err != nil {
		return nil, nil, fmt.Errorf("query records: %w", err)
	}
	defer rows.Close()
	var records []SupplyImportRecord
	for rows.Next() {
		var rec SupplyImportRecord
		if err := rows.Scan(&rec.ID, &rec.BatchID, &rec.MerchantID, &rec.ConnectionID, &rec.EntityType,
			&rec.ExternalProductID, &rec.ExternalVariantID, &rec.SKU, &rec.Barcode, &rec.Title, &rec.Currency,
			&rec.ExternalVersion, &rec.ContentDigest, &rec.Payload, &rec.Status, &rec.DuplicateOfConn,
			&rec.ReviewCaseID, &rec.MappingDecision, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, nil, fmt.Errorf("scan record: %w", err)
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate records: %w", err)
	}
	return &batch, records, tx.Commit(ctx)
}

func (s *supplyService) ListReviewCases(ctx context.Context, merchantID uuid.UUID, connectionID *uuid.UUID, status string) ([]MerchantReviewCase, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	rows, err := tx.Query(ctx, `
SELECT id, merchant_id, connection_id, case_type, status, reason_code, subject_batch_id, subject_record_id,
       details, resolution, resolved_by, resolved_at, created_at, updated_at
FROM merchant_integration_review_cases
WHERE merchant_id = $1 AND ($2::uuid IS NULL OR connection_id = $2) AND ($3::text IS NULL OR status = $3)
ORDER BY created_at DESC, id`,
		merchantID, connectionID, nullableString(status))
	if err != nil {
		return nil, fmt.Errorf("query review cases: %w", err)
	}
	defer rows.Close()
	var cases []MerchantReviewCase
	for rows.Next() {
		var c MerchantReviewCase
		var subjectRecordID *uuid.UUID
		if err := rows.Scan(&c.ID, &c.MerchantID, &c.ConnectionID, &c.CaseType, &c.Status, &c.ReasonCode,
			&c.SubjectBatchID, &subjectRecordID, &c.Details, &c.Resolution, &c.ResolvedBy, &c.ResolvedAt,
			&c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan review case: %w", err)
		}
		c.SubjectRecordID = subjectRecordID
		cases = append(cases, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate review cases: %w", err)
	}
	return cases, tx.Commit(ctx)
}

func (s *supplyService) UpsertSyncCursor(ctx context.Context, cursor MerchantSyncCursor) (*MerchantSyncCursor, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	var out MerchantSyncCursor
	err = tx.QueryRow(ctx, `
INSERT INTO merchant_integration_sync_cursors (connection_id, entity_type, cursor_token, last_successful_sync, last_reconciled_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (connection_id, entity_type) DO UPDATE SET
    cursor_token = EXCLUDED.cursor_token,
    last_successful_sync = COALESCE(EXCLUDED.last_successful_sync, merchant_integration_sync_cursors.last_successful_sync),
    last_reconciled_at = COALESCE(EXCLUDED.last_reconciled_at, merchant_integration_sync_cursors.last_reconciled_at),
    updated_at = NOW()
RETURNING id, connection_id, entity_type, cursor_token, last_successful_sync, last_reconciled_at, created_at, updated_at`,
		cursor.ConnectionID, cursor.EntityType, cursor.CursorToken, cursor.LastSuccessfulSync, cursor.LastReconciledAt,
	).Scan(&out.ID, &out.ConnectionID, &out.EntityType, &out.CursorToken, &out.LastSuccessfulSync,
		&out.LastReconciledAt, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("upsert cursor: %w", err)
	}
	return &out, tx.Commit(ctx)
}

func (s *supplyService) GetSyncCursor(ctx context.Context, connectionID uuid.UUID, entityType string) (*MerchantSyncCursor, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	var out MerchantSyncCursor
	err = tx.QueryRow(ctx, `
SELECT id, connection_id, entity_type, cursor_token, last_successful_sync, last_reconciled_at, created_at, updated_at
FROM merchant_integration_sync_cursors WHERE connection_id = $1 AND entity_type = $2`,
		connectionID, entityType).Scan(&out.ID, &out.ConnectionID, &out.EntityType, &out.CursorToken,
		&out.LastSuccessfulSync, &out.LastReconciledAt, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query cursor: %w", err)
	}
	return &out, tx.Commit(ctx)
}

func (s *supplyService) GetMappingByExternalID(ctx context.Context, connectionID uuid.UUID, entityType, externalProductID string, externalVariantID *string) (*MerchantEntityMapping, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	m, err := queryMapping(ctx, tx, connectionID, entityType, externalProductID, externalVariantID)
	if err != nil {
		return nil, err
	}
	return m, tx.Commit(ctx)
}

func queryMapping(ctx context.Context, tx pgx.Tx, connectionID uuid.UUID, entityType, externalProductID string, externalVariantID *string) (*MerchantEntityMapping, error) {
	var m MerchantEntityMapping
	err := tx.QueryRow(ctx, `
SELECT id, merchant_id, connection_id, entity_type, internal_id, external_product_id, external_variant_id,
       external_version, content_digest, authority_source, provenance, status, approved_batch_id,
       last_synced_at, created_at, updated_at
FROM merchant_integration_entity_mappings
WHERE connection_id = $1 AND entity_type = $2 AND external_product_id = $3
  AND COALESCE(external_variant_id, '') = COALESCE($4, '')`,
		connectionID, entityType, externalProductID, externalVariantID).
		Scan(&m.ID, &m.MerchantID, &m.ConnectionID, &m.EntityType, &m.InternalID, &m.ExternalProductID,
			&m.ExternalVariantID, &m.ExternalVersion, &m.ContentDigest, &m.AuthoritySource, &m.Provenance,
			&m.Status, &m.ApprovedBatchID, &m.LastSyncedAt, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query mapping: %w", err)
	}
	return &m, nil
}

func (s *supplyService) ListMappings(ctx context.Context, connectionID uuid.UUID) ([]MerchantEntityMapping, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	rows, err := tx.Query(ctx, `
SELECT id, merchant_id, connection_id, entity_type, internal_id, external_product_id, external_variant_id,
       external_version, content_digest, authority_source, provenance, status, approved_batch_id,
       last_synced_at, created_at, updated_at
FROM merchant_integration_entity_mappings WHERE connection_id = $1 ORDER BY created_at, id`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("query mappings: %w", err)
	}
	defer rows.Close()
	var out []MerchantEntityMapping
	for rows.Next() {
		var m MerchantEntityMapping
		if err := rows.Scan(&m.ID, &m.MerchantID, &m.ConnectionID, &m.EntityType, &m.InternalID,
			&m.ExternalProductID, &m.ExternalVariantID, &m.ExternalVersion, &m.ContentDigest, &m.AuthoritySource,
			&m.Provenance, &m.Status, &m.ApprovedBatchID, &m.LastSyncedAt, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan mapping: %w", err)
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mappings: %w", err)
	}
	return out, tx.Commit(ctx)
}

// CreateFulfillmentRequest records an outbound supplier fulfillment request
// idempotently. A retry with the same (connection, idempotency_key) returns the
// existing request without re-emitting the event.
func (s *supplyService) CreateFulfillmentRequest(ctx context.Context, input CreateFulfillmentRequestInput) (*MerchantSupplyFulfillmentRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	c, err := s.getConnectionForWork(ctx, tx, input.ConnectionID)
	if err != nil {
		return nil, err
	}
	if c.MerchantID != input.MerchantID {
		return nil, ErrConnectionNotFound
	}

	var req MerchantSupplyFulfillmentRequest
	err = tx.QueryRow(ctx, `
INSERT INTO merchant_integration_supply_fulfillment_requests
    (merchant_id, connection_id, idempotency_key, status, payload, provider, correlation_id, causation_id)
VALUES ($1, $2, $3, 'REQUESTED', $4, $5, $6, $7)
ON CONFLICT (connection_id, idempotency_key) DO NOTHING
RETURNING id, merchant_id, connection_id, idempotency_key, status, payload, external_fulfillment_id,
          provider, correlation_id, causation_id, created_at, updated_at`,
		input.MerchantID, input.ConnectionID, input.IdempotencyKey, orEmptyJSON(input.Payload), c.Provider,
		nullableString(input.CorrelationID), nullableString(input.CausationID),
	).Scan(&req.ID, &req.MerchantID, &req.ConnectionID, &req.IdempotencyKey, &req.Status, &req.Payload,
		&req.ExternalFulfillmentID, &req.Provider, &req.CorrelationID, &req.CausationID, &req.CreatedAt, &req.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Idempotent replay: return the existing request.
		existing, err := fulfillmentByIdempotency(ctx, tx, input.ConnectionID, input.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		return existing, tx.Commit(ctx)
	}
	if err != nil {
		return nil, fmt.Errorf("insert fulfillment request: %w", err)
	}

	envelope := events.EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypeSupplyFulfillmentRequested,
		SchemaVersion:    1,
		AggregateType:    "merchant_integration_supply_fulfillment_request",
		AggregateID:      req.ID.String(),
		AggregateVersion: 1,
		CorrelationID:    input.CorrelationID,
		CausationID:      input.CausationID,
		OccurredAt:       time.Now().UTC(),
		Payload: map[string]any{
			"request_id":      req.ID.String(),
			"merchant_id":     req.MerchantID.String(),
			"connection_id":   req.ConnectionID.String(),
			"provider":        string(req.Provider),
			"idempotency_key": req.IdempotencyKey,
			"payload":         req.Payload,
		},
	}
	if err := s.store.Enqueue(ctx, tx, envelope); err != nil {
		return nil, fmt.Errorf("enqueue fulfillment requested event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit fulfillment request: %w", err)
	}
	return &req, nil
}

func fulfillmentByIdempotency(ctx context.Context, tx pgx.Tx, connectionID uuid.UUID, key string) (*MerchantSupplyFulfillmentRequest, error) {
	var req MerchantSupplyFulfillmentRequest
	err := tx.QueryRow(ctx, `
SELECT id, merchant_id, connection_id, idempotency_key, status, payload, external_fulfillment_id,
       provider, correlation_id, causation_id, created_at, updated_at
FROM merchant_integration_supply_fulfillment_requests WHERE connection_id = $1 AND idempotency_key = $2`,
		connectionID, key).Scan(&req.ID, &req.MerchantID, &req.ConnectionID, &req.IdempotencyKey, &req.Status,
		&req.Payload, &req.ExternalFulfillmentID, &req.Provider, &req.CorrelationID, &req.CausationID,
		&req.CreatedAt, &req.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("query fulfillment by idempotency: %w", err)
	}
	return &req, nil
}

func (s *supplyService) GetFulfillmentRequest(ctx context.Context, id uuid.UUID) (*MerchantSupplyFulfillmentRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	var req MerchantSupplyFulfillmentRequest
	err = tx.QueryRow(ctx, `
SELECT id, merchant_id, connection_id, idempotency_key, status, payload, external_fulfillment_id,
       provider, correlation_id, causation_id, created_at, updated_at
FROM merchant_integration_supply_fulfillment_requests WHERE id = $1`, id).
		Scan(&req.ID, &req.MerchantID, &req.ConnectionID, &req.IdempotencyKey, &req.Status, &req.Payload,
			&req.ExternalFulfillmentID, &req.Provider, &req.CorrelationID, &req.CausationID, &req.CreatedAt, &req.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFulfillmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query fulfillment request: %w", err)
	}
	return &req, tx.Commit(ctx)
}

func (s *supplyService) UpdateFulfillmentRequestStatus(ctx context.Context, id uuid.UUID, status string, externalFulfillmentID *string) (*MerchantSupplyFulfillmentRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	var req MerchantSupplyFulfillmentRequest
	err = tx.QueryRow(ctx, `
UPDATE merchant_integration_supply_fulfillment_requests
SET status = $1,
    external_fulfillment_id = COALESCE($2, external_fulfillment_id),
    updated_at = NOW()
WHERE id = $3
RETURNING id, merchant_id, connection_id, idempotency_key, status, payload, external_fulfillment_id,
          provider, correlation_id, causation_id, created_at, updated_at`,
		status, externalFulfillmentID, id).
		Scan(&req.ID, &req.MerchantID, &req.ConnectionID, &req.IdempotencyKey, &req.Status, &req.Payload,
			&req.ExternalFulfillmentID, &req.Provider, &req.CorrelationID, &req.CausationID, &req.CreatedAt, &req.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFulfillmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("update fulfillment status: %w", err)
	}
	return &req, tx.Commit(ctx)
}

// RecordTrackingEvent imports one external tracking update exactly once.
// Duplicate (connection, external_event_id) deliveries return the existing row
// with isNew=false and never re-emit the event.
func (s *supplyService) RecordTrackingEvent(ctx context.Context, input RecordTrackingEventInput, correlationID, causationID string) (*MerchantSupplyTrackingEvent, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	var req MerchantSupplyFulfillmentRequest
	err = tx.QueryRow(ctx,
		`SELECT id, merchant_id, connection_id FROM merchant_integration_supply_fulfillment_requests WHERE id = $1`,
		input.RequestID).Scan(&req.ID, &req.MerchantID, &req.ConnectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, ErrFulfillmentNotFound
	}
	if err != nil {
		return nil, false, fmt.Errorf("query fulfillment for tracking: %w", err)
	}
	if req.ConnectionID != input.ConnectionID {
		return nil, false, ErrFulfillmentNotFound
	}

	var ev MerchantSupplyTrackingEvent
	err = tx.QueryRow(ctx, `
INSERT INTO merchant_integration_supply_tracking_events
    (request_id, connection_id, external_event_id, status, carrier, tracking_number, occurred_at, payload)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (connection_id, external_event_id) DO NOTHING
RETURNING id, request_id, connection_id, external_event_id, status, carrier, tracking_number, occurred_at, payload, created_at`,
		input.RequestID, input.ConnectionID, input.ExternalEventID, input.Status, input.Carrier,
		input.TrackingNumber, input.OccurredAt, orEmptyJSON(input.Payload),
	).Scan(&ev.ID, &ev.RequestID, &ev.ConnectionID, &ev.ExternalEventID, &ev.Status, &ev.Carrier,
		&ev.TrackingNumber, &ev.OccurredAt, &ev.Payload, &ev.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		var existing MerchantSupplyTrackingEvent
		if err := tx.QueryRow(ctx, `
SELECT id, request_id, connection_id, external_event_id, status, carrier, tracking_number, occurred_at, payload, created_at
FROM merchant_integration_supply_tracking_events WHERE connection_id = $1 AND external_event_id = $2`,
			input.ConnectionID, input.ExternalEventID).Scan(&existing.ID, &existing.RequestID, &existing.ConnectionID,
			&existing.ExternalEventID, &existing.Status, &existing.Carrier, &existing.TrackingNumber,
			&existing.OccurredAt, &existing.Payload, &existing.CreatedAt); err != nil {
			return nil, false, fmt.Errorf("query existing tracking event: %w", err)
		}
		return &existing, false, tx.Commit(ctx)
	}
	if err != nil {
		return nil, false, fmt.Errorf("insert tracking event: %w", err)
	}

	envelope := events.EventEnvelope{
		EventID:          uuid.NewString(),
		EventType:        EventTypeSupplyTrackingRecorded,
		SchemaVersion:    1,
		AggregateType:    "merchant_integration_supply_tracking_event",
		AggregateID:      ev.ID.String(),
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       time.Now().UTC(),
		Payload: map[string]any{
			"event_id":          ev.ID.String(),
			"request_id":        ev.RequestID.String(),
			"connection_id":     ev.ConnectionID.String(),
			"merchant_id":       req.MerchantID.String(),
			"external_event_id": ev.ExternalEventID,
			"status":            ev.Status,
		},
	}
	if err := s.store.Enqueue(ctx, tx, envelope); err != nil {
		return nil, false, fmt.Errorf("enqueue tracking recorded event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, fmt.Errorf("commit tracking event: %w", err)
	}
	return &ev, true, nil
}

// RecordWebhookInbox stores one inbound webhook exactly once per
// (provider, idempotency_key); duplicates return isNew=false.
func (s *supplyService) RecordWebhookInbox(ctx context.Context, input RecordWebhookInboxInput) (*MerchantWebhookInboxItem, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, false, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	var item MerchantWebhookInboxItem
	err = tx.QueryRow(ctx, `
INSERT INTO merchant_integration_webhook_inbox (connection_id, provider, event_type, idempotency_key, payload)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (provider, idempotency_key) DO NOTHING
RETURNING id, connection_id, provider, event_type, idempotency_key, payload, status, error_message, received_at, processed_at`,
		input.ConnectionID, input.Provider, input.EventType, input.IdempotencyKey, orEmptyJSON(input.Payload),
	).Scan(&item.ID, &item.ConnectionID, &item.Provider, &item.EventType, &item.IdempotencyKey, &item.Payload,
		&item.Status, &item.ErrorMessage, &item.ReceivedAt, &item.ProcessedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		var existing MerchantWebhookInboxItem
		if err := tx.QueryRow(ctx, `
SELECT id, connection_id, provider, event_type, idempotency_key, payload, status, error_message, received_at, processed_at
FROM merchant_integration_webhook_inbox WHERE provider = $1 AND idempotency_key = $2`,
			input.Provider, input.IdempotencyKey).Scan(&existing.ID, &existing.ConnectionID, &existing.Provider,
			&existing.EventType, &existing.IdempotencyKey, &existing.Payload, &existing.Status,
			&existing.ErrorMessage, &existing.ReceivedAt, &existing.ProcessedAt); err != nil {
			return nil, false, fmt.Errorf("query existing inbox item: %w", err)
		}
		return &existing, false, tx.Commit(ctx)
	}
	if err != nil {
		return nil, false, fmt.Errorf("insert inbox item: %w", err)
	}
	return &item, true, tx.Commit(ctx)
}

func findBatchByIdempotency(ctx context.Context, tx pgx.Tx, connectionID uuid.UUID, key string) (*SupplyImportBatch, []SupplyImportRecord, error) {
	var batch SupplyImportBatch
	err := tx.QueryRow(ctx, `
SELECT id, merchant_id, connection_id, provider, batch_type, status, first_import, cursor_token,
       record_count, approved_count, rejected_count, duplicate_count, idempotency_key,
       correlation_id, causation_id, created_at, updated_at
FROM merchant_integration_supply_import_batches WHERE connection_id = $1 AND idempotency_key = $2`,
		connectionID, key).
		Scan(&batch.ID, &batch.MerchantID, &batch.ConnectionID, &batch.Provider, &batch.BatchType,
			&batch.Status, &batch.FirstImport, &batch.CursorToken, &batch.RecordCount, &batch.ApprovedCount,
			&batch.RejectedCount, &batch.DuplicateCount, &batch.IdempotencyKey, &batch.CorrelationID,
			&batch.CausationID, &batch.CreatedAt, &batch.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, fmt.Errorf("query batch by idempotency: %w", err)
	}
	rows, err := tx.Query(ctx, `
SELECT id, batch_id, merchant_id, connection_id, entity_type, external_product_id, external_variant_id,
       sku, barcode, title, currency, external_version, content_digest, payload, status,
       duplicate_of_connection, review_case_id, mapping_decision, created_at, updated_at
FROM merchant_integration_supply_import_records WHERE batch_id = $1 ORDER BY created_at, id`, batch.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("query batch records: %w", err)
	}
	defer rows.Close()
	var records []SupplyImportRecord
	for rows.Next() {
		var rec SupplyImportRecord
		if err := rows.Scan(&rec.ID, &rec.BatchID, &rec.MerchantID, &rec.ConnectionID, &rec.EntityType,
			&rec.ExternalProductID, &rec.ExternalVariantID, &rec.SKU, &rec.Barcode, &rec.Title, &rec.Currency,
			&rec.ExternalVersion, &rec.ContentDigest, &rec.Payload, &rec.Status, &rec.DuplicateOfConn,
			&rec.ReviewCaseID, &rec.MappingDecision, &rec.CreatedAt, &rec.UpdatedAt); err != nil {
			return nil, nil, fmt.Errorf("scan batch record: %w", err)
		}
		records = append(records, rec)
	}
	return &batch, records, rows.Err()
}

func rollback(ctx context.Context, tx pgx.Tx) {
	_ = tx.Rollback(ctx)
}

func orEmptyJSON(raw json.RawMessage) json.RawMessage {
	if len(raw) == 0 {
		return json.RawMessage(`{}`)
	}
	return raw
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
