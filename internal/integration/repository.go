package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"core/modules/commerce"
)

type DBPool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Repository interface {
	CreateConnection(ctx context.Context, tx pgx.Tx, conn Connection) error
	GetConnectionByID(ctx context.Context, tx pgx.Tx, id string) (*Connection, error)
	ListConnectionsByActor(ctx context.Context, tx pgx.Tx, actorType ActorType, actorID string, page commerce.Page) ([]Connection, error)
	UpdateConnectionStatus(ctx context.Context, tx pgx.Tx, id string, status ConnectionStatus) (*Connection, error)

	UpsertEntityMapping(ctx context.Context, tx pgx.Tx, mapping EntityMapping) error
	GetMappingByExternalID(ctx context.Context, tx pgx.Tx, connectionID string, entityType EntityType, externalID string) (*EntityMapping, error)
	GetMappingByInternalID(ctx context.Context, tx pgx.Tx, connectionID string, entityType EntityType, internalID string) (*EntityMapping, error)
	ListEntityMappings(ctx context.Context, tx pgx.Tx, connectionID string, entityType EntityType, page commerce.Page) ([]EntityMapping, error)

	UpsertSyncCursor(ctx context.Context, tx pgx.Tx, cursor SyncCursor) error
	GetSyncCursor(ctx context.Context, tx pgx.Tx, connectionID string, entityType EntityType) (*SyncCursor, error)

	PersistWebhookInbox(ctx context.Context, tx pgx.Tx, item WebhookInboxItem) (*WebhookInboxItem, bool, error)

	CreateSyncJob(ctx context.Context, tx pgx.Tx, job SupplierSyncJob) (*SupplierSyncJob, error)
	GetSyncJobByID(ctx context.Context, tx pgx.Tx, id string) (*SupplierSyncJob, error)
	UpdateSyncJobStatus(ctx context.Context, tx pgx.Tx, id string, status SyncJobStatus, processed, failed int, errSummary string) (*SupplierSyncJob, error)
	ListSyncJobsBySupplier(ctx context.Context, tx pgx.Tx, supplierID string, page commerce.Page) ([]SupplierSyncJob, error)

	CreateSellerSyncJob(ctx context.Context, tx pgx.Tx, job SellerSyncJob) (*SellerSyncJob, error)
	GetSellerSyncJobByID(ctx context.Context, tx pgx.Tx, id string) (*SellerSyncJob, error)
	UpdateSellerSyncJobStatus(ctx context.Context, tx pgx.Tx, id string, status SyncJobStatus, processed, failed int, errSummary string) (*SellerSyncJob, error)
	ListSellerSyncJobsByStore(ctx context.Context, tx pgx.Tx, storeID string, page commerce.Page) ([]SellerSyncJob, error)

	CreateAPIKey(ctx context.Context, tx pgx.Tx, key APIKey) error
	GetAPIKeyByHash(ctx context.Context, tx pgx.Tx, hash string) (*APIKey, error)
	ListAPIKeysByActor(ctx context.Context, tx pgx.Tx, actorType ActorType, actorID string) ([]APIKey, error)
	RevokeAPIKey(ctx context.Context, tx pgx.Tx, id, actorID string) error
	RevokeAPIKeyWithReason(ctx context.Context, tx pgx.Tx, id, actorID, reason string) error
	MarkAPIKeyRotated(ctx context.Context, tx pgx.Tx, id, actorID string, rotatedAt time.Time, expiresAt *time.Time) error
	UpdateAPIKeyLastUsed(ctx context.Context, tx pgx.Tx, id string, lastUsed time.Time) error
	RecordAPIKeyAuditLog(ctx context.Context, tx pgx.Tx, log APIKeyAuditLog) error

	CreateWebhookSubscription(ctx context.Context, tx pgx.Tx, sub WebhookSubscription) error
	ListWebhookSubscriptionsByActor(ctx context.Context, tx pgx.Tx, actorType ActorType, actorID string) ([]WebhookSubscription, error)
	DeleteWebhookSubscription(ctx context.Context, tx pgx.Tx, id, actorID string) error
	GetWebhookSubscriptionByID(ctx context.Context, tx pgx.Tx, id string) (*WebhookSubscription, error)
	EnqueueWebhookOutbox(ctx context.Context, tx pgx.Tx, item WebhookOutboxItem) error
	UpdateWebhookOutboxStatus(ctx context.Context, tx pgx.Tx, id, status string, retryCount int, nextAttemptAt time.Time, lastError string) error
	RecordWebhookDeliveryLog(ctx context.Context, tx pgx.Tx, log WebhookDeliveryLog) error
}

