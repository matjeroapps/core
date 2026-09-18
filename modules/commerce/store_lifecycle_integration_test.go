package commerce_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/google/uuid"
	"core/internal/testdb"
	"core/modules/commerce"
	"core/packages/database"
)

type testEnv struct {
	db      *database.Pool
	repo    commerce.Repository
	service commerce.Service
	ctx     context.Context
}

func setupTestEnv(t *testing.T, dsn string) *testEnv {
	t.Helper()
	db := testdb.Open(t, dsn)
	for _, m := range []string{
		"000001_event_delivery_foundation",
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000004_admin_supplier_seller_platforms",
		"000005_store_domain_lifecycle",
		"000006_store_domain_integrity",
		"000007_theme_engine_schema",
		"000008_storefront_revisions",
		"000009_supplier_retail_capability",
		"000010_customer_cart_domain",
		"000011_checkout_sessions",
		"000024_store_lifecycle_and_entitlements",
	} {
		applySQLFile(t, db, filepath.Join("..", "..", "migrations", m+".up.sql"))
	}
	ctx := context.Background()

	repo := commerce.NewRepository(db.Pool)
	svc := commerce.NewService(repo)
	svc.StoreEntitlement = commerce.NewStoreEntitlementPolicy(1)

	return &testEnv{
		db:      db,
		repo:    repo,
		service: svc,
		ctx:     ctx,
	}
}

func applySQLFile(t *testing.T, db *database.Pool, path string) {
	t.Helper()

	sqlBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}

	if _, err := db.Exec(context.Background(), string(sqlBytes)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
}

type testSeller struct {
	Seller       commerce.Seller
	OwnerSubject string
	MgrSubject   string
	StaffSubject string
}

func createTestSellerWithMembers(t *testing.T, env *testEnv, suffix string) testSeller {
	t.Helper()
	seller, err := env.repo.CreateSeller(env.ctx, "sel-"+suffix, "Seller "+suffix, "active", nil)
	if err != nil {
		t.Fatalf("create seller: %v", err)
	}

	ownerSub := "subj-owner-" + suffix
	mgrSub := "subj-mgr-" + suffix
	staffSub := "subj-staff-" + suffix

	if _, err := env.repo.CreateSellerMember(env.ctx, seller.ID, ownerSub, "owner", "active"); err != nil {
		t.Fatalf("create owner member: %v", err)
	}
	if _, err := env.repo.CreateSellerMember(env.ctx, seller.ID, mgrSub, "manager", "active"); err != nil {
		t.Fatalf("create manager member: %v", err)
	}
	if _, err := env.repo.CreateSellerMember(env.ctx, seller.ID, staffSub, "staff", "active"); err != nil {
		t.Fatalf("create staff member: %v", err)
	}

	return testSeller{
		Seller:       seller,
		OwnerSubject: ownerSub,
		MgrSubject:   mgrSub,
		StaffSubject: staffSub,
	}
}

