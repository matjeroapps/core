package suppliers_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/matjeroapps/core/internal/suppliers"
	"github.com/matjeroapps/core/internal/testdb"
	"github.com/matjeroapps/core/packages/database"
)

func setupTestDB(t *testing.T) *database.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)

	migrations := []string{
		"000001_event_delivery_foundation.up.sql",
		"000002_market_reference_data.up.sql",
		"000003_commerce_domain_schema.up.sql",
		"000004_admin_supplier_seller_platforms.up.sql",
		"000005_store_domain_lifecycle.up.sql",
		"000006_store_domain_integrity.up.sql",
		"000007_theme_engine_schema.up.sql",
		"000008_storefront_revisions.up.sql",
		"000009_supplier_retail_capability.up.sql",
		"000010_customer_cart_domain.up.sql",
		"000011_checkout_sessions.up.sql",
		"000018_supplier_retail_affiliation.up.sql",
	}

	for _, m := range migrations {
		p := filepath.Join("..", "..", "migrations", m)
		body, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read migration %s: %v", p, err)
		}
		if _, err := db.Exec(context.Background(), string(body)); err != nil {
			t.Fatalf("apply migration %s: %v", p, err)
		}
	}

	return db
}

func createSupplierWithMember(t *testing.T, db *database.Pool, code, name, subject, role, status string) string {
	t.Helper()
	ctx := context.Background()
	supplierID := uuid.NewString()

	_, err := db.Exec(ctx, `
		INSERT INTO suppliers (id, code, name, status)
		VALUES ($1, $2, $3, 'active')
	`, supplierID, code, name)
	if err != nil {
		t.Fatalf("create test supplier: %v", err)
	}

	memberID := uuid.NewString()
	_, err = db.Exec(ctx, `
		INSERT INTO supplier_members (id, supplier_id, principal_subject, role, status)
		VALUES ($1, $2, $3, $4, $5)
	`, memberID, supplierID, subject, role, status)
	if err != nil {
		t.Fatalf("create test supplier member: %v", err)
	}

	return supplierID
}

func TestProvisionRetailCapability_Success(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	ownerSubject := "sub-owner-" + suffix
	supplierID := createSupplierWithMember(t, db, "sup-code-"+suffix, "Supplier Owner Co", ownerSubject, "owner", "active")

	// Also add a second manager member to verify they are NOT copied to seller_members (ADR-019 rule)
	managerSubject := "sub-mgr-" + suffix
	_, err := db.Exec(ctx, `
		INSERT INTO supplier_members (id, supplier_id, principal_subject, role, status)
		VALUES ($1, $2, $3, 'manager', 'active')
	`, uuid.NewString(), supplierID, managerSubject)
	if err != nil {
		t.Fatalf("create manager member: %v", err)
	}

	params := suppliers.ProvisionRetailParams{
		Code:     "ret-sel-" + suffix,
		Name:     "Retail Store Profile",
		Settings: map[string]any{"default_currency": "USD"},
	}

	seller, aff, err := suppliers.ProvisionRetailCapabilityTx(ctx, db.Pool, supplierID, ownerSubject, params)
	if err != nil {
		t.Fatalf("ProvisionRetailCapabilityTx failed: %v", err)
	}

	if seller.ID == "" || seller.Code != params.Code || seller.Name != params.Name || seller.Status != "active" {
		t.Fatalf("unexpected seller profile: %+v", seller)
	}

	if aff.SupplierID != supplierID || aff.SellerID != seller.ID {
		t.Fatalf("unexpected affiliation: %+v", aff)
	}

	// Verify seller_settings in DB
	var settingsJSON []byte
	err = db.QueryRow(ctx, `SELECT settings FROM seller_settings WHERE seller_id = $1`, seller.ID).Scan(&settingsJSON)
	if err != nil {
		t.Fatalf("query seller_settings: %v", err)
	}

	// Verify seller_members contains ONLY the executing owner (ADR-019 requirement)
	rows, err := db.Query(ctx, `SELECT principal_subject, role, status FROM seller_members WHERE seller_id = $1`, seller.ID)
	if err != nil {
		t.Fatalf("query seller_members: %v", err)
	}
	defer rows.Close()

	var memberSubjects []string
	for rows.Next() {
		var sub, role, st string
		if err := rows.Scan(&sub, &role, &st); err != nil {
			t.Fatalf("scan seller member: %v", err)
		}
		memberSubjects = append(memberSubjects, sub)
		if sub != ownerSubject || role != "owner" || st != "active" {
			t.Fatalf("unexpected seller member record: sub=%s, role=%s, status=%s", sub, role, st)
		}
	}
	if len(memberSubjects) != 1 {
		t.Fatalf("expected exactly 1 seller_member (the executing owner), got %d: %v", len(memberSubjects), memberSubjects)
	}
}

