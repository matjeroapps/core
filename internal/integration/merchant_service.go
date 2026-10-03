package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"core/modules/commerce"
	"core/packages/events"
	"core/packages/outbox"
)

const (
	EventTypeMerchantConnectionCreated       = "merchant.integration.connection.created.v1"
	EventTypeMerchantConnectionStatusChanged = "merchant.integration.connection.status_changed.v1"
	EventTypeMerchantMigrationCompleted      = "merchant.integration.migration.completed.v1"
)

type MerchantService interface {
	CreateMerchantConnection(ctx context.Context, input CreateMerchantConnectionInput, correlationID, causationID string) (*MerchantIntegrationConnection, error)
	GetMerchantConnection(ctx context.Context, id uuid.UUID) (*MerchantIntegrationConnection, error)
	ListMerchantConnections(ctx context.Context, merchantID uuid.UUID, connType *MerchantConnectionType, page commerce.Page) ([]MerchantIntegrationConnection, error)
	UpdateMerchantConnectionStatus(ctx context.Context, id uuid.UUID, status MerchantConnectionStatus, correlationID, causationID string) (*MerchantIntegrationConnection, error)

	CreateJobIntent(ctx context.Context, input CreateJobIntentInput) (*MerchantIntegrationJobIntent, error)
	GetJobIntent(ctx context.Context, id uuid.UUID) (*MerchantIntegrationJobIntent, error)
	ListJobIntents(ctx context.Context, merchantID uuid.UUID, connectionID *uuid.UUID, page commerce.Page) ([]MerchantIntegrationJobIntent, error)

	ReconcileLegacyConnections(ctx context.Context, dryRun bool) (*MigrationSummary, error)
}

type merchantService struct {
	repo  MerchantRepository
	pool  *pgxpool.Pool
	store outbox.Store
}

func NewMerchantService(repo MerchantRepository, pool *pgxpool.Pool) MerchantService {
	return &merchantService{
		repo:  repo,
		pool:  pool,
		store: outbox.NewStore(),
	}
}

