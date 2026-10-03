package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"core/modules/commerce"
)

var (
	ErrMerchantConnectionNotFound = errors.New("merchant integration connection not found")
	ErrMerchantJobIntentNotFound  = errors.New("merchant integration job intent not found")
	ErrDuplicateAccountConnection = errors.New("duplicate provider external account connection for merchant")
)

type MerchantRepository interface {
	CreateMerchantConnection(ctx context.Context, tx pgx.Tx, conn MerchantIntegrationConnection) error
	GetMerchantConnectionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*MerchantIntegrationConnection, error)
	ListMerchantConnections(ctx context.Context, tx pgx.Tx, merchantID uuid.UUID, connType *MerchantConnectionType, page commerce.Page) ([]MerchantIntegrationConnection, error)
	UpdateMerchantConnectionStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, status MerchantConnectionStatus) (*MerchantIntegrationConnection, error)

	CreateMerchantJobIntent(ctx context.Context, tx pgx.Tx, job MerchantIntegrationJobIntent) error
	GetMerchantJobIntentByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*MerchantIntegrationJobIntent, error)
	ListMerchantJobIntents(ctx context.Context, tx pgx.Tx, merchantID uuid.UUID, connectionID *uuid.UUID, page commerce.Page) ([]MerchantIntegrationJobIntent, error)

	RecordMigrationCrosswalk(ctx context.Context, tx pgx.Tx, record MigrationCrosswalkRecord) error
	ListLegacyConnections(ctx context.Context, tx pgx.Tx) ([]Connection, error)
	GetSellerMerchantID(ctx context.Context, tx pgx.Tx, sellerID string) (uuid.UUID, error)
	GetSupplierMerchantID(ctx context.Context, tx pgx.Tx, supplierID string) (uuid.UUID, error)
}

type merchantRepository struct {
	pool DBPool
}

func NewMerchantRepository(pool *pgxpool.Pool) MerchantRepository {
	return &merchantRepository{pool: pool}
}

func (r *merchantRepository) conn(tx pgx.Tx) DBPool {
	if tx != nil {
		return tx
	}
	return r.pool
}

func (r *merchantRepository) CreateMerchantConnection(ctx context.Context, tx pgx.Tx, conn MerchantIntegrationConnection) error {
	if err := conn.Validate(); err != nil {
		return err
	}
	if conn.ID == uuid.Nil {
		conn.ID = uuid.New()
	}
	if conn.Status == "" {
		conn.Status = MerchantStatusDraft
	}
	if conn.HealthStatus == "" {
		conn.HealthStatus = "healthy"
	}
	if conn.GrantedScopes == nil {
		conn.GrantedScopes = json.RawMessage("[]")
	}
	if conn.Settings == nil {
		conn.Settings = json.RawMessage("{}")
	}
	now := time.Now().UTC()
	if conn.CreatedAt.IsZero() {
		conn.CreatedAt = now
	}
	conn.UpdatedAt = now

	query := `
		INSERT INTO merchant_integration_connections (
			id, merchant_id, connection_type, provider, external_account_id, name, store_id,
			status, credentials_vault_ref, granted_scopes, health_status, last_health_check_at,
			first_successful_sync_at, legacy_actor_type, legacy_actor_id, legacy_connection_id,
			settings, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12,
			$13, $14, $15, $16,
			$17, $18, $19
		)
	`
	_, err := r.conn(tx).Exec(ctx, query,
		conn.ID, conn.MerchantID, conn.ConnectionType, conn.Provider, conn.ExternalAccountID, conn.Name, conn.StoreID,
		conn.Status, conn.CredentialsVaultRef, conn.GrantedScopes, conn.HealthStatus, conn.LastHealthCheckAt,
		conn.FirstSuccessfulSyncAt, conn.LegacyActorType, conn.LegacyActorID, conn.LegacyConnectionID,
		conn.Settings, conn.CreatedAt, conn.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrDuplicateAccountConnection
		}
		return err
	}
	return nil
}

