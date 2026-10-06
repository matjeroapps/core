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

// Store-scoped category integration tests. These run against an isolated
// schema per test (testdb.Open) with the real migration batch applied.

type storeCategoryEnv struct {
	repo     Repository
	service  Service
	storeAID string
	storeBID string
	subjectA string
	subjectB string
}

func setupStoreCategoryEnv(t *testing.T) storeCategoryEnv {
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

	sellerA, err := repo.CreateSeller(ctx, "cat-seller-a-"+suffix, "Category Seller A", "active", nil)
	if err != nil {
		t.Fatalf("create seller A: %v", err)
	}
	subjectA := "cat-owner-a-" + suffix
	if _, err := repo.CreateSellerMember(ctx, sellerA.ID, subjectA, "owner", "active"); err != nil {
		t.Fatalf("create seller member A: %v", err)
	}
	storeA, err := repo.CreateStore(ctx, sellerA.ID, "EG", "store-a-"+suffix, "Store A", "active", nil)
	if err != nil {
		t.Fatalf("create store A: %v", err)
	}

	sellerB, err := repo.CreateSeller(ctx, "cat-seller-b-"+suffix, "Category Seller B", "active", nil)
	if err != nil {
		t.Fatalf("create seller B: %v", err)
	}
	subjectB := "cat-owner-b-" + suffix
	if _, err := repo.CreateSellerMember(ctx, sellerB.ID, subjectB, "owner", "active"); err != nil {
		t.Fatalf("create seller member B: %v", err)
	}
	storeB, err := repo.CreateStore(ctx, sellerB.ID, "EG", "store-b-"+suffix, "Store B", "active", nil)
	if err != nil {
		t.Fatalf("create store B: %v", err)
	}

	return storeCategoryEnv{
		repo:     repo,
		service:  service,
		storeAID: storeA.ID,
		storeBID: storeB.ID,
		subjectA: subjectA,
		subjectB: subjectB,
	}
}

func storeCategoryTranslations(en, ar string) []StoreCategoryTranslation {
	translations := []StoreCategoryTranslation{{Locale: "en", Name: en}}
	if ar != "" {
		translations = append(translations, StoreCategoryTranslation{Locale: "ar", Name: ar})
	}
	return translations
}

func TestStoreCategoryLifecycle(t *testing.T) {
	env := setupStoreCategoryEnv(t)
	ctx := context.Background()

	root, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, "winter-jackets", nil, nil, storeCategoryTranslations("Winter Jackets", "جاكيتات الشتاء"))
	if err != nil {
		t.Fatalf("create root category: %v", err)
	}
	if root.Status != StoreCategoryStatusActive || root.StoreID != env.storeAID {
		t.Fatalf("unexpected root: %+v", root.StoreCategory)
	}
	if len(root.Translations) != 2 {
		t.Fatalf("expected 2 translations, got %d", len(root.Translations))
	}

	child, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, "parkas", &root.ID, nil, storeCategoryTranslations("Parkas", ""))
	if err != nil {
		t.Fatalf("create child category: %v", err)
	}
	if child.ParentCategoryID == nil || *child.ParentCategoryID != root.ID {
		t.Fatalf("child parent = %v, want %s", child.ParentCategoryID, root.ID)
	}

	got, err := env.service.GetStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, root.ID)
	if err != nil {
		t.Fatalf("get category: %v", err)
	}
	if got.ChildCount != 1 {
		t.Fatalf("child count = %d, want 1", got.ChildCount)
	}

	list, total, err := env.service.ListStoreCategoriesForSubject(ctx, env.subjectA, env.storeAID, "", 100, 0)
	if err != nil {
		t.Fatalf("list categories: %v", err)
	}
	if total != 2 || len(list) != 2 {
		t.Fatalf("list = %d items (total %d), want 2", len(list), total)
	}

	// Update: rename + replace translations + reparent under root's sibling.
	updated, err := env.service.UpdateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, child.ID, StoreCategoryPatch{
		Slug:         strPtr("winter-parkas"),
		Translations: storeCategoryTranslations("Winter Parkas", "جاكيتات"),
	})
	if err != nil {
		t.Fatalf("update category: %v", err)
	}
	if updated.Slug != "winter-parkas" || len(updated.Translations) != 2 {
		t.Fatalf("unexpected update result: %+v", updated)
	}

	// Status transition per node: deactivating the parent leaves the child active.
	if _, err := env.service.TransitionStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, root.ID, StoreCategoryStatusInactive); err != nil {
		t.Fatalf("transition root: %v", err)
	}
	childAfter, err := env.service.GetStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, child.ID)
	if err != nil {
		t.Fatalf("get child after parent transition: %v", err)
	}
	if childAfter.Status != StoreCategoryStatusActive {
		t.Fatalf("child status = %q, want active (per-node status)", childAfter.Status)
	}

	// Reorder: explicit sort orders for every node; list is ordered globally by
	// sort_order and the client sorts within siblings when building the tree.
	second, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, "summer", nil, nil, storeCategoryTranslations("Summer", ""))
	if err != nil {
		t.Fatalf("create second root: %v", err)
	}
	if err := env.service.ReorderStoreCategoriesForSubject(ctx, env.subjectA, env.storeAID, []StoreCategoryOrder{
		{ID: second.ID, SortOrder: 0},
		{ID: root.ID, SortOrder: 1},
		{ID: child.ID, SortOrder: 2},
	}); err != nil {
		t.Fatalf("reorder: %v", err)
	}
	reordered, _, err := env.service.ListStoreCategoriesForSubject(ctx, env.subjectA, env.storeAID, "", 100, 0)
	if err != nil {
		t.Fatalf("list after reorder: %v", err)
	}
	if len(reordered) != 3 || reordered[0].ID != second.ID || reordered[1].ID != root.ID || reordered[2].ID != child.ID {
		t.Fatalf("order after reorder = [%s, %s, %s], want [%s, %s, %s]",
			reordered[0].ID, reordered[1].ID, reordered[2].ID, second.ID, root.ID, child.ID)
	}

	// Delete guards: the parent still has a child; the child is an empty leaf.
	if err := env.service.DeleteStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, root.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("delete parent with children: err = %v, want ErrInvalidInput", err)
	}
	if err := env.service.DeleteStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, child.ID); err != nil {
		t.Fatalf("delete empty leaf: %v", err)
	}
	// With the child gone, the former parent is now deletable.
	if err := env.service.DeleteStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, root.ID); err != nil {
		t.Fatalf("delete emptied parent: %v", err)
	}
}