func (s *merchantService) CreateMerchantConnection(ctx context.Context, input CreateMerchantConnectionInput, correlationID, causationID string) (*MerchantIntegrationConnection, error) {
	conn := MerchantIntegrationConnection{
		ID:                  uuid.New(),
		MerchantID:          input.MerchantID,
		ConnectionType:      input.ConnectionType,
		Provider:            input.Provider,
		ExternalAccountID:   input.ExternalAccountID,
		Name:                input.Name,
		StoreID:             input.StoreID,
		Status:              MerchantStatusDraft,
		CredentialsVaultRef: input.CredentialsVaultRef,
		GrantedScopes:       input.GrantedScopes,
		Settings:            input.Settings,
		LegacyActorType:     input.LegacyActorType,
		LegacyActorID:       input.LegacyActorID,
		LegacyConnectionID:  input.LegacyConnectionID,
		CreatedAt:           time.Now().UTC(),
		UpdatedAt:           time.Now().UTC(),
	}

	if err := conn.Validate(); err != nil {
		return nil, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.repo.CreateMerchantConnection(ctx, tx, conn); err != nil {
		return nil, err
	}

	payload := map[string]any{
		"connection_id":        conn.ID.String(),
		"merchant_id":          conn.MerchantID.String(),
		"connection_type":      string(conn.ConnectionType),
		"provider":             string(conn.Provider),
		"name":                 conn.Name,
		"status":               string(conn.Status),
		"legacy_actor_type":    strPtrVal(conn.LegacyActorType),
		"legacy_actor_id":      strPtrVal(conn.LegacyActorID),
		"legacy_connection_id": strPtrVal(conn.LegacyConnectionID),
	}
	if conn.ExternalAccountID != nil {
		payload["external_account_id"] = *conn.ExternalAccountID
	}
	if conn.StoreID != nil {
		payload["store_id"] = conn.StoreID.String()
	}

	envelope := events.EventEnvelope{
		EventID:          uuid.New().String(),
		EventType:        EventTypeMerchantConnectionCreated,
		SchemaVersion:    1,
		AggregateType:    "merchant_integration_connection",
		AggregateID:      conn.ID.String(),
		AggregateVersion: 1,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       time.Now().UTC(),
		Payload:          payload,
	}

	if err := s.store.Enqueue(ctx, tx, envelope); err != nil {
		return nil, fmt.Errorf("failed to enqueue outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit tx: %w", err)
	}

	return &conn, nil
}

func (s *merchantService) GetMerchantConnection(ctx context.Context, id uuid.UUID) (*MerchantIntegrationConnection, error) {
	if id == uuid.Nil {
		return nil, ErrMerchantConnectionNotFound
	}
	return s.repo.GetMerchantConnectionByID(ctx, nil, id)
}

func (s *merchantService) ListMerchantConnections(ctx context.Context, merchantID uuid.UUID, connType *MerchantConnectionType, page commerce.Page) ([]MerchantIntegrationConnection, error) {
	if merchantID == uuid.Nil {
		return nil, errors.New("merchant_id is required")
	}
	return s.repo.ListMerchantConnections(ctx, nil, merchantID, connType, page)
}

func (s *merchantService) UpdateMerchantConnectionStatus(ctx context.Context, id uuid.UUID, status MerchantConnectionStatus, correlationID, causationID string) (*MerchantIntegrationConnection, error) {
	if id == uuid.Nil {
		return nil, ErrMerchantConnectionNotFound
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	conn, err := s.repo.UpdateMerchantConnectionStatus(ctx, tx, id, status)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"connection_id":   conn.ID.String(),
		"merchant_id":     conn.MerchantID.String(),
		"connection_type": string(conn.ConnectionType),
		"status":          string(conn.Status),
	}

	envelope := events.EventEnvelope{
		EventID:          uuid.New().String(),
		EventType:        EventTypeMerchantConnectionStatusChanged,
		SchemaVersion:    1,
		AggregateType:    "merchant_integration_connection",
		AggregateID:      conn.ID.String(),
		AggregateVersion: 2,
		CorrelationID:    correlationID,
		CausationID:      causationID,
		OccurredAt:       time.Now().UTC(),
		Payload:          payload,
	}

	if err := s.store.Enqueue(ctx, tx, envelope); err != nil {
		return nil, fmt.Errorf("failed to enqueue outbox event: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("failed to commit tx: %w", err)
	}

	return conn, nil
}

func (s *merchantService) CreateJobIntent(ctx context.Context, input CreateJobIntentInput) (*MerchantIntegrationJobIntent, error) {
	if input.MerchantID == uuid.Nil || input.ConnectionID == uuid.Nil {
		return nil, errors.New("merchant_id and connection_id are required")
	}
	if input.IdempotencyKey == "" {
		return nil, errors.New("idempotency_key is required")
	}
	if input.PipelineType == "" {
		return nil, errors.New("pipeline_type is required")
	}
	if input.JobType == "" {
		return nil, errors.New("job_type is required")
	}

	job := MerchantIntegrationJobIntent{
		ID:              uuid.New(),
		MerchantID:      input.MerchantID,
		ConnectionID:    input.ConnectionID,
		PipelineType:    input.PipelineType,
		JobType:         input.JobType,
		Status:          JobStatusRequested,
		IdempotencyKey:  input.IdempotencyKey,
		CursorOrVersion: input.CursorOrVersion,
		CorrelationID:   input.CorrelationID,
		CausationID:     input.CausationID,
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}

	if err := s.repo.CreateMerchantJobIntent(ctx, nil, job); err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *merchantService) GetJobIntent(ctx context.Context, id uuid.UUID) (*MerchantIntegrationJobIntent, error) {
	if id == uuid.Nil {
		return nil, ErrMerchantJobIntentNotFound
	}
	return s.repo.GetMerchantJobIntentByID(ctx, nil, id)
}

func (s *merchantService) ListJobIntents(ctx context.Context, merchantID uuid.UUID, connectionID *uuid.UUID, page commerce.Page) ([]MerchantIntegrationJobIntent, error) {
	if merchantID == uuid.Nil {
		return nil, errors.New("merchant_id is required")
	}
	return s.repo.ListMerchantJobIntents(ctx, nil, merchantID, connectionID, page)
}

func (s *merchantService) ReconcileLegacyConnections(ctx context.Context, dryRun bool) (*MigrationSummary, error) {
	runID := uuid.New()
	summary := &MigrationSummary{
		RunID:  runID,
		DryRun: dryRun,
	}

	var tx pgx.Tx
	var err error
	if !dryRun {
		tx, err = s.pool.Begin(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to begin migration tx: %w", err)
		}
		defer tx.Rollback(ctx)
	}

	legacyConns, err := s.repo.ListLegacyConnections(ctx, tx)
	if err != nil {
		return nil, fmt.Errorf("failed to list legacy connections: %w", err)
	}

	summary.ScannedCount = len(legacyConns)

	for _, leg := range legacyConns {
		var merchantID uuid.UUID
		var connType MerchantConnectionType
		var quarantineReason string

		switch leg.ActorType {
		case ActorTypeSeller:
			connType = ConnectionTypeRetailChannel
			mid, mErr := s.repo.GetSellerMerchantID(ctx, tx, leg.ActorID)
			if mErr != nil {
				quarantineReason = QuarantineMissingMerchantLink
			} else {
				merchantID = mid
			}
		case ActorTypeSupplier:
			connType = ConnectionTypeSupplySource
			mid, mErr := s.repo.GetSupplierMerchantID(ctx, tx, leg.ActorID)
			if mErr != nil {
				quarantineReason = QuarantineMissingMerchantLink
			} else {
				merchantID = mid
			}
		default:
			quarantineReason = QuarantineAmbiguousOwnerMismatch
		}

		if quarantineReason != "" || merchantID == uuid.Nil {
			summary.QuarantinedCount++
			if !dryRun {
				detailJSON, _ := json.Marshal(map[string]any{
					"actor_type": string(leg.ActorType),
					"actor_id":   leg.ActorID,
					"provider":   string(leg.Provider),
					"name":       leg.Name,
				})
				cross := MigrationCrosswalkRecord{
					ID:                   uuid.New(),
					LegacyConnectionID:   leg.ID,
					LegacyActorType:      string(leg.ActorType),
					LegacyActorID:        leg.ActorID,
					Classification:       MigrationClassQuarantined,
					QuarantineReasonCode: &quarantineReason,
					QuarantineDetails:    detailJSON,
					RunID:                runID,
					CreatedAt:            time.Now().UTC(),
				}
				if err := s.repo.RecordMigrationCrosswalk(ctx, tx, cross); err != nil {
					return nil, fmt.Errorf("failed to record quarantine crosswalk: %w", err)
				}
			}
			continue
		}

		summary.MappedCount++

		if !dryRun {
			targetConnID := uuid.New()
			legacyConnID := leg.ID
			legacyActorType := string(leg.ActorType)
			legacyActorID := leg.ActorID

			mConn := MerchantIntegrationConnection{
				ID:                  targetConnID,
				MerchantID:          merchantID,
				ConnectionType:      connType,
				Provider:            leg.Provider,
				Name:                leg.Name,
				Status:              MerchantStatusActive,
				CredentialsVaultRef: leg.CredentialsVaultRef,
				Settings:            leg.Settings,
				LegacyActorType:     &legacyActorType,
				LegacyActorID:       &legacyActorID,
				LegacyConnectionID:  &legacyConnID,
				CreatedAt:           leg.CreatedAt,
				UpdatedAt:           time.Now().UTC(),
			}

			if err := s.repo.CreateMerchantConnection(ctx, tx, mConn); err != nil {
				// If duplicate constraint error occurs during apply, quarantine instead
				if errors.Is(err, ErrDuplicateAccountConnection) {
					summary.MappedCount--
					summary.QuarantinedCount++
					qReason := QuarantineDuplicateAccount
					detailJSON, _ := json.Marshal(map[string]any{"error": err.Error()})
					cross := MigrationCrosswalkRecord{
						ID:                   uuid.New(),
						LegacyConnectionID:   leg.ID,
						LegacyActorType:      string(leg.ActorType),
						LegacyActorID:        leg.ActorID,
						MerchantID:           &merchantID,
						Classification:       MigrationClassQuarantined,
						QuarantineReasonCode: &qReason,
						QuarantineDetails:    detailJSON,
						RunID:                runID,
						CreatedAt:            time.Now().UTC(),
					}
					_ = s.repo.RecordMigrationCrosswalk(ctx, tx, cross)
					continue
				}
				return nil, fmt.Errorf("failed to migrate connection %s: %w", leg.ID, err)
			}

			cross := MigrationCrosswalkRecord{
				ID:                 uuid.New(),
				LegacyConnectionID: leg.ID,
				LegacyActorType:    string(leg.ActorType),
				LegacyActorID:      leg.ActorID,
				TargetConnectionID: &targetConnID,
				MerchantID:         &merchantID,
				Classification:     MigrationClassMigrated,
				RunID:              runID,
				CreatedAt:          time.Now().UTC(),
			}
			if err := s.repo.RecordMigrationCrosswalk(ctx, tx, cross); err != nil {
				return nil, fmt.Errorf("failed to record migration crosswalk: %w", err)
			}
		}
	}

	if !dryRun {
		payload := map[string]any{
			"run_id":            runID.String(),
			"scanned_count":     summary.ScannedCount,
			"mapped_count":      summary.MappedCount,
			"quarantined_count": summary.QuarantinedCount,
		}

		envelope := events.EventEnvelope{
			EventID:          uuid.New().String(),
			EventType:        EventTypeMerchantMigrationCompleted,
			SchemaVersion:    1,
			AggregateType:    "merchant_integration_migration",
			AggregateID:      runID.String(),
			AggregateVersion: 1,
			OccurredAt:       time.Now().UTC(),
			Payload:          payload,
		}

		if err := s.store.Enqueue(ctx, tx, envelope); err != nil {
			return nil, fmt.Errorf("failed to enqueue migration completion event: %w", err)
		}

		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("failed to commit migration tx: %w", err)
		}
	}

	return summary, nil
}

func strPtrVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
