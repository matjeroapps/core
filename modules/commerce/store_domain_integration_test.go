package commerce

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/database"
)

func applyStoreDomainMigrations(t *testing.T, db *database.Pool) {
	t.Helper()
	for _, m := range []string{
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000004_admin_supplier_seller_platforms",
		"000005_store_domain_lifecycle",
		"000006_store_domain_integrity",
		"000008_storefront_revisions",
		"000009_supplier_retail_capability",
		"000010_customer_cart_domain",
		"000011_checkout_sessions",
	} {
		applySQLFile(t, db, filepath.Join("..", "..", "migrations", m+".up.sql"))
	}
}

func countStores(t *testing.T, db *database.Pool) int {
	t.Helper()
	var n int
	if err := db.Pool.QueryRow(context.Background(), `SELECT count(*) FROM stores`).Scan(&n); err != nil {
		t.Fatalf("count stores: %v", err)
	}
	return n
}

func TestStoreDomainIntegrity(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	ctx := context.Background()
	db := testdb.Open(t, dsn)
	applyStoreDomainMigrations(t, db)
	repo := NewRepository(db.Pool)
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	seller, err := repo.CreateSeller(ctx, "seller-"+suffix, "Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	store, err := repo.CreateStore(ctx, seller.ID, "EG", "store-"+suffix, "Cairo Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}

	t.Run("canonical lowercase persistence", func(t *testing.T) {
		sd, err := repo.CreateStoreDomain(ctx, store.ID, "  MixedCase.COM  ", "platform", "active", false, nil, nil)
		if err != nil {
			t.Fatalf("CreateStoreDomain: %v", err)
		}
		if sd.Domain != "mixedcase.com" {
			t.Fatalf("domain not canonicalized: %q", sd.Domain)
		}
		if sd.DomainType != "platform" {
			t.Fatalf("domain_type: %q", sd.DomainType)
		}
		if sd.Status != "active" {
			t.Fatalf("status: %q", sd.Status)
		}
		if sd.CreatedAt.IsZero() || sd.UpdatedAt.IsZero() {
			t.Fatal("lifecycle timestamps not populated")
		}
	})

	t.Run("case-insensitive duplicate rejection", func(t *testing.T) {
		if _, err := repo.CreateStoreDomain(ctx, store.ID, "Duplicate.COM", "platform", "active", false, nil, nil); err != nil {
			t.Fatalf("first domain: %v", err)
		}
		_, err := repo.CreateStoreDomain(ctx, store.ID, "duplicate.com", "platform", "active", false, nil, nil)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("expected ErrConflict for case-insensitive duplicate, got %v", err)
		}
	})

	t.Run("one primary domain per store", func(t *testing.T) {
		if _, err := repo.CreateStoreDomain(ctx, store.ID, "primary.example.com", "platform", "active", true, nil, nil); err != nil {
			t.Fatalf("first primary domain: %v", err)
		}
		_, err := repo.CreateStoreDomain(ctx, store.ID, "second-primary.example.com", "platform", "active", true, nil, nil)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("expected ErrConflict for second primary domain, got %v", err)
		}
	})

	t.Run("custom domain lifecycle", func(t *testing.T) {
		cd, err := repo.CreateCustomStoreDomain(ctx, store.ID, "  Shop.MyBrand.com  ")
		if err != nil {
			t.Fatalf("CreateCustomStoreDomain: %v", err)
		}
		if cd.Domain != "shop.mybrand.com" {
			t.Fatalf("custom domain not canonicalized: %q", cd.Domain)
		}
		if cd.DomainType != "custom" {
			t.Fatalf("domain_type: %q", cd.DomainType)
		}
		if cd.Status != "pending" {
			t.Fatalf("custom domain must begin pending, got %q", cd.Status)
		}
		if cd.IsPrimary {
			t.Fatal("custom domain must not be primary")
		}
		if cd.VerificationToken == nil || *cd.VerificationToken == "" {
			t.Fatal("custom domain must receive a verification token")
		}
		if cd.VerifiedAt != nil {
			t.Fatal("custom domain must not be pre-verified")
		}

		// Tokens must be unique across domains.
		cd2, err := repo.CreateCustomStoreDomain(ctx, store.ID, "other.mybrand.com")
		if err != nil {
			t.Fatalf("second custom domain: %v", err)
		}
		if cd.VerificationToken != nil && cd2.VerificationToken != nil && *cd.VerificationToken == *cd2.VerificationToken {
			t.Fatal("verification tokens collided")
		}

		// Pending custom domain is visible to the data layer but must not resolve publicly.
		res, err := repo.GetStoreByDomain(ctx, "shop.mybrand.com")
		if err != nil {
			t.Fatalf("GetStoreByDomain: %v", err)
		}
		if res.StoreDomain.Status != "pending" {
			t.Fatalf("status: %q", res.StoreDomain.Status)
		}
	})

	t.Run("atomic store+domain creation rolls back on conflict", func(t *testing.T) {
		now := time.Now()
		seller2, err := repo.CreateSeller(ctx, "seller2-"+suffix, "Seller 2", "active", nil)
		if err != nil {
			t.Fatalf("CreateSeller 2: %v", err)
		}
		storeA, _, err := repo.CreateStoreWithDomain(ctx, seller2.ID, "EG", "a-"+suffix, "Store A", "active", nil, "a-"+suffix+".matjero.com", "platform", "active", true, &now, nil)
		if err != nil {
			t.Fatalf("seed store A: %v", err)
		}

		before := countStores(t, db)
		_, _, err = repo.CreateStoreWithDomain(ctx, seller2.ID, "EG", "b-"+suffix, "Store B", "active", nil, "a-"+suffix+".matjero.com", "platform", "active", true, &now, nil, 2)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("expected ErrConflict, got %v", err)
		}
		after := countStores(t, db)
		if after != before {
			t.Fatalf("rollback leaked a store: before=%d after=%d", before, after)
		}

		// The conflicting domain still maps only to store A (no orphaned domain/store).
		res, err := repo.GetStoreByDomain(ctx, "a-"+suffix+".matjero.com")
		if err != nil {
			t.Fatalf("GetStoreByDomain after rollback: %v", err)
		}
		if res.Store.ID != storeA.ID {
			t.Fatalf("domain mapping changed after rollback: %q", res.Store.ID)
		}
	})
}

func TestMigrationsReplay(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	ctx := context.Background()
	db := testdb.Open(t, dsn)
	migrations := []string{
		"000001_event_delivery_foundation",
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000004_admin_supplier_seller_platforms",
		"000005_store_domain_lifecycle",
		"000006_store_domain_integrity",
		"000007_theme_engine_schema",
		"000008_storefront_revisions",
	}
	for _, m := range migrations {
		applySQLFile(t, db, filepath.Join("..", "..", "migrations", m+".up.sql"))
	}
	for i := len(migrations) - 1; i >= 0; i-- {
		applySQLFile(t, db, filepath.Join("..", "..", "migrations", migrations[i]+".down.sql"))
	}
	for _, m := range migrations {
		applySQLFile(t, db, filepath.Join("..", "..", "migrations", m+".up.sql"))
	}
	_ = ctx
}