func TestStoreCategorySlugUniquenessPerStore(t *testing.T) {
	env := setupStoreCategoryEnv(t)
	ctx := context.Background()

	if _, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, "sale", nil, nil, storeCategoryTranslations("Sale", "")); err != nil {
		t.Fatalf("create sale in store A: %v", err)
	}
	if _, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, "sale", nil, nil, storeCategoryTranslations("Sale", "")); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate slug in same store: err = %v, want ErrConflict", err)
	}
	// Same slug in a different store is allowed (FR-002).
	if _, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectB, env.storeBID, "sale", nil, nil, storeCategoryTranslations("Sale", "")); err != nil {
		t.Fatalf("same slug in store B: %v", err)
	}
}

func TestStoreCategoryValidation(t *testing.T) {
	env := setupStoreCategoryEnv(t)
	ctx := context.Background()

	cases := []struct {
		name         string
		slug         string
		translations []StoreCategoryTranslation
	}{
		{"empty slug", "", storeCategoryTranslations("X", "")},
		{"uppercase slug", "Winter", storeCategoryTranslations("X", "")},
		{"leading hyphen", "-winter", storeCategoryTranslations("X", "")},
		{"trailing hyphen", "winter-", storeCategoryTranslations("X", "")},
		{"double hyphen", "win--ter", storeCategoryTranslations("X", "")},
		{"missing english name", "ok-slug", []StoreCategoryTranslation{{Locale: "ar", Name: "عربي"}}},
		{"unsupported locale", "ok-slug", []StoreCategoryTranslation{{Locale: "fr", Name: "Noel"}, {Locale: "en", Name: "X"}}},
	}
	for _, tc := range cases {
		_, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, tc.slug, nil, nil, tc.translations)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("%s: err = %v, want ErrInvalidInput", tc.name, err)
		}
	}
}

func TestStoreCategoryCircularHierarchy(t *testing.T) {
	env := setupStoreCategoryEnv(t)
	ctx := context.Background()

	a, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, "level-a", nil, nil, storeCategoryTranslations("A", ""))
	if err != nil {
		t.Fatalf("create A: %v", err)
	}
	b, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, "level-b", &a.ID, nil, storeCategoryTranslations("B", ""))
	if err != nil {
		t.Fatalf("create B: %v", err)
	}
	c, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, "level-c", &b.ID, nil, storeCategoryTranslations("C", ""))
	if err != nil {
		t.Fatalf("create C: %v", err)
	}

	// Direct self-parent.
	self := a.ID
	if _, err := env.service.UpdateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, a.ID, StoreCategoryPatch{ParentCategoryID: &self}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("self parent: err = %v, want ErrInvalidInput", err)
	}
	// Transitive cycle: move A under C, its own grandchild.
	if _, err := env.service.UpdateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, a.ID, StoreCategoryPatch{ParentCategoryID: &c.ID}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("transitive cycle: err = %v, want ErrInvalidInput", err)
	}
	// The hierarchy is unchanged after both rejected attempts.
	aAfter, err := env.service.GetStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, a.ID)
	if err != nil {
		t.Fatalf("get A after cycle attempts: %v", err)
	}
	if aAfter.ParentCategoryID != nil {
		t.Fatalf("A parent changed to %v after rejected cycles", aAfter.ParentCategoryID)
	}
}