type repository struct {
	pool DBPool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return &repository{pool: pool}
}

func (r *repository) conn(tx pgx.Tx) DBPool {
	if tx != nil {
		return tx
	}
	return r.pool
}

func (r *repository) CreateConnection(ctx context.Context, tx pgx.Tx, conn Connection) error {
	query := `
		INSERT INTO integration_connections (id, actor_type, actor_id, provider, name, status, credentials_vault_ref, settings, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.conn(tx).Exec(ctx, query, conn.ID, conn.ActorType, conn.ActorID, conn.Provider, conn.Name, conn.Status, conn.CredentialsVaultRef, conn.Settings, conn.CreatedAt, conn.UpdatedAt)
	return err
}

func (r *repository) GetConnectionByID(ctx context.Context, tx pgx.Tx, id string) (*Connection, error) {
	query := `
		SELECT id, actor_type, actor_id, provider, name, status, credentials_vault_ref, settings, created_at, updated_at
		FROM integration_connections WHERE id = $1
	`
	var conn Connection
	var ref sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, id).Scan(&conn.ID, &conn.ActorType, &conn.ActorID, &conn.Provider, &conn.Name, &conn.Status, &ref, &conn.Settings, &conn.CreatedAt, &conn.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConnectionNotFound
	}
	if err != nil {
		return nil, err
	}
	if ref.Valid {
		conn.CredentialsVaultRef = ref.String
	}
	return &conn, nil
}

func (r *repository) ListConnectionsByActor(ctx context.Context, tx pgx.Tx, actorType ActorType, actorID string, page commerce.Page) ([]Connection, error) {
	query := `
		SELECT id, actor_type, actor_id, provider, name, status, credentials_vault_ref, settings, created_at, updated_at
		FROM integration_connections WHERE actor_type = $1 AND actor_id = $2
		ORDER BY created_at DESC LIMIT $3 OFFSET $4
	`
	limit, offset := page.Limit, page.Offset
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.conn(tx).Query(ctx, query, actorType, actorID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []Connection
	for rows.Next() {
		var conn Connection
		var ref sql.NullString
		if err := rows.Scan(&conn.ID, &conn.ActorType, &conn.ActorID, &conn.Provider, &conn.Name, &conn.Status, &ref, &conn.Settings, &conn.CreatedAt, &conn.UpdatedAt); err != nil {
			return nil, err
		}
		if ref.Valid {
			conn.CredentialsVaultRef = ref.String
		}
		list = append(list, conn)
	}
	return list, nil
}

func (r *repository) UpdateConnectionStatus(ctx context.Context, tx pgx.Tx, id string, status ConnectionStatus) (*Connection, error) {
	query := `
		UPDATE integration_connections SET status = $2, updated_at = NOW() WHERE id = $1
		RETURNING id, actor_type, actor_id, provider, name, status, credentials_vault_ref, settings, created_at, updated_at
	`
	var conn Connection
	var ref sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, id, status).Scan(&conn.ID, &conn.ActorType, &conn.ActorID, &conn.Provider, &conn.Name, &conn.Status, &ref, &conn.Settings, &conn.CreatedAt, &conn.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrConnectionNotFound
	}
	if err != nil {
		return nil, err
	}
	if ref.Valid {
		conn.CredentialsVaultRef = ref.String
	}
	return &conn, nil
}

func (r *repository) UpsertEntityMapping(ctx context.Context, tx pgx.Tx, mapping EntityMapping) error {
	query := `
		INSERT INTO external_entity_mappings (id, connection_id, entity_type, internal_id, external_id, external_version, mapping_status, sync_direction, conflict_status, metadata, last_synced_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (connection_id, entity_type, external_id) DO UPDATE SET
			internal_id = EXCLUDED.internal_id,
			external_version = EXCLUDED.external_version,
			mapping_status = EXCLUDED.mapping_status,
			sync_direction = EXCLUDED.sync_direction,
			conflict_status = EXCLUDED.conflict_status,
			metadata = EXCLUDED.metadata,
			last_synced_at = EXCLUDED.last_synced_at,
			updated_at = EXCLUDED.updated_at
	`
	_, err := r.conn(tx).Exec(ctx, query, mapping.ID, mapping.ConnectionID, mapping.EntityType, mapping.InternalID, mapping.ExternalID, mapping.ExternalVersion, mapping.MappingStatus, mapping.SyncDirection, mapping.ConflictStatus, mapping.Metadata, mapping.LastSyncedAt, mapping.CreatedAt, mapping.UpdatedAt)
	return err
}

func (r *repository) GetMappingByExternalID(ctx context.Context, tx pgx.Tx, connectionID string, entityType EntityType, externalID string) (*EntityMapping, error) {
	query := `
		SELECT id, connection_id, entity_type, internal_id, external_id, external_version, mapping_status, sync_direction, conflict_status, metadata, last_synced_at, created_at, updated_at
		FROM external_entity_mappings WHERE connection_id = $1 AND entity_type = $2 AND external_id = $3
	`
	var m EntityMapping
	var version, conflict sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, connectionID, entityType, externalID).Scan(&m.ID, &m.ConnectionID, &m.EntityType, &m.InternalID, &m.ExternalID, &version, &m.MappingStatus, &m.SyncDirection, &conflict, &m.Metadata, &m.LastSyncedAt, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMappingNotFound
	}
	if err != nil {
		return nil, err
	}
	if version.Valid {
		m.ExternalVersion = version.String
	}
	if conflict.Valid {
		m.ConflictStatus = conflict.String
	}
	return &m, nil
}

func (r *repository) GetMappingByInternalID(ctx context.Context, tx pgx.Tx, connectionID string, entityType EntityType, internalID string) (*EntityMapping, error) {
	query := `
		SELECT id, connection_id, entity_type, internal_id, external_id, external_version, mapping_status, sync_direction, conflict_status, metadata, last_synced_at, created_at, updated_at
		FROM external_entity_mappings WHERE connection_id = $1 AND entity_type = $2 AND internal_id = $3
	`
	var m EntityMapping
	var version, conflict sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, connectionID, entityType, internalID).Scan(&m.ID, &m.ConnectionID, &m.EntityType, &m.InternalID, &m.ExternalID, &version, &m.MappingStatus, &m.SyncDirection, &conflict, &m.Metadata, &m.LastSyncedAt, &m.CreatedAt, &m.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMappingNotFound
	}
	if err != nil {
		return nil, err
	}
	if version.Valid {
		m.ExternalVersion = version.String
	}
	if conflict.Valid {
		m.ConflictStatus = conflict.String
	}
	return &m, nil
}

func (r *repository) ListEntityMappings(ctx context.Context, tx pgx.Tx, connectionID string, entityType EntityType, page commerce.Page) ([]EntityMapping, error) {
	query := `
		SELECT id, connection_id, entity_type, internal_id, external_id, external_version, mapping_status, sync_direction, conflict_status, metadata, last_synced_at, created_at, updated_at
		FROM external_entity_mappings WHERE connection_id = $1 AND entity_type = $2
		ORDER BY created_at DESC LIMIT $3 OFFSET $4
	`
	limit, offset := page.Limit, page.Offset
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.conn(tx).Query(ctx, query, connectionID, entityType, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []EntityMapping
	for rows.Next() {
		var m EntityMapping
		var version, conflict sql.NullString
		if err := rows.Scan(&m.ID, &m.ConnectionID, &m.EntityType, &m.InternalID, &m.ExternalID, &version, &m.MappingStatus, &m.SyncDirection, &conflict, &m.Metadata, &m.LastSyncedAt, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, err
		}
		if version.Valid {
			m.ExternalVersion = version.String
		}
		if conflict.Valid {
			m.ConflictStatus = conflict.String
		}
		list = append(list, m)
	}
	return list, nil
}

func (r *repository) UpsertSyncCursor(ctx context.Context, tx pgx.Tx, cursor SyncCursor) error {
	query := `
		INSERT INTO integration_sync_cursors (id, connection_id, entity_type, cursor_token, last_successful_sync, last_reconciled_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (connection_id, entity_type) DO UPDATE SET
			cursor_token = EXCLUDED.cursor_token,
			last_successful_sync = EXCLUDED.last_successful_sync,
			last_reconciled_at = EXCLUDED.last_reconciled_at,
			updated_at = EXCLUDED.updated_at
	`
	_, err := r.conn(tx).Exec(ctx, query, cursor.ID, cursor.ConnectionID, cursor.EntityType, cursor.CursorToken, cursor.LastSuccessfulSync, cursor.LastReconciledAt, cursor.CreatedAt, cursor.UpdatedAt)
	return err
}

func (r *repository) GetSyncCursor(ctx context.Context, tx pgx.Tx, connectionID string, entityType EntityType) (*SyncCursor, error) {
	query := `
		SELECT id, connection_id, entity_type, cursor_token, last_successful_sync, last_reconciled_at, created_at, updated_at
		FROM integration_sync_cursors WHERE connection_id = $1 AND entity_type = $2
	`
	var c SyncCursor
	var token sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, connectionID, entityType).Scan(&c.ID, &c.ConnectionID, &c.EntityType, &token, &c.LastSuccessfulSync, &c.LastReconciledAt, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSyncCursorNotFound
	}
	if err != nil {
		return nil, err
	}
	if token.Valid {
		c.CursorToken = token.String
	}
	return &c, nil
}

func (r *repository) PersistWebhookInbox(ctx context.Context, tx pgx.Tx, item WebhookInboxItem) (*WebhookInboxItem, bool, error) {
	query := `
		INSERT INTO integration_webhook_inbox (id, connection_id, provider, event_type, idempotency_key, payload, status, error_message, received_at, processed_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (provider, idempotency_key) DO NOTHING
		RETURNING id, connection_id, provider, event_type, idempotency_key, payload, status, error_message, received_at, processed_at
	`
	var connRef, errMsg sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, item.ID, item.ConnectionID, item.Provider, item.EventType, item.IdempotencyKey, item.Payload, item.Status, item.ErrorMessage, item.ReceivedAt, item.ProcessedAt).Scan(&item.ID, &connRef, &item.Provider, &item.EventType, &item.IdempotencyKey, &item.Payload, &item.Status, &errMsg, &item.ReceivedAt, &item.ProcessedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		// Already exists
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if connRef.Valid {
		item.ConnectionID = connRef.String
	}
	if errMsg.Valid {
		item.ErrorMessage = errMsg.String
	}
	return &item, true, nil
}

func (r *repository) CreateSyncJob(ctx context.Context, tx pgx.Tx, job SupplierSyncJob) (*SupplierSyncJob, error) {
	query := `
		INSERT INTO supplier_sync_jobs (id, connection_id, supplier_id, status, total_items, processed_items, failed_items, error_summary, started_at, completed_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		RETURNING id, connection_id, supplier_id, status, total_items, processed_items, failed_items, error_summary, started_at, completed_at, created_at, updated_at
	`
	var errSum sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, job.ID, job.ConnectionID, job.SupplierID, job.Status, job.TotalItems, job.ProcessedItems, job.FailedItems, job.ErrorSummary, job.StartedAt, job.CompletedAt, job.CreatedAt, job.UpdatedAt).Scan(&job.ID, &job.ConnectionID, &job.SupplierID, &job.Status, &job.TotalItems, &job.ProcessedItems, &job.FailedItems, &errSum, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if errSum.Valid {
		job.ErrorSummary = errSum.String
	}
	return &job, nil
}

func (r *repository) GetSyncJobByID(ctx context.Context, tx pgx.Tx, id string) (*SupplierSyncJob, error) {
	query := `
		SELECT id, connection_id, supplier_id, status, total_items, processed_items, failed_items, error_summary, started_at, completed_at, created_at, updated_at
		FROM supplier_sync_jobs WHERE id = $1
	`
	var job SupplierSyncJob
	var errSum sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, id).Scan(&job.ID, &job.ConnectionID, &job.SupplierID, &job.Status, &job.TotalItems, &job.ProcessedItems, &job.FailedItems, &errSum, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSyncJobNotFound
	}
	if err != nil {
		return nil, err
	}
	if errSum.Valid {
		job.ErrorSummary = errSum.String
	}
	return &job, nil
}

func (r *repository) UpdateSyncJobStatus(ctx context.Context, tx pgx.Tx, id string, status SyncJobStatus, processed, failed int, errSummary string) (*SupplierSyncJob, error) {
	query := `
		UPDATE supplier_sync_jobs SET status = $2, processed_items = $3, failed_items = $4, error_summary = $5, updated_at = NOW() WHERE id = $1
		RETURNING id, connection_id, supplier_id, status, total_items, processed_items, failed_items, error_summary, started_at, completed_at, created_at, updated_at
	`
	var job SupplierSyncJob
	var errSum sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, id, status, processed, failed, errSummary).Scan(&job.ID, &job.ConnectionID, &job.SupplierID, &job.Status, &job.TotalItems, &job.ProcessedItems, &job.FailedItems, &errSum, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSyncJobNotFound
	}
	if err != nil {
		return nil, err
	}
	if errSum.Valid {
		job.ErrorSummary = errSum.String
	}
	return &job, nil
}

func (r *repository) ListSyncJobsBySupplier(ctx context.Context, tx pgx.Tx, supplierID string, page commerce.Page) ([]SupplierSyncJob, error) {
	query := `
		SELECT id, connection_id, supplier_id, status, total_items, processed_items, failed_items, error_summary, started_at, completed_at, created_at, updated_at
		FROM supplier_sync_jobs WHERE supplier_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3
	`
	limit, offset := page.Limit, page.Offset
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.conn(tx).Query(ctx, query, supplierID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []SupplierSyncJob
	for rows.Next() {
		var job SupplierSyncJob
		var errSum sql.NullString
		if err := rows.Scan(&job.ID, &job.ConnectionID, &job.SupplierID, &job.Status, &job.TotalItems, &job.ProcessedItems, &job.FailedItems, &errSum, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		if errSum.Valid {
			job.ErrorSummary = errSum.String
		}
		list = append(list, job)
	}
	return list, nil
}

func (r *repository) CreateSellerSyncJob(ctx context.Context, tx pgx.Tx, job SellerSyncJob) (*SellerSyncJob, error) {
	query := `
		INSERT INTO seller_sync_jobs (id, store_id, connection_id, sync_type, status, total_items, processed_items, failed_items, error_summary, started_at, completed_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		RETURNING id, store_id, connection_id, sync_type, status, total_items, processed_items, failed_items, error_summary, started_at, completed_at, created_at, updated_at
	`
	var errSum sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, job.ID, job.StoreID, job.ConnectionID, job.SyncType, job.Status, job.TotalItems, job.ProcessedItems, job.FailedItems, job.ErrorSummary, job.StartedAt, job.CompletedAt, job.CreatedAt, job.UpdatedAt).Scan(&job.ID, &job.StoreID, &job.ConnectionID, &job.SyncType, &job.Status, &job.TotalItems, &job.ProcessedItems, &job.FailedItems, &errSum, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.UpdatedAt)
	if err != nil {
		return nil, err
	}
	if errSum.Valid {
		job.ErrorSummary = errSum.String
	}
	return &job, nil
}

func (r *repository) GetSellerSyncJobByID(ctx context.Context, tx pgx.Tx, id string) (*SellerSyncJob, error) {
	query := `
		SELECT id, store_id, connection_id, sync_type, status, total_items, processed_items, failed_items, error_summary, started_at, completed_at, created_at, updated_at
		FROM seller_sync_jobs WHERE id = $1
	`
	var job SellerSyncJob
	var errSum sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, id).Scan(&job.ID, &job.StoreID, &job.ConnectionID, &job.SyncType, &job.Status, &job.TotalItems, &job.ProcessedItems, &job.FailedItems, &errSum, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSellerSyncJobNotFound
	}
	if err != nil {
		return nil, err
	}
	if errSum.Valid {
		job.ErrorSummary = errSum.String
	}
	return &job, nil
}

func (r *repository) UpdateSellerSyncJobStatus(ctx context.Context, tx pgx.Tx, id string, status SyncJobStatus, processed, failed int, errSummary string) (*SellerSyncJob, error) {
	query := `
		UPDATE seller_sync_jobs SET status = $2, processed_items = $3, failed_items = $4, error_summary = $5, updated_at = NOW() WHERE id = $1
		RETURNING id, store_id, connection_id, sync_type, status, total_items, processed_items, failed_items, error_summary, started_at, completed_at, created_at, updated_at
	`
	var job SellerSyncJob
	var errSum sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, id, status, processed, failed, errSummary).Scan(&job.ID, &job.StoreID, &job.ConnectionID, &job.SyncType, &job.Status, &job.TotalItems, &job.ProcessedItems, &job.FailedItems, &errSum, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrSellerSyncJobNotFound
	}
	if err != nil {
		return nil, err
	}
	if errSum.Valid {
		job.ErrorSummary = errSum.String
	}
	return &job, nil
}

func (r *repository) ListSellerSyncJobsByStore(ctx context.Context, tx pgx.Tx, storeID string, page commerce.Page) ([]SellerSyncJob, error) {
	query := `
		SELECT id, store_id, connection_id, sync_type, status, total_items, processed_items, failed_items, error_summary, started_at, completed_at, created_at, updated_at
		FROM seller_sync_jobs WHERE store_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3
	`
	limit, offset := page.Limit, page.Offset
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.conn(tx).Query(ctx, query, storeID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []SellerSyncJob
	for rows.Next() {
		var job SellerSyncJob
		var errSum sql.NullString
		if err := rows.Scan(&job.ID, &job.StoreID, &job.ConnectionID, &job.SyncType, &job.Status, &job.TotalItems, &job.ProcessedItems, &job.FailedItems, &errSum, &job.StartedAt, &job.CompletedAt, &job.CreatedAt, &job.UpdatedAt); err != nil {
			return nil, err
		}
		if errSum.Valid {
			job.ErrorSummary = errSum.String
		}
		list = append(list, job)
	}
	return list, nil
}

func (r *repository) CreateAPIKey(ctx context.Context, tx pgx.Tx, key APIKey) error {
	query := `
		INSERT INTO api_keys (id, actor_type, actor_id, name, key_prefix, key_hash, scopes, status, expires_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	scopesJSON, _ := json.Marshal(key.Scopes)
	_, err := r.conn(tx).Exec(ctx, query, key.ID, key.ActorType, key.ActorID, key.Name, key.KeyPrefix, key.KeyHash, scopesJSON, key.Status, key.ExpiresAt, key.CreatedAt, key.UpdatedAt)
	return err
}

func (r *repository) GetAPIKeyByHash(ctx context.Context, tx pgx.Tx, hash string) (*APIKey, error) {
	query := `
		SELECT id, actor_type, actor_id, name, key_prefix, key_hash, scopes, status, expires_at, last_used_at, rotated_at, revocation_reason, created_at, updated_at
		FROM api_keys WHERE key_hash = $1
	`
	var k APIKey
	var scopesRaw []byte
	var reason sql.NullString
	err := r.conn(tx).QueryRow(ctx, query, hash).Scan(&k.ID, &k.ActorType, &k.ActorID, &k.Name, &k.KeyPrefix, &k.KeyHash, &scopesRaw, &k.Status, &k.ExpiresAt, &k.LastUsedAt, &k.RotatedAt, &reason, &k.CreatedAt, &k.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrAPIKeyNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(scopesRaw, &k.Scopes)
	if reason.Valid {
		k.RevocationReason = reason.String
	}
	return &k, nil
}

func (r *repository) ListAPIKeysByActor(ctx context.Context, tx pgx.Tx, actorType ActorType, actorID string) ([]APIKey, error) {
	query := `
		SELECT id, actor_type, actor_id, name, key_prefix, key_hash, scopes, status, expires_at, last_used_at, rotated_at, revocation_reason, created_at, updated_at
		FROM api_keys WHERE actor_type = $1 AND actor_id = $2 ORDER BY created_at DESC
	`
	rows, err := r.conn(tx).Query(ctx, query, actorType, actorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []APIKey
	for rows.Next() {
		var k APIKey
		var scopesRaw []byte
		var reason sql.NullString
		if err := rows.Scan(&k.ID, &k.ActorType, &k.ActorID, &k.Name, &k.KeyPrefix, &k.KeyHash, &scopesRaw, &k.Status, &k.ExpiresAt, &k.LastUsedAt, &k.RotatedAt, &reason, &k.CreatedAt, &k.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(scopesRaw, &k.Scopes)
		if reason.Valid {
			k.RevocationReason = reason.String
		}
		list = append(list, k)
	}
	return list, nil
}

func (r *repository) RevokeAPIKey(ctx context.Context, tx pgx.Tx, id, actorID string) error {
	return r.RevokeAPIKeyWithReason(ctx, tx, id, actorID, "user_requested")
}

func (r *repository) RevokeAPIKeyWithReason(ctx context.Context, tx pgx.Tx, id, actorID, reason string) error {
	query := `
		UPDATE api_keys SET status = 'revoked', revocation_reason = $3, updated_at = NOW()
		WHERE id = $1 AND actor_id = $2
	`
	tag, err := r.conn(tx).Exec(ctx, query, id, actorID, reason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (r *repository) MarkAPIKeyRotated(ctx context.Context, tx pgx.Tx, id, actorID string, rotatedAt time.Time, expiresAt *time.Time) error {
	query := `
		UPDATE api_keys SET rotated_at = $3, expires_at = $4, updated_at = NOW()
		WHERE id = $1 AND actor_id = $2
	`
	tag, err := r.conn(tx).Exec(ctx, query, id, actorID, rotatedAt, expiresAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrAPIKeyNotFound
	}
	return nil
}

func (r *repository) UpdateAPIKeyLastUsed(ctx context.Context, tx pgx.Tx, id string, lastUsed time.Time) error {
	query := `UPDATE api_keys SET last_used_at = $2, updated_at = NOW() WHERE id = $1`
	_, err := r.conn(tx).Exec(ctx, query, id, lastUsed)
	return err
}

func (r *repository) RecordAPIKeyAuditLog(ctx context.Context, tx pgx.Tx, log APIKeyAuditLog) error {
	query := `
		INSERT INTO api_key_audit_logs (id, api_key_id, actor_type, actor_id, action, details, ip_address, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, err := r.conn(tx).Exec(ctx, query, log.ID, log.APIKeyID, log.ActorType, log.ActorID, log.Action, log.Details, log.IPAddress, log.CreatedAt)
	return err
}

func (r *repository) CreateWebhookSubscription(ctx context.Context, tx pgx.Tx, sub WebhookSubscription) error {
	query := `
		INSERT INTO webhook_subscriptions (id, actor_type, actor_id, target_url, secret_hash, subscribed_events, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	eventsJSON, _ := json.Marshal(sub.SubscribedEvents)
	_, err := r.conn(tx).Exec(ctx, query, sub.ID, sub.ActorType, sub.ActorID, sub.TargetURL, sub.SecretHash, eventsJSON, sub.Status, sub.CreatedAt, sub.UpdatedAt)
	return err
}

func (r *repository) ListWebhookSubscriptionsByActor(ctx context.Context, tx pgx.Tx, actorType ActorType, actorID string) ([]WebhookSubscription, error) {
	query := `
		SELECT id, actor_type, actor_id, target_url, secret_hash, subscribed_events, status, created_at, updated_at
		FROM webhook_subscriptions WHERE actor_type = $1 AND actor_id = $2 ORDER BY created_at DESC
	`
	rows, err := r.conn(tx).Query(ctx, query, actorType, actorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []WebhookSubscription
	for rows.Next() {
		var sub WebhookSubscription
		var eventsRaw []byte
		if err := rows.Scan(&sub.ID, &sub.ActorType, &sub.ActorID, &sub.TargetURL, &sub.SecretHash, &eventsRaw, &sub.Status, &sub.CreatedAt, &sub.UpdatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(eventsRaw, &sub.SubscribedEvents)
		list = append(list, sub)
	}
	return list, nil
}

func (r *repository) DeleteWebhookSubscription(ctx context.Context, tx pgx.Tx, id, actorID string) error {
	query := `DELETE FROM webhook_subscriptions WHERE id = $1 AND actor_id = $2`
	tag, err := r.conn(tx).Exec(ctx, query, id, actorID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrWebhookSubNotFound
	}
	return nil
}

func (r *repository) GetWebhookSubscriptionByID(ctx context.Context, tx pgx.Tx, id string) (*WebhookSubscription, error) {
	query := `
		SELECT id, actor_type, actor_id, target_url, secret_hash, subscribed_events, status, created_at, updated_at
		FROM webhook_subscriptions WHERE id = $1
	`
	var sub WebhookSubscription
	var eventsRaw []byte
	err := r.conn(tx).QueryRow(ctx, query, id).Scan(&sub.ID, &sub.ActorType, &sub.ActorID, &sub.TargetURL, &sub.SecretHash, &eventsRaw, &sub.Status, &sub.CreatedAt, &sub.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrWebhookSubNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(eventsRaw, &sub.SubscribedEvents)
	return &sub, nil
}

func (r *repository) EnqueueWebhookOutbox(ctx context.Context, tx pgx.Tx, item WebhookOutboxItem) error {
	query := `
		INSERT INTO webhook_outbox (id, subscription_id, event_type, payload, status, retry_count, max_retries, next_attempt_at, last_error, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	_, err := r.conn(tx).Exec(ctx, query, item.ID, item.SubscriptionID, item.EventType, item.Payload, item.Status, item.RetryCount, item.MaxRetries, item.NextAttemptAt, item.LastError, item.CreatedAt, item.UpdatedAt)
	return err
}

func (r *repository) UpdateWebhookOutboxStatus(ctx context.Context, tx pgx.Tx, id, status string, retryCount int, nextAttemptAt time.Time, lastError string) error {
	query := `
		UPDATE webhook_outbox SET status = $2, retry_count = $3, next_attempt_at = $4, last_error = $5, updated_at = NOW()
		WHERE id = $1
	`
	_, err := r.conn(tx).Exec(ctx, query, id, status, retryCount, nextAttemptAt, lastError)
	return err
}

func (r *repository) RecordWebhookDeliveryLog(ctx context.Context, tx pgx.Tx, log WebhookDeliveryLog) error {
	query := `
		INSERT INTO webhook_deliveries (id, subscription_id, event_type, payload, response_status_code, attempts, status, next_retry_at, delivered_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.conn(tx).Exec(ctx, query, log.ID, log.SubscriptionID, log.EventType, log.Payload, log.ResponseStatusCode, log.Attempts, log.Status, log.NextRetryAt, log.DeliveredAt, log.CreatedAt)
	return err
}