func (r *merchantRepository) GetMerchantConnectionByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*MerchantIntegrationConnection, error) {
	query := `
		SELECT
			id, merchant_id, connection_type, provider, external_account_id, name, store_id,
			status, credentials_vault_ref, granted_scopes, health_status, last_health_check_at,
			first_successful_sync_at, legacy_actor_type, legacy_actor_id, legacy_connection_id,
			settings, created_at, updated_at
		FROM merchant_integration_connections
		WHERE id = $1
	`
	row := r.conn(tx).QueryRow(ctx, query, id)
	return scanMerchantConnection(row)
}

func (r *merchantRepository) ListMerchantConnections(ctx context.Context, tx pgx.Tx, merchantID uuid.UUID, connType *MerchantConnectionType, page commerce.Page) ([]MerchantIntegrationConnection, error) {
	limit := page.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := page.Offset
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT
			id, merchant_id, connection_type, provider, external_account_id, name, store_id,
			status, credentials_vault_ref, granted_scopes, health_status, last_health_check_at,
			first_successful_sync_at, legacy_actor_type, legacy_actor_id, legacy_connection_id,
			settings, created_at, updated_at
		FROM merchant_integration_connections
		WHERE merchant_id = $1
	`
	args := []any{merchantID}
	if connType != nil && *connType != "" {
		query += fmt.Sprintf(" AND connection_type = $%d", len(args)+1)
		args = append(args, *connType)
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.conn(tx).Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []MerchantIntegrationConnection
	for rows.Next() {
		conn, err := scanMerchantConnectionFromRows(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *conn)
	}
	return result, rows.Err()
}

func (r *merchantRepository) UpdateMerchantConnectionStatus(ctx context.Context, tx pgx.Tx, id uuid.UUID, status MerchantConnectionStatus) (*MerchantIntegrationConnection, error) {
	now := time.Now().UTC()
	query := `
		UPDATE merchant_integration_connections
		SET status = $1, updated_at = $2
		WHERE id = $3
		RETURNING
			id, merchant_id, connection_type, provider, external_account_id, name, store_id,
			status, credentials_vault_ref, granted_scopes, health_status, last_health_check_at,
			first_successful_sync_at, legacy_actor_type, legacy_actor_id, legacy_connection_id,
			settings, created_at, updated_at
	`
	row := r.conn(tx).QueryRow(ctx, query, status, now, id)
	return scanMerchantConnection(row)
}

func (r *merchantRepository) CreateMerchantJobIntent(ctx context.Context, tx pgx.Tx, job MerchantIntegrationJobIntent) error {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.Status == "" {
		job.Status = JobStatusRequested
	}
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	job.UpdatedAt = now

	query := `
		INSERT INTO merchant_integration_job_intents (
			id, merchant_id, connection_id, pipeline_type, job_type, status, idempotency_key,
			cursor_or_version, correlation_id, causation_id, total_items, processed_items, failed_items,
			error_summary, started_at, completed_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10, $11, $12, $13,
			$14, $15, $16, $17, $18
		)
	`
	_, err := r.conn(tx).Exec(ctx, query,
		job.ID, job.MerchantID, job.ConnectionID, job.PipelineType, job.JobType, job.Status, job.IdempotencyKey,
		job.CursorOrVersion, job.CorrelationID, job.CausationID, job.TotalItems, job.ProcessedItems, job.FailedItems,
		job.ErrorSummary, job.StartedAt, job.CompletedAt, job.CreatedAt, job.UpdatedAt,
	)
	return err
}

func (r *merchantRepository) GetMerchantJobIntentByID(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*MerchantIntegrationJobIntent, error) {
	query := `
		SELECT
			id, merchant_id, connection_id, pipeline_type, job_type, status, idempotency_key,
			cursor_or_version, correlation_id, causation_id, total_items, processed_items, failed_items,
			error_summary, started_at, completed_at, created_at, updated_at
		FROM merchant_integration_job_intents
		WHERE id = $1
	`
	row := r.conn(tx).QueryRow(ctx, query, id)
	var j MerchantIntegrationJobIntent
	err := row.Scan(
		&j.ID, &j.MerchantID, &j.ConnectionID, &j.PipelineType, &j.JobType, &j.Status, &j.IdempotencyKey,
		&j.CursorOrVersion, &j.CorrelationID, &j.CausationID, &j.TotalItems, &j.ProcessedItems, &j.FailedItems,
		&j.ErrorSummary, &j.StartedAt, &j.CompletedAt, &j.CreatedAt, &j.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMerchantJobIntentNotFound
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}

func (r *merchantRepository) ListMerchantJobIntents(ctx context.Context, tx pgx.Tx, merchantID uuid.UUID, connectionID *uuid.UUID, page commerce.Page) ([]MerchantIntegrationJobIntent, error) {
	limit := page.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := page.Offset
	if offset < 0 {
		offset = 0
	}

	query := `
		SELECT
			id, merchant_id, connection_id, pipeline_type, job_type, status, idempotency_key,
			cursor_or_version, correlation_id, causation_id, total_items, processed_items, failed_items,
			error_summary, started_at, completed_at, created_at, updated_at
		FROM merchant_integration_job_intents
		WHERE merchant_id = $1
	`
	args := []any{merchantID}
	if connectionID != nil && *connectionID != uuid.Nil {
		query += fmt.Sprintf(" AND connection_id = $%d", len(args)+1)
		args = append(args, *connectionID)
	}

	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", len(args)+1, len(args)+2)
	args = append(args, limit, offset)

	rows, err := r.conn(tx).Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []MerchantIntegrationJobIntent
	for rows.Next() {
		var j MerchantIntegrationJobIntent
		err := rows.Scan(
			&j.ID, &j.MerchantID, &j.ConnectionID, &j.PipelineType, &j.JobType, &j.Status, &j.IdempotencyKey,
			&j.CursorOrVersion, &j.CorrelationID, &j.CausationID, &j.TotalItems, &j.ProcessedItems, &j.FailedItems,
			&j.ErrorSummary, &j.StartedAt, &j.CompletedAt, &j.CreatedAt, &j.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		result = append(result, j)
	}
	return result, rows.Err()
}

func (r *merchantRepository) RecordMigrationCrosswalk(ctx context.Context, tx pgx.Tx, record MigrationCrosswalkRecord) error {
	if record.ID == uuid.Nil {
		record.ID = uuid.New()
	}
	if record.QuarantineDetails == nil {
		record.QuarantineDetails = json.RawMessage("{}")
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	query := `
		INSERT INTO merchant_integration_migration_crosswalk (
			id, legacy_connection_id, legacy_actor_type, legacy_actor_id, target_connection_id,
			merchant_id, classification, quarantine_reason_code, quarantine_details, run_id, created_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9, $10, $11
		)
	`
	_, err := r.conn(tx).Exec(ctx, query,
		record.ID, record.LegacyConnectionID, record.LegacyActorType, record.LegacyActorID, record.TargetConnectionID,
		record.MerchantID, record.Classification, record.QuarantineReasonCode, record.QuarantineDetails, record.RunID, record.CreatedAt,
	)
	return err
}

func (r *merchantRepository) ListLegacyConnections(ctx context.Context, tx pgx.Tx) ([]Connection, error) {
	query := `
		SELECT id, actor_type, actor_id, provider, name, status, credentials_vault_ref, settings, created_at, updated_at
		FROM integration_connections
		ORDER BY created_at ASC
	`
	rows, err := r.conn(tx).Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Connection
	for rows.Next() {
		var c Connection
		var ref sql.NullString
		err := rows.Scan(&c.ID, &c.ActorType, &c.ActorID, &c.Provider, &c.Name, &c.Status, &ref, &c.Settings, &c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			return nil, err
		}
		if ref.Valid {
			c.CredentialsVaultRef = ref.String
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

func (r *merchantRepository) GetSellerMerchantID(ctx context.Context, tx pgx.Tx, sellerID string) (uuid.UUID, error) {
	query := `SELECT merchant_id FROM sellers WHERE id = $1`
	var merchantID uuid.NullUUID
	err := r.conn(tx).QueryRow(ctx, query, sellerID).Scan(&merchantID)
	if errors.Is(err, pgx.ErrNoRows) || !merchantID.Valid || merchantID.UUID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("seller %s has no explicit merchant_id", sellerID)
	}
	return merchantID.UUID, err
}

func (r *merchantRepository) GetSupplierMerchantID(ctx context.Context, tx pgx.Tx, supplierID string) (uuid.UUID, error) {
	query := `SELECT merchant_id FROM suppliers WHERE id = $1`
	var merchantID uuid.NullUUID
	err := r.conn(tx).QueryRow(ctx, query, supplierID).Scan(&merchantID)
	if errors.Is(err, pgx.ErrNoRows) || !merchantID.Valid || merchantID.UUID == uuid.Nil {
		return uuid.Nil, fmt.Errorf("supplier %s has no explicit merchant_id", supplierID)
	}
	return merchantID.UUID, err
}

func scanMerchantConnection(row pgx.Row) (*MerchantIntegrationConnection, error) {
	var c MerchantIntegrationConnection
	var extAcc sql.NullString
	var storeID uuid.NullUUID
	var vaultRef sql.NullString
	var legacyActorType sql.NullString
	var legacyActorID sql.NullString
	var legacyConnID sql.NullString

	err := row.Scan(
		&c.ID, &c.MerchantID, &c.ConnectionType, &c.Provider, &extAcc, &c.Name, &storeID,
		&c.Status, &vaultRef, &c.GrantedScopes, &c.HealthStatus, &c.LastHealthCheckAt,
		&c.FirstSuccessfulSyncAt, &legacyActorType, &legacyActorID, &legacyConnID,
		&c.Settings, &c.CreatedAt, &c.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrMerchantConnectionNotFound
	}
	if err != nil {
		return nil, err
	}

	if extAcc.Valid {
		c.ExternalAccountID = &extAcc.String
	}
	if storeID.Valid && storeID.UUID != uuid.Nil {
		c.StoreID = &storeID.UUID
	}
	if vaultRef.Valid {
		c.CredentialsVaultRef = vaultRef.String
	}
	if legacyActorType.Valid {
		c.LegacyActorType = &legacyActorType.String
	}
	if legacyActorID.Valid {
		c.LegacyActorID = &legacyActorID.String
	}
	if legacyConnID.Valid {
		c.LegacyConnectionID = &legacyConnID.String
	}

	return &c, nil
}

func scanMerchantConnectionFromRows(rows pgx.Rows) (*MerchantIntegrationConnection, error) {
	var c MerchantIntegrationConnection
	var extAcc sql.NullString
	var storeID uuid.NullUUID
	var vaultRef sql.NullString
	var legacyActorType sql.NullString
	var legacyActorID sql.NullString
	var legacyConnID sql.NullString

	err := rows.Scan(
		&c.ID, &c.MerchantID, &c.ConnectionType, &c.Provider, &extAcc, &c.Name, &storeID,
		&c.Status, &vaultRef, &c.GrantedScopes, &c.HealthStatus, &c.LastHealthCheckAt,
		&c.FirstSuccessfulSyncAt, &legacyActorType, &legacyActorID, &legacyConnID,
		&c.Settings, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	if extAcc.Valid {
		c.ExternalAccountID = &extAcc.String
	}
	if storeID.Valid && storeID.UUID != uuid.Nil {
		c.StoreID = &storeID.UUID
	}
	if vaultRef.Valid {
		c.CredentialsVaultRef = vaultRef.String
	}
	if legacyActorType.Valid {
		c.LegacyActorType = &legacyActorType.String
	}
	if legacyActorID.Valid {
		c.LegacyActorID = &legacyActorID.String
	}
	if legacyConnID.Valid {
		c.LegacyConnectionID = &legacyConnID.String
	}

	return &c, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	if errStr == "" {
		return false
	}
	return contains(errStr, "23505") || contains(errStr, "unique constraint") || contains(errStr, "uq_merchant_integration_conn_account")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
