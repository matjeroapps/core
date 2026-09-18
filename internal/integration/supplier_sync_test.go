package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"core/internal/integration"
	"core/modules/commerce"
)

type mockIntegrationRepo struct {
	connections    []integration.Connection
	mappings       []integration.EntityMapping
	cursors        []integration.SyncCursor
	inbox          []integration.WebhookInboxItem
	syncJobs       []integration.SupplierSyncJob
	sellerSyncJobs []integration.SellerSyncJob
}

func (m *mockIntegrationRepo) CreateConnection(ctx context.Context, tx pgx.Tx, conn integration.Connection) error {
	m.connections = append(m.connections, conn)
	return nil
}

func (m *mockIntegrationRepo) GetConnectionByID(ctx context.Context, tx pgx.Tx, id string) (*integration.Connection, error) {
	for _, c := range m.connections {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, integration.ErrConnectionNotFound
}

func (m *mockIntegrationRepo) ListConnectionsByActor(ctx context.Context, tx pgx.Tx, actorType integration.ActorType, actorID string, page commerce.Page) ([]integration.Connection, error) {
	var result []integration.Connection
	for _, c := range m.connections {
		if c.ActorType == actorType && c.ActorID == actorID {
			result = append(result, c)
		}
	}
	return result, nil
}

func (m *mockIntegrationRepo) UpdateConnectionStatus(ctx context.Context, tx pgx.Tx, id string, status integration.ConnectionStatus) (*integration.Connection, error) {
	for i, c := range m.connections {
		if c.ID == id {
			m.connections[i].Status = status
			return &m.connections[i], nil
		}
	}
	return nil, integration.ErrConnectionNotFound
}

func (m *mockIntegrationRepo) UpsertEntityMapping(ctx context.Context, tx pgx.Tx, mapping integration.EntityMapping) error {
	m.mappings = append(m.mappings, mapping)
	return nil
}

func (m *mockIntegrationRepo) GetMappingByExternalID(ctx context.Context, tx pgx.Tx, connectionID string, entityType integration.EntityType, externalID string) (*integration.EntityMapping, error) {
	for _, mapItem := range m.mappings {
		if mapItem.ConnectionID == connectionID && mapItem.EntityType == entityType && mapItem.ExternalID == externalID {
			return &mapItem, nil
		}
	}
	return nil, integration.ErrMappingNotFound
}

func (m *mockIntegrationRepo) GetMappingByInternalID(ctx context.Context, tx pgx.Tx, connectionID string, entityType integration.EntityType, internalID string) (*integration.EntityMapping, error) {
	for _, mapItem := range m.mappings {
		if mapItem.ConnectionID == connectionID && mapItem.EntityType == entityType && mapItem.InternalID == internalID {
			return &mapItem, nil
		}
	}
	return nil, integration.ErrMappingNotFound
}

func (m *mockIntegrationRepo) ListEntityMappings(ctx context.Context, tx pgx.Tx, connectionID string, entityType integration.EntityType, page commerce.Page) ([]integration.EntityMapping, error) {
	var result []integration.EntityMapping
	for _, mapItem := range m.mappings {
		if mapItem.ConnectionID == connectionID && (entityType == "" || mapItem.EntityType == entityType) {
			result = append(result, mapItem)
		}
	}
	return result, nil
}

func (m *mockIntegrationRepo) UpsertSyncCursor(ctx context.Context, tx pgx.Tx, cursor integration.SyncCursor) error {
	m.cursors = append(m.cursors, cursor)
	return nil
}

func (m *mockIntegrationRepo) GetSyncCursor(ctx context.Context, tx pgx.Tx, connectionID string, entityType integration.EntityType) (*integration.SyncCursor, error) {
	for _, c := range m.cursors {
		if c.ConnectionID == connectionID && c.EntityType == entityType {
			return &c, nil
		}
	}
	return nil, integration.ErrSyncCursorNotFound
}

func (m *mockIntegrationRepo) PersistWebhookInbox(ctx context.Context, tx pgx.Tx, item integration.WebhookInboxItem) (*integration.WebhookInboxItem, bool, error) {
	m.inbox = append(m.inbox, item)
	return &item, true, nil
}

func (m *mockIntegrationRepo) CreateSyncJob(ctx context.Context, tx pgx.Tx, job integration.SupplierSyncJob) (*integration.SupplierSyncJob, error) {
	m.syncJobs = append(m.syncJobs, job)
	return &job, nil
}

func (m *mockIntegrationRepo) GetSyncJobByID(ctx context.Context, tx pgx.Tx, id string) (*integration.SupplierSyncJob, error) {
	for _, j := range m.syncJobs {
		if j.ID == id {
			return &j, nil
		}
	}
	return nil, integration.ErrSyncJobNotFound
}

func (m *mockIntegrationRepo) UpdateSyncJobStatus(ctx context.Context, tx pgx.Tx, id string, status integration.SyncJobStatus, processed, failed int, errSummary string) (*integration.SupplierSyncJob, error) {
	for i, j := range m.syncJobs {
		if j.ID == id {
			m.syncJobs[i].Status = status
			m.syncJobs[i].ProcessedItems = processed
			m.syncJobs[i].FailedItems = failed
			m.syncJobs[i].ErrorSummary = errSummary
			now := time.Now().UTC()
			m.syncJobs[i].UpdatedAt = now
			if status == integration.SyncJobStatusCompleted || status == integration.SyncJobStatusFailed {
				m.syncJobs[i].CompletedAt = &now
			}
			return &m.syncJobs[i], nil
		}
	}
	return nil, integration.ErrSyncJobNotFound
}

func (m *mockIntegrationRepo) ListSyncJobsBySupplier(ctx context.Context, tx pgx.Tx, supplierID string, page commerce.Page) ([]integration.SupplierSyncJob, error) {
	var result []integration.SupplierSyncJob
	for _, j := range m.syncJobs {
		if j.SupplierID == supplierID {
			result = append(result, j)
		}
	}
	return result, nil
}

func TestSupplierSyncJobLifecycle(t *testing.T) {
	mockRepo := &mockIntegrationRepo{}
	svc := integration.NewService(mockRepo)
	ctx := context.Background()

	// Create Sync Job
	job, err := svc.CreateSyncJob(ctx, "conn-123", "supplier-456")
	if err != nil {
		t.Fatalf("CreateSyncJob failed: %v", err)
	}
	if job.Status != integration.SyncJobStatusQueued {
		t.Errorf("status = %s, want %s", job.Status, integration.SyncJobStatusQueued)
	}

	// Get Sync Job
	fetched, err := svc.GetSyncJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetSyncJob failed: %v", err)
	}
	if fetched.SupplierID != "supplier-456" {
		t.Errorf("supplier_id = %s, want supplier-456", fetched.SupplierID)
	}

	// Update Sync Job Status
	updated, err := svc.UpdateSyncJobStatus(ctx, job.ID, integration.SyncJobStatusCompleted, 10, 0, "")
	if err != nil {
		t.Fatalf("UpdateSyncJobStatus failed: %v", err)
	}
	if updated.Status != integration.SyncJobStatusCompleted {
		t.Errorf("updated status = %s, want completed", updated.Status)
	}
	if updated.CompletedAt == nil {
		t.Errorf("completed_at is nil, want non-nil timestamp")
	}

	// List Sync Jobs
	jobs, err := svc.ListSyncJobsBySupplier(ctx, "supplier-456", commerce.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListSyncJobsBySupplier failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs length = %d, want 1", len(jobs))
	}
}
