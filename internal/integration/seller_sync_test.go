package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"core/internal/integration"
	"core/modules/commerce"
)

func (m *mockIntegrationRepo) CreateSellerSyncJob(ctx context.Context, tx pgx.Tx, job integration.SellerSyncJob) (*integration.SellerSyncJob, error) {
	m.sellerSyncJobs = append(m.sellerSyncJobs, job)
	return &job, nil
}

func (m *mockIntegrationRepo) GetSellerSyncJobByID(ctx context.Context, tx pgx.Tx, id string) (*integration.SellerSyncJob, error) {
	for _, j := range m.sellerSyncJobs {
		if j.ID == id {
			return &j, nil
		}
	}
	return nil, integration.ErrSellerSyncJobNotFound
}

func (m *mockIntegrationRepo) UpdateSellerSyncJobStatus(ctx context.Context, tx pgx.Tx, id string, status integration.SyncJobStatus, processed, failed int, errSummary string) (*integration.SellerSyncJob, error) {
	for i, j := range m.sellerSyncJobs {
		if j.ID == id {
			m.sellerSyncJobs[i].Status = status
			m.sellerSyncJobs[i].ProcessedItems = processed
			m.sellerSyncJobs[i].FailedItems = failed
			m.sellerSyncJobs[i].ErrorSummary = errSummary
			now := time.Now().UTC()
			m.sellerSyncJobs[i].UpdatedAt = now
			if status == integration.SyncJobStatusCompleted || status == integration.SyncJobStatusFailed {
				m.sellerSyncJobs[i].CompletedAt = &now
			}
			return &m.sellerSyncJobs[i], nil
		}
	}
	return nil, integration.ErrSellerSyncJobNotFound
}

func (m *mockIntegrationRepo) ListSellerSyncJobsByStore(ctx context.Context, tx pgx.Tx, storeID string, page commerce.Page) ([]integration.SellerSyncJob, error) {
	var result []integration.SellerSyncJob
	for _, j := range m.sellerSyncJobs {
		if j.StoreID == storeID {
			result = append(result, j)
		}
	}
	return result, nil
}

func TestSellerSyncJobLifecycle(t *testing.T) {
	mockRepo := &mockIntegrationRepo{}
	svc := integration.NewService(mockRepo)
	ctx := context.Background()

	// Create Seller Sync Job
	job, err := svc.CreateSellerSyncJob(ctx, "conn-999", "store-123", "catalog_import")
	if err != nil {
		t.Fatalf("CreateSellerSyncJob failed: %v", err)
	}
	if job.Status != integration.SyncJobStatusQueued {
		t.Errorf("status = %s, want %s", job.Status, integration.SyncJobStatusQueued)
	}

	// Get Seller Sync Job
	fetched, err := svc.GetSellerSyncJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("GetSellerSyncJob failed: %v", err)
	}
	if fetched.StoreID != "store-123" {
		t.Errorf("store_id = %s, want store-123", fetched.StoreID)
	}

	// Update Seller Sync Job Status
	updated, err := svc.UpdateSellerSyncJobStatus(ctx, job.ID, integration.SyncJobStatusCompleted, 25, 0, "")
	if err != nil {
		t.Fatalf("UpdateSellerSyncJobStatus failed: %v", err)
	}
	if updated.Status != integration.SyncJobStatusCompleted {
		t.Errorf("updated status = %s, want completed", updated.Status)
	}
	if updated.CompletedAt == nil {
		t.Errorf("completed_at is nil, want non-nil timestamp")
	}

	// List Seller Sync Jobs
	jobs, err := svc.ListSellerSyncJobsByStore(ctx, "store-123", commerce.Page{Limit: 10})
	if err != nil {
		t.Fatalf("ListSellerSyncJobsByStore failed: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs length = %d, want 1", len(jobs))
	}
}