func TestStoreLifecycleAndEntitlementIntegration(t *testing.T) {
	dsn := "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	env := setupTestEnv(t, dsn)

	suffix := uuid.NewString()[:8]
	sellerA := createTestSellerWithMembers(t, env, "a-"+suffix)

	// 1. Create 3 draft stores for Seller A
	storeA1, err := env.service.CreateStoreForSubject(env.ctx, sellerA.OwnerSubject, sellerA.Seller.ID, "EG", "store-a1-"+suffix, "Store A1", "draft", nil)
	if err != nil {
		t.Fatalf("CreateStore A1: %v", err)
	}
	if storeA1.Status != "draft" {
		t.Fatalf("expected status draft, got %s", storeA1.Status)
	}

	storeA2, err := env.service.CreateStoreForSubject(env.ctx, sellerA.OwnerSubject, sellerA.Seller.ID, "EG", "store-a2-"+suffix, "Store A2", "draft", nil)
	if err != nil {
		t.Fatalf("CreateStore A2: %v", err)
	}

	storeA3, err := env.service.CreateStoreForSubject(env.ctx, sellerA.OwnerSubject, sellerA.Seller.ID, "EG", "store-a3-"+suffix, "Store A3", "draft", nil)
	if err != nil {
		t.Fatalf("CreateStore A3: %v", err)
	}

	// Verify draft stores do not consume active entitlement slots
	activeCount, err := env.repo.CountActiveStoresBySellerID(env.ctx, sellerA.Seller.ID)
	if err != nil {
		t.Fatalf("CountActiveStores: %v", err)
	}
	if activeCount != 0 {
		t.Fatalf("expected 0 active stores, got %d", activeCount)
	}

	// 2. Activate Store A1 -> succeeds (active count becomes 1)
	updatedA1, err := env.service.UpdateStoreStatusForSubject(env.ctx, sellerA.OwnerSubject, storeA1.ID, "active")
	if err != nil {
		t.Fatalf("Activate A1: %v", err)
	}
	if updatedA1.Status != "active" {
		t.Fatalf("expected status active, got %s", updatedA1.Status)
	}

	// 3. Attempting to activate Store A2 while Store A1 is active exceeds default limit of 1 -> ErrStoreEntitlementExceeded
	_, err = env.service.UpdateStoreStatusForSubject(env.ctx, sellerA.OwnerSubject, storeA2.ID, "active")
	if !errors.Is(err, commerce.ErrStoreEntitlementExceeded) {
		t.Fatalf("expected ErrStoreEntitlementExceeded activating A2, got: %v", err)
	}

	// 4. Same-state retry on Store A1 -> idempotent success
	updatedA1Re, err := env.service.UpdateStoreStatusForSubject(env.ctx, sellerA.OwnerSubject, storeA1.ID, "active")
	if err != nil {
		t.Fatalf("Re-activate A1: %v", err)
	}
	if updatedA1Re.Status != "active" {
		t.Fatalf("expected status active on retry, got %s", updatedA1Re.Status)
	}

	// 5. Deactivate Store A1 -> ACTIVE -> INACTIVE (releases active slot)
	inactA1, err := env.service.UpdateStoreStatusForSubject(env.ctx, sellerA.OwnerSubject, storeA1.ID, "inactive")
	if err != nil {
		t.Fatalf("Deactivate A1: %v", err)
	}
	if inactA1.Status != "inactive" {
		t.Fatalf("expected status inactive, got %s", inactA1.Status)
	}

	// 6. Now activate Store A2 -> succeeds!
	updatedA2, err := env.service.UpdateStoreStatusForSubject(env.ctx, sellerA.OwnerSubject, storeA2.ID, "active")
	if err != nil {
		t.Fatalf("Activate A2 after deactivating A1: %v", err)
	}
	if updatedA2.Status != "active" {
		t.Fatalf("expected status active for A2, got %s", updatedA2.Status)
	}

	// 7. Invalid transition: ACTIVE -> DRAFT or INACTIVE -> DRAFT returns ErrInvalidTransition
	_, err = env.service.UpdateStoreStatusForSubject(env.ctx, sellerA.OwnerSubject, storeA2.ID, "draft")
	if !errors.Is(err, commerce.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for ACTIVE->DRAFT, got: %v", err)
	}

	_, err = env.service.UpdateStoreStatusForSubject(env.ctx, sellerA.OwnerSubject, storeA1.ID, "draft")
	if !errors.Is(err, commerce.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for INACTIVE->DRAFT, got: %v", err)
	}

	// 8. Custom higher limit test: set limit = 2
	env.service.StoreEntitlement = commerce.NewStoreEntitlementPolicy(2)

	// Now Store A2 is active. Reactivating Store A3 should succeed because limit is 2.
	updatedA3, err := env.service.UpdateStoreStatusForSubject(env.ctx, sellerA.OwnerSubject, storeA3.ID, "active")
	if err != nil {
		t.Fatalf("Activate A3 under limit=2: %v", err)
	}
	if updatedA3.Status != "active" {
		t.Fatalf("expected status active for A3, got %s", updatedA3.Status)
	}

	// Activating Store A1 now (making 3 active stores) should fail
	_, err = env.service.UpdateStoreStatusForSubject(env.ctx, sellerA.OwnerSubject, storeA1.ID, "active")
	if !errors.Is(err, commerce.ErrStoreEntitlementExceeded) {
		t.Fatalf("expected ErrStoreEntitlementExceeded under limit=2 for 3rd active store, got: %v", err)
	}
}

func TestStoreActivationConcurrencyIntegration(t *testing.T) {
	dsn := "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	env := setupTestEnv(t, dsn)
	env.service.StoreEntitlement = commerce.NewStoreEntitlementPolicy(1)

	suffix := uuid.NewString()[:8]
	seller := createTestSellerWithMembers(t, env, "conc-"+suffix)

	const numStores = 5
	stores := make([]commerce.Store, numStores)
	for i := 0; i < numStores; i++ {
		st, err := env.service.CreateStoreForSubject(env.ctx, seller.OwnerSubject, seller.Seller.ID, "EG", fmt.Sprintf("store-c%d-%s", i, suffix), fmt.Sprintf("Store C%d", i), "draft", nil)
		if err != nil {
			t.Fatalf("CreateStore C%d: %v", i, err)
		}
		stores[i] = st
	}

	// Concurrently attempt to activate all 5 stores for the same seller
	var wg sync.WaitGroup
	errs := make([]error, numStores)
	startSignal := make(chan struct{})

	for i := 0; i < numStores; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-startSignal
			_, errs[idx] = env.service.UpdateStoreStatusForSubject(env.ctx, seller.OwnerSubject, stores[idx].ID, "active")
		}(i)
	}

	close(startSignal)
	wg.Wait()

	activeCount, err := env.repo.CountActiveStoresBySellerID(env.ctx, seller.Seller.ID)
	if err != nil {
		t.Fatalf("CountActiveStores: %v", err)
	}

	if activeCount != 1 {
		t.Fatalf("expected exactly 1 active store after concurrent activation attempts, got %d", activeCount)
	}

	successCount := 0
	entitlementErrCount := 0
	for _, err := range errs {
		if err == nil {
			successCount++
		} else if errors.Is(err, commerce.ErrStoreEntitlementExceeded) {
			entitlementErrCount++
		}
	}

	if successCount != 1 {
		t.Errorf("expected exactly 1 success, got %d", successCount)
	}
	if entitlementErrCount != numStores-1 {
		t.Errorf("expected %d entitlement errors, got %d", numStores-1, entitlementErrCount)
	}
}

