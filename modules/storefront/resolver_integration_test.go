package storefront

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/packages/database"
)

func applySQLFileStorefront(t *testing.T, db *database.Pool, path string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := db.Exec(context.Background(), string(b)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
}

func timePtr(t time.Time) *time.Time { return &t }

func TestStoreResolverIntegration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	ctx := context.Background()
	db := testdb.Open(t, dsn)
	for _, m := range []string{
		"000001_event_delivery_foundation",
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000004_admin_supplier_seller_platforms",
		"000005_store_domain_lifecycle",
		"000006_store_domain_integrity",
		"000008_storefront_revisions",
		"000009_supplier_retail_capability",
		"000010_customer_cart_domain",
		"000011_checkout_sessions",
		"000012_order_aggregate_schema",
		"000013_outbox_publish_claims",
		"000014_seller_catalog_authoring",
		"000015_media_upload_intent",
		"000016_catalog_invariants",
	} {
		applySQLFileStorefront(t, db, filepath.Join("..", "..", "migrations", m+".up.sql"))
	}

	repo := commerce.NewRepository(db.Pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}

	// Store A: active store + active primary platform domain.
	storeA, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "a-"+suffix, "Store A", "active", nil, "store-a.matjero.com", "platform", "active", true, timePtr(time.Now()), nil)
	if err != nil {
		t.Fatalf("create store A: %v", err)
	}
	// Store B: active store + active primary platform domain (different tenant).
	storeB, _, err := repo.CreateStoreWithDomain(ctx, seller.ID, "EG", "b-"+suffix, "Store B", "active", nil, "store-b.matjero.com", "platform", "active", true, timePtr(time.Now()), nil)
	if err != nil {
		t.Fatalf("create store B: %v", err)
	}
	// Pending custom domain on store A (must NOT resolve publicly).
	if _, err := repo.CreateCustomStoreDomain(ctx, storeA.ID, "brand.mybrand.com"); err != nil {
		t.Fatalf("create custom domain: %v", err)
	}

	resolver := NewStoreResolver(repo)

	// Active domain resolves.
	res, err := resolver.Resolve(ctx, "store-a.matjero.com")
	if err != nil {
		t.Fatalf("resolve A: %v", err)
	}
	if res.Store.ID != storeA.ID {
		t.Fatalf("resolved store A id mismatch: %q", res.Store.ID)
	}

	// Case-insensitive lookup resolves the same tenant.
	res, err = resolver.Resolve(ctx, "STORE-A.MATJERO.COM")
	if err != nil {
		t.Fatalf("resolve A (upper): %v", err)
	}
	if res.Store.ID != storeA.ID {
		t.Fatalf("case-insensitive resolve mismatch: %q", res.Store.ID)
	}

	// Pending custom domain does NOT resolve (VERIFIED != ACTIVE gate).
	if _, err := resolver.Resolve(ctx, "brand.mybrand.com"); err != ErrDomainInactive {
		t.Fatalf("expected ErrDomainInactive for pending domain, got %v", err)
	}

	// Tenant isolation: A's domain never returns B and vice versa.
	resB, err := resolver.Resolve(ctx, "store-b.matjero.com")
	if err != nil {
		t.Fatalf("resolve B: %v", err)
	}
	if resB.Store.ID == res.Store.ID {
		t.Fatal("tenant isolation broken: A resolved to B")
	}
	if res.Store.ID == storeB.ID {
		t.Fatal("A resolved to B")
	}
}
