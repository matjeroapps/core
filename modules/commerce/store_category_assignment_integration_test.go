package commerce

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"core/internal/testdb"
)

// Product ↔ store-category assignment tests (US3). Same-store assignment is
// enforced inside the product write path; legacy global category_ids behaviour
// is untouched.

func setupStoreCategoryAssignmentEnv(t *testing.T) (Service, string, string, string, string, string) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	ctx := context.Background()
	db := testdb.Open(t, dsn)

	applyMigrationBatch(t, db,
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000004_admin_supplier_seller_platforms",
		"000005_store_domain_lifecycle",
		"000006_store_domain_integrity",
		"000008_storefront_revisions",
		"000009_supplier_retail_capability",
		"000010_customer_cart_domain",
		"000011_checkout_sessions",
		"000014_seller_catalog_authoring",
		"000015_media_upload_intent",
		"000025_seller_catalog_phase_b",
		"000026_seller_catalog_phase_c",
		"000044_store_scoped_categories",
	)

	repo := NewRepository(db.Pool)
	service := NewService(repo)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	seller, err := repo.CreateSeller(ctx, "assign-seller-"+suffix, "Assignment Seller", "active", nil)
	if err != nil {
		t.Fatalf("create seller: %v", err)
	}
	subject := "assign-owner-" + suffix
	if _, err := repo.CreateSellerMember(ctx, seller.ID, subject, "owner", "active"); err != nil {
		t.Fatalf("create member: %v", err)
	}
	store, err := repo.CreateStore(ctx, seller.ID, "EG", "assign-store-"+suffix, "Assignment Store", "active", nil)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	sellerB, err := repo.CreateSeller(ctx, "assign-seller-b-"+suffix, "Assignment Seller B", "active", nil)
	if err != nil {
		t.Fatalf("create seller B: %v", err)
	}
	subjectB := "assign-owner-b-" + suffix
	if _, err := repo.CreateSellerMember(ctx, sellerB.ID, subjectB, "owner", "active"); err != nil {
		t.Fatalf("create member B: %v", err)
	}
	storeB, err := repo.CreateStore(ctx, sellerB.ID, "EG", "assign-store-b-"+suffix, "Assignment Store B", "active", nil)
	if err != nil {
		t.Fatalf("create store B: %v", err)
	}
	return service, store.ID, subject, suffix, storeB.ID, subjectB
}

func TestProductStoreCategoryAssignmentLifecycle(t *testing.T) {
	service, storeID, subject, suffix, storeBID, subjectB := setupStoreCategoryAssignmentEnv(t)
	ctx := context.Background()

	catA, err := service.CreateStoreCategoryForSubject(ctx, subject, storeID, "cat-a", nil, nil, storeCategoryTranslations("Category A", ""))
	if err != nil {
		t.Fatalf("create cat A: %v", err)
	}
	catB, err := service.CreateStoreCategoryForSubject(ctx, subject, storeID, "cat-b", nil, nil, storeCategoryTranslations("Category B", "فئة ب"))
	if err != nil {
		t.Fatalf("create cat B: %v", err)
	}

	// A different seller's store, with its own category (cross-store probe).
	catForeign, err := service.CreateStoreCategoryForSubject(ctx, subjectB, storeBID, "cat-foreign", nil, nil, storeCategoryTranslations("Foreign", ""))
	if err != nil {
		t.Fatalf("create foreign category: %v", err)
	}

	draft := SellerProductDraft{
		Slug:             "assigned-product-" + suffix,
		StoreCategoryIDs: []string{catA.ID},
	}
	detail, err := service.CreateSellerProductForSubject(ctx, subject, storeID, draft)
	if err != nil {
		t.Fatalf("create product with store category: %v", err)
	}
	if len(detail.StoreCategories) != 1 || detail.StoreCategories[0].ID != catA.ID || detail.StoreCategories[0].Name != "Category A" {
		t.Fatalf("detail refs = %+v, want one readable ref to Category A", detail.StoreCategories)
	}

	productID := detail.Product.ID

	// Replace semantics: non-nil replaces the whole set.
	if _, err := service.UpdateSellerProductForSubject(ctx, subject, storeID, productID, "", nil, nil, []string{catA.ID, catB.ID}); err != nil {
		t.Fatalf("update with two categories: %v", err)
	}
	updated, err := service.GetSellerProductDetailForSubject(ctx, subject, storeID, productID)
	if err != nil {
		t.Fatalf("get detail: %v", err)
	}
	if len(updated.StoreCategories) != 2 {
		t.Fatalf("store categories = %d, want 2", len(updated.StoreCategories))
	}

	// Nil leaves assignments untouched.
	if _, err := service.UpdateSellerProductForSubject(ctx, subject, storeID, productID, "", nil, nil, nil); err != nil {
		t.Fatalf("update with nil: %v", err)
	}
	stillTwo, err := service.GetSellerProductDetailForSubject(ctx, subject, storeID, productID)
	if err != nil {
		t.Fatalf("get detail after nil: %v", err)
	}
	if len(stillTwo.StoreCategories) != 2 {
		t.Fatalf("nil update changed assignments: %d", len(stillTwo.StoreCategories))
	}

	// Empty slice clears assignments.
	if _, err := service.UpdateSellerProductForSubject(ctx, subject, storeID, productID, "", nil, nil, []string{}); err != nil {
		t.Fatalf("update with empty: %v", err)
	}
	cleared, err := service.GetSellerProductDetailForSubject(ctx, subject, storeID, productID)
	if err != nil {
		t.Fatalf("get detail after empty: %v", err)
	}
	if len(cleared.StoreCategories) != 0 {
		t.Fatalf("empty update did not clear assignments: %d", len(cleared.StoreCategories))
	}

	// Cross-store assignment is rejected even through the service API, and the
	// (now empty) assignment set is unchanged.
	if _, err := service.UpdateSellerProductForSubject(ctx, subject, storeID, productID, "", nil, nil, []string{catForeign.ID}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cross-store update: err = %v, want ErrInvalidInput", err)
	}
	afterReject, err := service.GetSellerProductDetailForSubject(ctx, subject, storeID, productID)
	if err != nil {
		t.Fatalf("get detail after reject: %v", err)
	}
	if len(afterReject.StoreCategories) != 0 {
		t.Fatalf("rejected cross-store update partially wrote: %d", len(afterReject.StoreCategories))
	}

	// Cross-store create is rejected atomically (no product, no assignment).
	badDraft := SellerProductDraft{Slug: "bad-product-" + suffix, StoreCategoryIDs: []string{catForeign.ID}}
	if _, err := service.CreateSellerProductForSubject(ctx, subject, storeID, badDraft); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("cross-store create: err = %v, want ErrInvalidInput", err)
	}
	items, _, err := service.ListSellerProductsForSubject(ctx, subject, storeID, "", "", "bad-product-"+suffix, 10, 0)
	if err != nil {
		t.Fatalf("list products after reject: %v", err)
	}
	if len(items) != 0 {
		t.Fatal("rejected cross-store create must not leave the product behind")
	}
}
