package integration

import (
	"context"

	"github.com/jackc/pgx/v5"

	"core/modules/commerce"
)

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
}