func TestRoleAndTenantIsolationIntegration(t *testing.T) {
	dsn := "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	env := setupTestEnv(t, dsn)

	suffix := uuid.NewString()[:8]
	sellerA := createTestSellerWithMembers(t, env, "iso-a-"+suffix)
	sellerB := createTestSellerWithMembers(t, env, "iso-b-"+suffix)

	storeA1, err := env.service.CreateStoreForSubject(env.ctx, sellerA.OwnerSubject, sellerA.Seller.ID, "EG", "store-iso-a1-"+suffix, "Store A1", "draft", nil)
	if err != nil {
		t.Fatalf("CreateStore A1: %v", err)
	}

	// 1. Role Authorization Matrix Tests for Store Mutations (Owner Only)
	t.Run("manager cannot create store", func(t *testing.T) {
		_, err := env.service.CreateStoreForSubject(env.ctx, sellerA.MgrSubject, sellerA.Seller.ID, "EG", "store-iso-a2-"+suffix, "Store A2", "draft", nil)
		if !errors.Is(err, commerce.ErrForbidden) {
			t.Fatalf("expected ErrForbidden for manager store creation, got: %v", err)
		}
	})

	t.Run("staff cannot create store", func(t *testing.T) {
		_, err := env.service.CreateStoreForSubject(env.ctx, sellerA.StaffSubject, sellerA.Seller.ID, "EG", "store-iso-a3-"+suffix, "Store A3", "draft", nil)
		if !errors.Is(err, commerce.ErrForbidden) {
			t.Fatalf("expected ErrForbidden for staff store creation, got: %v", err)
		}
	})

	t.Run("manager cannot change store status", func(t *testing.T) {
		_, err := env.service.UpdateStoreStatusForSubject(env.ctx, sellerA.MgrSubject, storeA1.ID, "active")
		if !errors.Is(err, commerce.ErrForbidden) {
			t.Fatalf("expected ErrForbidden for manager status update, got: %v", err)
		}
	})

	t.Run("staff cannot change store status", func(t *testing.T) {
		_, err := env.service.UpdateStoreStatusForSubject(env.ctx, sellerA.StaffSubject, storeA1.ID, "active")
		if !errors.Is(err, commerce.ErrForbidden) {
			t.Fatalf("expected ErrForbidden for staff status update, got: %v", err)
		}
	})

	// 2. Cross-Seller Isolation (Tenant Enumeration Prevention)
	t.Run("seller B owner accessing seller A store returns ErrNotFound", func(t *testing.T) {
		_, err := env.service.UpdateStoreStatusForSubject(env.ctx, sellerB.OwnerSubject, storeA1.ID, "active")
		if !errors.Is(err, commerce.ErrNotFound) {
			t.Fatalf("expected ErrNotFound for cross-tenant access, got: %v", err)
		}
	})

	t.Run("seller B owner creating store under seller A returns ErrNotFound", func(t *testing.T) {
		_, err := env.service.CreateStoreForSubject(env.ctx, sellerB.OwnerSubject, sellerA.Seller.ID, "EG", "store-illegal-"+suffix, "Illegal Store", "draft", nil)
		if !errors.Is(err, commerce.ErrNotFound) {
			t.Fatalf("expected ErrNotFound when requesting another seller ID, got: %v", err)
		}
	})

	// 3. Unauthenticated / Unknown Subject
	t.Run("unknown subject returns ErrForbidden", func(t *testing.T) {
		_, err := env.service.CreateStoreForSubject(env.ctx, "subj-nonexistent", sellerA.Seller.ID, "EG", "store-anon-"+suffix, "Anon Store", "draft", nil)
		if !errors.Is(err, commerce.ErrForbidden) {
			t.Fatalf("expected ErrForbidden for unknown subject, got: %v", err)
		}
	})
}
