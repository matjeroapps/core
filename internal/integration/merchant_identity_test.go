package integration

import (
	"context"
	"os"
	"testing"

	"core/internal/merchants"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMerchantIdentityIntegration(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set; skipping database integration test")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect to postgres: %v", err)
	}
	defer pool.Close()

	repo := merchants.NewPostgresRepository(pool)
	svc := merchants.NewService(repo)
	auth := merchants.NewAuthorizer(repo, merchants.AuthModeShadow)

	// 1. Create Merchant
	code := "mer_" + uuid.New().String()[:8]
	m, err := svc.CreateMerchant(ctx, code, "Test Legal Entity", merchants.CapabilityTypeRetail)
	if err != nil {
		t.Fatalf("failed to create merchant: %v", err)
	}

	if m.ID == uuid.Nil {
		t.Errorf("expected non-nil merchant ID")
	}

	// 2. Verify Retail Capability Active
	cap, err := repo.GetCapability(ctx, m.ID, merchants.CapabilityTypeRetail)
	if err != nil {
		t.Fatalf("failed to get retail capability: %v", err)
	}
	if !cap.IsActive() {
		t.Errorf("expected retail capability to be active")
	}

	// 3. Activate Supply Capability
	supplyCap, err := svc.ActivateCapability(ctx, m.ID, merchants.CapabilityTypeSupply)
	if err != nil {
		t.Fatalf("failed to activate supply capability: %v", err)
	}
	if !supplyCap.IsActive() {
		t.Errorf("expected supply capability to be active after activation")
	}

	// 4. Add Team Member & Grant Permissions
	sub := "sub_test_" + uuid.New().String()[:8]
	mem, err := svc.AddMember(ctx, m.ID, sub, []string{merchants.PermissionRetailStoresManage, merchants.PermissionSupplyInventoryManage})
	if err != nil {
		t.Fatalf("failed to add team member: %v", err)
	}

	if mem.ID == uuid.Nil {
		t.Errorf("expected non-nil membership ID")
	}

	// 5. Test Authorization
	if err := auth.Authorize(ctx, m.ID, sub, merchants.CapabilityTypeRetail, merchants.PermissionRetailStoresManage); err != nil {
		t.Errorf("expected authorization to succeed, got %v", err)
	}

	if err := auth.Authorize(ctx, m.ID, sub, merchants.CapabilityTypeRetail, merchants.PermissionFinanceManage); err == nil {
		t.Errorf("expected permission denial for ungranted permission finance.manage")
	}
}
