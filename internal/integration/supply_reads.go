package integration

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SupplyListPage bounds a merchant-authorized supply read.
type SupplyListPage struct {
	Limit  int
	Offset int
}

const (
	defaultSupplyListLimit = 20
	maxSupplyListLimit     = 100
)

// NormalizeSupplyListPage applies bounded defaults to a supply list page.
func NormalizeSupplyListPage(limit, offset int) SupplyListPage {
	if limit <= 0 {
		limit = defaultSupplyListLimit
	}
	if limit > maxSupplyListLimit {
		limit = maxSupplyListLimit
	}
	if offset < 0 {
		offset = 0
	}
	return SupplyListPage{Limit: limit, Offset: offset}
}

// ListImportBatchesForMerchant lists a Merchant's own import batches. Rows
// from any other Merchant are never visible, regardless of filter values.
func (s *supplyService) ListImportBatchesForMerchant(ctx context.Context, merchantID uuid.UUID, connectionID *uuid.UUID, status string, page SupplyListPage) ([]SupplyImportBatch, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	rows, err := tx.Query(ctx, `
SELECT id, merchant_id, connection_id, provider, batch_type, status, first_import, cursor_token,
       record_count, approved_count, rejected_count, duplicate_count, idempotency_key,
       correlation_id, causation_id, created_at, updated_at
FROM merchant_integration_supply_import_batches
WHERE merchant_id = $1 AND ($2::uuid IS NULL OR connection_id = $2) AND ($3::text IS NULL OR status = $3)
ORDER BY created_at DESC, id
LIMIT $4 OFFSET $5`, merchantID, connectionID, nullableString(status), page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("query import batches: %w", err)
	}
	defer rows.Close()

	batches := []SupplyImportBatch{}
	for rows.Next() {
		var b SupplyImportBatch
		if err := rows.Scan(&b.ID, &b.MerchantID, &b.ConnectionID, &b.Provider, &b.BatchType,
			&b.Status, &b.FirstImport, &b.CursorToken, &b.RecordCount, &b.ApprovedCount,
			&b.RejectedCount, &b.DuplicateCount, &b.IdempotencyKey, &b.CorrelationID,
			&b.CausationID, &b.CreatedAt, &b.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan import batch: %w", err)
		}
		batches = append(batches, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate import batches: %w", err)
	}
	return batches, tx.Commit(ctx)
}

// GetImportBatchForMerchant returns one of the Merchant's own batches with its
// staged records. A batch owned by another Merchant is indistinguishable from
// a missing one.
func (s *supplyService) GetImportBatchForMerchant(ctx context.Context, merchantID, batchID uuid.UUID) (*SupplyImportBatch, []SupplyImportRecord, error) {
	batch, records, err := s.GetImportBatch(ctx, batchID)
	if err != nil {
		return nil, nil, err
	}
	if batch.MerchantID != merchantID {
		return nil, nil, ErrBatchNotFound
	}
	return batch, records, nil
}

// GetReviewCaseForMerchant returns one of the Merchant's own review cases.
func (s *supplyService) GetReviewCaseForMerchant(ctx context.Context, merchantID, caseID uuid.UUID) (*MerchantReviewCase, error) {
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
FROM merchant_integration_review_cases WHERE id = $1 AND merchant_id = $2`, caseID, merchantID).
		Scan(&c.ID, &c.MerchantID, &c.ConnectionID, &c.CaseType, &c.Status, &c.ReasonCode,
			&c.SubjectBatchID, &subjectRecordID, &c.Details, &c.Resolution, &c.ResolvedBy, &c.ResolvedAt,
			&c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrReviewCaseNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query review case: %w", err)
	}
	c.SubjectRecordID = subjectRecordID
	return &c, tx.Commit(ctx)
}

// ListMappingsForMerchant lists the Merchant's own entity mappings.
func (s *supplyService) ListMappingsForMerchant(ctx context.Context, merchantID uuid.UUID, connectionID *uuid.UUID) ([]MerchantEntityMapping, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	rows, err := tx.Query(ctx, `
SELECT id, merchant_id, connection_id, entity_type, internal_id, external_product_id, external_variant_id,
       external_version, content_digest, authority_source, provenance, status, approved_batch_id,
       last_synced_at, created_at, updated_at
FROM merchant_integration_entity_mappings
WHERE merchant_id = $1 AND ($2::uuid IS NULL OR connection_id = $2)
ORDER BY created_at DESC, id`, merchantID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("query mappings: %w", err)
	}
	defer rows.Close()

	mappings := []MerchantEntityMapping{}
	for rows.Next() {
		var m MerchantEntityMapping
		if err := rows.Scan(&m.ID, &m.MerchantID, &m.ConnectionID, &m.EntityType, &m.InternalID,
			&m.ExternalProductID, &m.ExternalVariantID, &m.ExternalVersion, &m.ContentDigest,
			&m.AuthoritySource, &m.Provenance, &m.Status, &m.ApprovedBatchID, &m.LastSyncedAt,
			&m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan mapping: %w", err)
		}
		mappings = append(mappings, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate mappings: %w", err)
	}
	return mappings, tx.Commit(ctx)
}

// GetMappingForMerchant returns one of the Merchant's own mappings.
func (s *supplyService) GetMappingForMerchant(ctx context.Context, merchantID, mappingID uuid.UUID) (*MerchantEntityMapping, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	var m MerchantEntityMapping
	err = tx.QueryRow(ctx, `
SELECT id, merchant_id, connection_id, entity_type, internal_id, external_product_id, external_variant_id,
       external_version, content_digest, authority_source, provenance, status, approved_batch_id,
       last_synced_at, created_at, updated_at
FROM merchant_integration_entity_mappings WHERE id = $1 AND merchant_id = $2`, mappingID, merchantID).
		Scan(&m.ID, &m.MerchantID, &m.ConnectionID, &m.EntityType, &m.InternalID,
			&m.ExternalProductID, &m.ExternalVariantID, &m.ExternalVersion, &m.ContentDigest,
			&m.AuthoritySource, &m.Provenance, &m.Status, &m.ApprovedBatchID, &m.LastSyncedAt,
			&m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMappingNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query mapping: %w", err)
	}
	return &m, tx.Commit(ctx)
}

// ListSyncCursorsForMerchant lists the sync state of the Merchant's own
// connections. Cursors are joined through their connection, which carries the
// Merchant ownership.
func (s *supplyService) ListSyncCursorsForMerchant(ctx context.Context, merchantID uuid.UUID, connectionID *uuid.UUID) ([]MerchantSyncCursor, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	rows, err := tx.Query(ctx, `
SELECT cur.id, cur.connection_id, cur.entity_type, cur.cursor_token, cur.last_successful_sync,
       cur.last_reconciled_at, cur.created_at, cur.updated_at
FROM merchant_integration_sync_cursors cur
JOIN merchant_integration_connections conn ON conn.id = cur.connection_id
WHERE conn.merchant_id = $1 AND ($2::uuid IS NULL OR cur.connection_id = $2)
ORDER BY cur.created_at DESC, cur.id`, merchantID, connectionID)
	if err != nil {
		return nil, fmt.Errorf("query sync cursors: %w", err)
	}
	defer rows.Close()

	cursors := []MerchantSyncCursor{}
	for rows.Next() {
		var c MerchantSyncCursor
		if err := rows.Scan(&c.ID, &c.ConnectionID, &c.EntityType, &c.CursorToken,
			&c.LastSuccessfulSync, &c.LastReconciledAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan sync cursor: %w", err)
		}
		cursors = append(cursors, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sync cursors: %w", err)
	}
	return cursors, tx.Commit(ctx)
}

// ListFulfillmentRequestsForMerchant lists the Merchant's own fulfillment
// requests.
func (s *supplyService) ListFulfillmentRequestsForMerchant(ctx context.Context, merchantID uuid.UUID, connectionID *uuid.UUID, status string, page SupplyListPage) ([]MerchantSupplyFulfillmentRequest, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	rows, err := tx.Query(ctx, `
SELECT id, merchant_id, connection_id, idempotency_key, status, payload, external_fulfillment_id,
       provider, correlation_id, causation_id, created_at, updated_at
FROM merchant_integration_supply_fulfillment_requests
WHERE merchant_id = $1 AND ($2::uuid IS NULL OR connection_id = $2) AND ($3::text IS NULL OR status = $3)
ORDER BY created_at DESC, id
LIMIT $4 OFFSET $5`, merchantID, connectionID, nullableString(status), page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("query fulfillment requests: %w", err)
	}
	defer rows.Close()

	requests := []MerchantSupplyFulfillmentRequest{}
	for rows.Next() {
		var f MerchantSupplyFulfillmentRequest
		if err := rows.Scan(&f.ID, &f.MerchantID, &f.ConnectionID, &f.IdempotencyKey, &f.Status,
			&f.Payload, &f.ExternalFulfillmentID, &f.Provider, &f.CorrelationID, &f.CausationID,
			&f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan fulfillment request: %w", err)
		}
		requests = append(requests, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fulfillment requests: %w", err)
	}
	return requests, tx.Commit(ctx)
}

// GetFulfillmentRequestForMerchant returns one of the Merchant's own fulfillment
// requests with its tracking events, ordered by occurrence.
func (s *supplyService) GetFulfillmentRequestForMerchant(ctx context.Context, merchantID, requestID uuid.UUID) (*MerchantSupplyFulfillmentRequest, []MerchantSupplyTrackingEvent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	var f MerchantSupplyFulfillmentRequest
	err = tx.QueryRow(ctx, `
SELECT id, merchant_id, connection_id, idempotency_key, status, payload, external_fulfillment_id,
       provider, correlation_id, causation_id, created_at, updated_at
FROM merchant_integration_supply_fulfillment_requests WHERE id = $1 AND merchant_id = $2`, requestID, merchantID).
		Scan(&f.ID, &f.MerchantID, &f.ConnectionID, &f.IdempotencyKey, &f.Status, &f.Payload,
			&f.ExternalFulfillmentID, &f.Provider, &f.CorrelationID, &f.CausationID, &f.CreatedAt, &f.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, ErrFulfillmentNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("query fulfillment request: %w", err)
	}

	events, err := listTrackingEventsTx(ctx, tx, requestID)
	if err != nil {
		return nil, nil, err
	}
	return &f, events, tx.Commit(ctx)
}

// ListTrackingEventsForMerchant lists the tracking events of one of the
// Merchant's own fulfillment requests.
func (s *supplyService) ListTrackingEventsForMerchant(ctx context.Context, merchantID, requestID uuid.UUID) ([]MerchantSupplyTrackingEvent, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer rollback(ctx, tx)

	var owner uuid.UUID
	err = tx.QueryRow(ctx, `SELECT merchant_id FROM merchant_integration_supply_fulfillment_requests WHERE id = $1 AND merchant_id = $2`, requestID, merchantID).Scan(&owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrFulfillmentNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("query fulfillment owner: %w", err)
	}

	events, err := listTrackingEventsTx(ctx, tx, requestID)
	if err != nil {
		return nil, err
	}
	return events, tx.Commit(ctx)
}

func listTrackingEventsTx(ctx context.Context, tx pgx.Tx, requestID uuid.UUID) ([]MerchantSupplyTrackingEvent, error) {
	rows, err := tx.Query(ctx, `
SELECT id, request_id, connection_id, external_event_id, status, carrier, tracking_number,
       occurred_at, payload, created_at
FROM merchant_integration_supply_tracking_events
WHERE request_id = $1
ORDER BY created_at DESC, id`, requestID)
	if err != nil {
		return nil, fmt.Errorf("query tracking events: %w", err)
	}
	defer rows.Close()

	events := []MerchantSupplyTrackingEvent{}
	for rows.Next() {
		var e MerchantSupplyTrackingEvent
		if err := rows.Scan(&e.ID, &e.RequestID, &e.ConnectionID, &e.ExternalEventID, &e.Status,
			&e.Carrier, &e.TrackingNumber, &e.OccurredAt, &e.Payload, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan tracking event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}