func TestProvisionRetailCapability_SecurityNonOwnerForbidden(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	managerSubject := "sub-mgr-only-" + suffix
	supplierID := createSupplierWithMember(t, db, "sup-mgr-"+suffix, "Manager Supplier", managerSubject, "manager", "active")

	params := suppliers.ProvisionRetailParams{
		Code: "ret-mgr-" + suffix,
		Name: "Forbidden Profile",
	}

	_, _, err := suppliers.ProvisionRetailCapabilityTx(ctx, db.Pool, supplierID, managerSubject, params)
	if err == nil {
		t.Fatalf("expected error when non-owner (manager) attempts to provision retail capability, got nil")
	}
	if err != suppliers.ErrForbidden {
		t.Fatalf("expected ErrForbidden, got %v", err)
	}

	// Verify no seller was created
	var count int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM sellers WHERE code = $1`, params.Code).Scan(&count)
	if count != 0 {
		t.Fatalf("expected 0 sellers created, found %d", count)
	}
}

func TestProvisionRetailCapability_OneToOneConstraint(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	ownerSubject := "sub-1to1-" + suffix
	supplierID := createSupplierWithMember(t, db, "sup-1to1-"+suffix, "1to1 Supplier", ownerSubject, "owner", "active")

	params1 := suppliers.ProvisionRetailParams{
		Code: "ret-1to1-a-" + suffix,
		Name: "First Retail Profile",
	}

	seller1, _, err := suppliers.ProvisionRetailCapabilityTx(ctx, db.Pool, supplierID, ownerSubject, params1)
	if err != nil {
		t.Fatalf("first provision failed: %v", err)
	}
	if seller1.ID == "" {
		t.Fatalf("expected valid seller ID")
	}

	// Attempt second provision for same supplier -> must fail (1:1 constraint)
	params2 := suppliers.ProvisionRetailParams{
		Code: "ret-1to1-b-" + suffix,
		Name: "Second Retail Profile Attempt",
	}

	_, _, err = suppliers.ProvisionRetailCapabilityTx(ctx, db.Pool, supplierID, ownerSubject, params2)
	if err == nil {
		t.Fatalf("expected error on second retail capability provision for same supplier, got nil")
	}
	if err != suppliers.ErrAlreadyExists {
		t.Fatalf("expected ErrAlreadyExists, got %v", err)
	}

	// Verify no second seller was created
	var count int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM supplier_seller_affiliations WHERE supplier_id = $1`, supplierID).Scan(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 affiliation for supplier, got %d", count)
	}
}

func TestProvisionRetailCapability_AtomicRollbackOnFailure(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// Create pre-existing seller with specific code
	existingCode := "existing-code-" + suffix
	_, err := db.Exec(ctx, `
		INSERT INTO sellers (id, code, name, status)
		VALUES ($1, $2, $3, 'active')
	`, uuid.NewString(), existingCode, "Pre-existing Seller")
	if err != nil {
		t.Fatalf("create pre-existing seller: %v", err)
	}

	ownerSubject := "sub-rollback-" + suffix
	supplierID := createSupplierWithMember(t, db, "sup-rollback-"+suffix, "Rollback Supplier", ownerSubject, "owner", "active")

	var sellersCountBefore int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM sellers`).Scan(&sellersCountBefore)

	// Attempt provisioning with duplicate seller code
	params := suppliers.ProvisionRetailParams{
		Code: existingCode,
		Name: "Duplicate Code Attempt",
	}

	_, _, err = suppliers.ProvisionRetailCapabilityTx(ctx, db.Pool, supplierID, ownerSubject, params)
	if err == nil {
		t.Fatalf("expected error due to duplicate seller code, got nil")
	}

	var sellersCountAfter int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM sellers`).Scan(&sellersCountAfter)

	if sellersCountAfter != sellersCountBefore {
		t.Fatalf("atomic transaction failed: sellers count changed from %d to %d (orphaned record created)", sellersCountBefore, sellersCountAfter)
	}

	var affCount int
	_ = db.QueryRow(ctx, `SELECT COUNT(*) FROM supplier_seller_affiliations WHERE supplier_id = $1`, supplierID).Scan(&affCount)
	if affCount != 0 {
		t.Fatalf("expected 0 affiliations created on failed transaction, got %d", affCount)
	}
}

func TestGetSupplierSellerAffiliation(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	ownerSubject := "sub-getaff-" + suffix
	supplierID := createSupplierWithMember(t, db, "sup-getaff-"+suffix, "GetAff Supplier", ownerSubject, "owner", "active")

	// 1. Get before provision -> ErrNotFound
	_, err := suppliers.GetSupplierSellerAffiliation(ctx, db.Pool, supplierID)
	if err != suppliers.ErrNotFound {
		t.Fatalf("expected ErrNotFound before provision, got %v", err)
	}

	// 2. Provision
	params := suppliers.ProvisionRetailParams{
		Code: "ret-getaff-" + suffix,
		Name: "GetAff Retail Profile",
	}
	seller, _, err := suppliers.ProvisionRetailCapabilityTx(ctx, db.Pool, supplierID, ownerSubject, params)
	if err != nil {
		t.Fatalf("provision failed: %v", err)
	}

	// 3. Get after provision -> success
	aff, err := suppliers.GetSupplierSellerAffiliation(ctx, db.Pool, supplierID)
	if err != nil {
		t.Fatalf("GetSupplierSellerAffiliation failed: %v", err)
	}
	if aff.SupplierID != supplierID || aff.SellerID != seller.ID {
		t.Fatalf("unexpected affiliation retrieved: %+v", aff)
	}
}