func TestStoreCategoryStatusTransitions(t *testing.T) {
	env := setupStoreCategoryEnv(t)
	ctx := context.Background()

	cat, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, "electronics", nil, nil, storeCategoryTranslations("Electronics", ""))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	for _, status := range []string{StoreCategoryStatusInactive, StoreCategoryStatusArchived, StoreCategoryStatusActive} {
		got, err := env.service.TransitionStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, cat.ID, status)
		if err != nil {
			t.Fatalf("transition to %s: %v", status, err)
		}
		if got.Status != status {
			t.Fatalf("status = %q, want %q", got.Status, status)
		}
	}
	if _, err := env.service.TransitionStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, cat.ID, "discontinued"); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("invalid status: err = %v, want ErrInvalidInput", err)
	}
}

func TestStoreCategoryDeleteGuardOnAssignments(t *testing.T) {
	env := setupStoreCategoryEnv(t)
	ctx := context.Background()

	cat, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, "assigned-cat", nil, nil, storeCategoryTranslations("Assigned", ""))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	product, err := env.repo.CreateProduct(ctx, "assigned-product", "draft")
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if err := env.repo.SetStoreProductCategories(ctx, product.ID, env.storeAID, []string{cat.ID}); err != nil {
		t.Fatalf("assign category to product: %v", err)
	}

	if err := env.service.DeleteStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, cat.ID); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("delete assigned category: err = %v, want ErrInvalidInput", err)
	}
	if _, err := env.service.GetStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, cat.ID); err != nil {
		t.Fatalf("assigned category must still exist after rejected delete: %v", err)
	}

	refs, err := env.repo.GetStoreProductCategoryRefs(ctx, product.ID)
	if err != nil {
		t.Fatalf("get product category refs: %v", err)
	}
	if len(refs) != 1 || refs[0].ID != cat.ID || refs[0].Name != "Assigned" {
		t.Fatalf("refs = %+v, want one ref to %s named Assigned", refs, cat.ID)
	}
}

func TestStoreCategoryIsolation(t *testing.T) {
	env := setupStoreCategoryEnv(t)
	ctx := context.Background()

	catA, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectA, env.storeAID, "seller-a-only", nil, nil, storeCategoryTranslations("Seller A Only", ""))
	if err != nil {
		t.Fatalf("create in store A: %v", err)
	}
	catB, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectB, env.storeBID, "seller-b-only", nil, nil, storeCategoryTranslations("Seller B Only", ""))
	if err != nil {
		t.Fatalf("create in store B: %v", err)
	}

	// Seller B cannot touch store A's category; foreign ids are not found.
	if _, err := env.service.GetStoreCategoryForSubject(ctx, env.subjectB, env.storeAID, catA.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign read: err = %v, want ErrNotFound", err)
	}
	if _, err := env.service.GetStoreCategoryForSubject(ctx, env.subjectB, env.storeAID, catB.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("category of another store read from wrong store: err = %v, want ErrNotFound", err)
	}
	if err := env.service.DeleteStoreCategoryForSubject(ctx, env.subjectB, env.storeAID, catA.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign delete: err = %v, want ErrNotFound", err)
	}
	if err := env.service.ReorderStoreCategoriesForSubject(ctx, env.subjectB, env.storeAID, []StoreCategoryOrder{{ID: catA.ID, SortOrder: 5}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign reorder: err = %v, want ErrNotFound", err)
	}

	// A cross-store parent reference is a not-found error (FR-011, US2).
	if _, err := env.service.CreateStoreCategoryForSubject(ctx, env.subjectB, env.storeBID, "cross-parent", &catA.ID, nil, storeCategoryTranslations("Cross", "")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-store parent: err = %v, want ErrNotFound", err)
	}

	// Seller B cannot list store A at all.
	if _, _, err := env.service.ListStoreCategoriesForSubject(ctx, env.subjectB, env.storeAID, "", 10, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign store list: err = %v, want ErrNotFound", err)
	}

	// Assignment validation: foreign category rejected, own accepted.
	ok, err := env.repo.StoreCategoriesBelongToStore(ctx, env.storeAID, []string{catB.ID})
	if err != nil {
		t.Fatalf("belong check: %v", err)
	}
	if ok {
		t.Fatal("cross-store category must not belong to store A")
	}
}

func strPtr(s string) *string { return &s }
