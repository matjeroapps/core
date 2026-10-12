package commerce

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"core/internal/testdb"
	"core/packages/database"
)

type productVariantMgmtEnv struct {
	db      *database.Pool
	repo    Repository
	service Service
	subject string
	storeID string
	product string
	colorID string
	redID   string
	sizeID  string
	smallID string
}

func newProductVariantMgmtEnv(t *testing.T) productVariantMgmtEnv {
	t.Helper()
	db, svc, repo, suffix := setupSellerCatalogTestDB(t)
	for _, migration := range []string{
		"000016_catalog_invariants.up.sql",
		"000041_catalog_variant_options_and_sku_specs.up.sql",
		"000045_product_variant_management.up.sql",
	} {
		applySQLFileForVariantMgmt(t, db.Pool, filepath.Join("..", "..", "migrations", migration))
	}

	subject := "subject-" + suffix
	seller, err := repo.CreateSeller(context.Background(), "seller-"+suffix, "Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	if _, err := repo.CreateSellerMember(context.Background(), seller.ID, subject, "manager", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	store, err := repo.CreateStore(context.Background(), seller.ID, "EG", "store-"+uuid.NewString(), "Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}
	detail, err := svc.CreateSellerProductForSubject(context.Background(), subject, store.ID, SellerProductDraft{Slug: "product-" + uuid.NewString(), Translations: []ProductTranslation{{Locale: "en", Name: "Product"}}})
	if err != nil {
		t.Fatalf("CreateSellerProductForSubject: %v", err)
	}
	color, err := repo.CreateAttribute(context.Background(), "color-"+uuid.NewString(), "active")
	if err != nil {
		t.Fatalf("CreateAttribute color: %v", err)
	}
	red, err := repo.CreateAttributeValue(context.Background(), color.ID, "red", "active")
	if err != nil {
		t.Fatalf("CreateAttributeValue red: %v", err)
	}
	size, err := repo.CreateAttribute(context.Background(), "size-"+uuid.NewString(), "active")
	if err != nil {
		t.Fatalf("CreateAttribute size: %v", err)
	}
	small, err := repo.CreateAttributeValue(context.Background(), size.ID, "small", "active")
	if err != nil {
		t.Fatalf("CreateAttributeValue small: %v", err)
	}
	return productVariantMgmtEnv{db: db, repo: repo, service: svc, subject: subject, storeID: store.ID, product: detail.Product.ID, colorID: color.ID, redID: red.ID, sizeID: size.ID, smallID: small.ID}
}

func TestProductVariantAtomicCreateRollsBackOnSKUConflict(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	_, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small", "active", "SKU-RED-S", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("first CreateVariantWithAttributesAndSKU: %v", err)
	}

	blue, err := env.repo.CreateAttributeValue(ctx, env.colorID, "blue", "active")
	if err != nil {
		t.Fatalf("CreateAttributeValue blue: %v", err)
	}
	_, _, err = env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "blue-small", "active", "SKU-RED-S", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: blue.ID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}, nil, nil, nil, nil, nil)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate store SKU err = %v, want ErrConflict", err)
	}

	var count int
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variants WHERE product_id = $1 AND code = 'blue-small'`, env.product).Scan(&count); err != nil {
		t.Fatalf("count variants: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected failed SKU write to roll back variant, found %d", count)
	}
}

func TestProductVariantStoreScopedSKUAllowsCrossStoreReuse(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	_, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small", "active", "SKU-SHARED", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("store A create: %v", err)
	}

	otherSubject := "subject-" + uuid.NewString()
	otherSeller, err := env.repo.CreateSeller(ctx, "seller-"+uuid.NewString(), "Other Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller other: %v", err)
	}
	if _, err := env.repo.CreateSellerMember(ctx, otherSeller.ID, otherSubject, "manager", "active"); err != nil {
		t.Fatalf("CreateSellerMember other: %v", err)
	}
	otherStore, err := env.repo.CreateStore(ctx, otherSeller.ID, "EG", "store-"+uuid.NewString(), "Other Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore other: %v", err)
	}
	otherDetail, err := env.service.CreateSellerProductForSubject(ctx, otherSubject, otherStore.ID, SellerProductDraft{Slug: "product-" + uuid.NewString(), Translations: []ProductTranslation{{Locale: "en", Name: "Other"}}})
	if err != nil {
		t.Fatalf("CreateSellerProductForSubject other: %v", err)
	}
	_, _, err = env.service.CreateVariantWithAttributesAndSKU(ctx, otherSubject, otherStore.ID, otherDetail.Product.ID, "red-small", "active", "SKU-SHARED", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("same SKU code in different store should be allowed: %v", err)
	}
}

func TestMigration000045UpAndDown(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	base := []string{
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
		"000012_order_aggregate_schema",
		"000013_outbox_publish_claims",
		"000014_seller_catalog_authoring",
		"000015_media_upload_intent",
		"000016_catalog_invariants",
		"000041_catalog_variant_options_and_sku_specs",
	}
	paths := make([]string, 0, len(base))
	for _, migration := range base {
		paths = append(paths, filepath.Join("..", "..", "migrations", migration+".up.sql"))
	}
	testdb.ApplyMigrations(t, db, paths...)
	applySQLFileForVariantMgmt(t, db.Pool, filepath.Join("..", "..", "migrations", "000045_product_variant_management.up.sql"))
	var hasStoreID bool
	if err := db.Pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'skus' AND column_name = 'store_id')`).Scan(&hasStoreID); err != nil {
		t.Fatalf("check store_id column: %v", err)
	}
	if !hasStoreID {
		t.Fatal("expected skus.store_id after migration up")
	}
	applySQLFileForVariantMgmt(t, db.Pool, filepath.Join("..", "..", "migrations", "000045_product_variant_management.down.sql"))
	if err := db.Pool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'skus' AND column_name = 'store_id')`).Scan(&hasStoreID); err != nil {
		t.Fatalf("check store_id column after down: %v", err)
	}
	if hasStoreID {
		t.Fatal("expected skus.store_id removed after migration down")
	}
}

func applySQLFileForVariantMgmt(t *testing.T, pool *pgxpool.Pool, path string) {
	t.Helper()
	if err := applySQLFileForVariantMgmtErr(t, pool, path); err != nil {
		t.Fatalf("apply %s: %v", path, err)
	}
}

func applySQLFileForVariantMgmtErr(t *testing.T, pool *pgxpool.Pool, path string) error {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	_, err = pool.Exec(context.Background(), string(content))
	return err
}

func TestProductVariantDuplicateCanonicalCombinationReturnsAlreadyExists(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	attrs := []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}
	existing, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small", "active", "SKU-A", "", attrs, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	// A retry of the same canonical combination with a different proposed code
	// finds the prior successful write and returns it unchanged instead of
	// creating a second variant for the same signature.
	existingDetail, created, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small-copy", "active", "SKU-B", "", []VariantAttributeMapping{attrs[1], attrs[0]}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("duplicate combination err = %v, want already_exists outcome", err)
	}
	if created {
		t.Fatal("duplicate canonical combination must not create a second variant (already_exists expected)")
	}
	if existingDetail.ID != existing.ID || existingDetail.Code != "red-small" {
		t.Fatalf("already_exists returned %q (%s), want existing variant red-small (%s)", existingDetail.Code, existingDetail.ID, existing.ID)
	}
	var count int
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variants WHERE product_id = $1`, env.product).Scan(&count); err != nil {
		t.Fatalf("count variants: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one variant for the canonical signature, found %d", count)
	}
}

func TestProductVariantRejectsInvalidVariantStatus(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	_, _, err := env.service.CreateVariantWithAttributesAndSKU(context.Background(), env.subject, env.storeID, env.product, "red-small", "draft", "SKU-DRAFT", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}}, nil, nil, nil, nil, nil)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("draft variant status err = %v, want ErrInvalidInput", err)
	}
}

func seedVariantInventoryReference(t *testing.T, env productVariantMgmtEnv, skuID string) {
	t.Helper()
	ctx := context.Background()
	locID := uuid.NewString()
	if _, err := env.db.Pool.Exec(ctx, `
		INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status)
		VALUES ($1, $2, 'EG', $3, 'Loc', 'warehouse', 'active')
	`, locID, env.storeID, "loc-"+uuid.NewString()); err != nil {
		t.Fatalf("seed fulfillment location: %v", err)
	}
	if _, err := env.db.Pool.Exec(ctx, `
		INSERT INTO inventory_snapshots (id, sku_id, fulfillment_location_id, on_hand_qty, reserved_qty, version)
		VALUES ($1, $2, $3, 10, 0, 1)
	`, uuid.NewString(), skuID, locID); err != nil {
		t.Fatalf("seed inventory snapshot: %v", err)
	}
}

func variantJunctionValues(t *testing.T, env productVariantMgmtEnv, variantID string) map[string]string {
	t.Helper()
	ctx := context.Background()
	rows, err := env.db.Pool.Query(ctx, `SELECT attribute_id, attribute_value_id FROM variant_attribute_values WHERE variant_id = $1 ORDER BY attribute_id`, variantID)
	if err != nil {
		t.Fatalf("query junction rows: %v", err)
	}
	defer rows.Close()
	values := map[string]string{}
	for rows.Next() {
		var attributeID, valueID string
		if err := rows.Scan(&attributeID, &valueID); err != nil {
			t.Fatalf("scan junction row: %v", err)
		}
		values[attributeID] = valueID
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate junction rows: %v", err)
	}
	return values
}

func TestUpdateUnreferencedVariantReplacesCombinationAtomically(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	attrs := []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}
	created, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small", "active", "SKU-RED-S", "", attrs, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	blue, err := env.repo.CreateAttributeValue(ctx, env.colorID, "blue", "active")
	if err != nil {
		t.Fatalf("CreateAttributeValue blue: %v", err)
	}

	res, err := env.service.UpdateVariantWithAttributesForSubject(ctx, env.subject, env.storeID, env.product, created.ID, "blue-small", "inactive", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: blue.ID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}})
	if err != nil {
		t.Fatalf("update unreferenced variant: %v", err)
	}
	if res.Code != "blue-small" || res.Status != "inactive" {
		t.Fatalf("updated variant = %q/%q, want blue-small/inactive", res.Code, res.Status)
	}
	values := variantJunctionValues(t, env, created.ID)
	if values[env.colorID] != blue.ID || values[env.sizeID] != env.smallID || len(values) != 2 {
		t.Fatalf("junction rows after replacement = %v, want blue + small", values)
	}
	var skuCode string
	if err := env.db.Pool.QueryRow(ctx, `SELECT code FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuCode); err != nil {
		t.Fatalf("query sku after replacement: %v", err)
	}
	if skuCode != "SKU-RED-S" {
		t.Fatalf("sku code changed during attribute replacement: %q", skuCode)
	}
}

func TestUpdateUnreferencedVariantCombinationConflictDoesNotOverwrite(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	attrs := []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}
	created, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small", "active", "SKU-RED-S", "", attrs, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create variant A: %v", err)
	}
	blue, err := env.repo.CreateAttributeValue(ctx, env.colorID, "blue", "active")
	if err != nil {
		t.Fatalf("CreateAttributeValue blue: %v", err)
	}
	if _, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "blue-small", "active", "SKU-BLUE-S", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: blue.ID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("create variant B: %v", err)
	}

	if _, err := env.service.UpdateVariantWithAttributesForSubject(ctx, env.subject, env.storeID, env.product, created.ID, "red-small", "active", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: blue.ID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate combination err = %v, want ErrConflict", err)
	}
	if values := variantJunctionValues(t, env, created.ID); values[env.colorID] != env.redID || values[env.sizeID] != env.smallID {
		t.Fatalf("atomically failing update overwrote junction rows: %v", values)
	}
	var code, status string
	if err := env.db.Pool.QueryRow(ctx, `SELECT code, status FROM variants WHERE id = $1`, created.ID).Scan(&code, &status); err != nil {
		t.Fatalf("query variant after conflict: %v", err)
	}
	if code != "red-small" || status != "active" {
		t.Fatalf("variant overwritten after conflict: %q/%q", code, status)
	}
}

func TestUpdateReferencedVariantCombinationConflictIsActionable(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	created, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small", "active", "SKU-RED-S", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	var skuID string
	if err := env.db.Pool.QueryRow(ctx, `SELECT id FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuID); err != nil {
		t.Fatalf("query sku: %v", err)
	}
	seedVariantInventoryReference(t, env, skuID)
	blue, err := env.repo.CreateAttributeValue(ctx, env.colorID, "blue", "active")
	if err != nil {
		t.Fatalf("CreateAttributeValue blue: %v", err)
	}

	_, err = env.service.UpdateVariantWithAttributesForSubject(ctx, env.subject, env.storeID, env.product, created.ID, "blue-small", "active", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: blue.ID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("referenced combination change err = %v, want ErrConflict", err)
	}
	if err != nil && !strings.Contains(err.Error(), "inventory or order references") {
		t.Fatalf("conflict should cite inventory/order references, got: %v", err)
	}
	if values := variantJunctionValues(t, env, created.ID); values[env.colorID] != env.redID || values[env.sizeID] != env.smallID {
		t.Fatalf("referenced combination change altered junction rows: %v", values)
	}

	// Referenced variants may still edit code/status without a new signature.
	res, err := env.service.UpdateVariantWithAttributesForSubject(ctx, env.subject, env.storeID, env.product, created.ID, "red-small-live", "inactive", nil)
	if err != nil {
		t.Fatalf("referenced code/status edit: %v", err)
	}
	if res.Code != "red-small-live" || res.Status != "inactive" {
		t.Fatalf("referenced code/status edit result = %q/%q", res.Code, res.Status)
	}
}

func TestDeleteReferencedVariantIsProtectedAndUnreferencedDeleteWorks(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	created, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small", "active", "SKU-RED-S", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	var skuID string
	if err := env.db.Pool.QueryRow(ctx, `SELECT id FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuID); err != nil {
		t.Fatalf("query sku: %v", err)
	}
	seedVariantInventoryReference(t, env, skuID)

	if err := env.service.DeleteVariantForSubject(ctx, env.subject, env.storeID, env.product, created.ID); !errors.Is(err, ErrResourceInUse) {
		t.Fatalf("delete referenced variant err = %v, want ErrResourceInUse", err)
	}
	var variantCount, skuCount, junctionCount, snapshotCount int
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variants WHERE id = $1`, created.ID).Scan(&variantCount); err != nil {
		t.Fatalf("query variant count: %v", err)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuCount); err != nil {
		t.Fatalf("query sku count: %v", err)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variant_attribute_values WHERE variant_id = $1`, created.ID).Scan(&junctionCount); err != nil {
		t.Fatalf("query junction count: %v", err)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM inventory_snapshots WHERE sku_id = $1`, skuID).Scan(&snapshotCount); err != nil {
		t.Fatalf("query snapshot count: %v", err)
	}
	if variantCount != 1 || skuCount != 1 || junctionCount != 2 || snapshotCount != 1 {
		t.Fatalf("referenced delete left partial removal: variant=%d sku=%d junction=%d snapshot=%d", variantCount, skuCount, junctionCount, snapshotCount)
	}

	// The unreferenced sibling variant deletes fully and atomically.
	spares, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "blue-small", "active", "SKU-BLUE-S", "", []VariantAttributeMapping{{AttributeID: env.sizeID, AttributeValueID: env.smallID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create spare variant: %v", err)
	}
	if err := env.service.DeleteVariantForSubject(ctx, env.subject, env.storeID, env.product, spares.ID); err != nil {
		t.Fatalf("delete unreferenced variant: %v", err)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variants WHERE id = $1`, spares.ID).Scan(&variantCount); err != nil {
		t.Fatalf("query spare variant count: %v", err)
	}
	if variantCount != 0 {
		t.Fatalf("unreferenced delete left variant behind, found %d", variantCount)
	}
}

func TestArchiveVariantIsIdempotentAndPreservesHistoricalRows(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	created, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small", "active", "SKU-RED-S", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	var skuID string
	if err := env.db.Pool.QueryRow(ctx, `SELECT id FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuID); err != nil {
		t.Fatalf("query sku: %v", err)
	}
	seedVariantInventoryReference(t, env, skuID)

	for attempt := 1; attempt <= 2; attempt++ {
		if _, err := env.service.ArchiveVariantForSubject(ctx, env.subject, env.storeID, env.product, created.ID); err != nil {
			t.Fatalf("archive attempt %d: %v", attempt, err)
		}
		var code, status, skuStatus string
		var skuCount, junctionCount, snapshotCount int
		if err := env.db.Pool.QueryRow(ctx, `SELECT code, status FROM variants WHERE id = $1`, created.ID).Scan(&code, &status); err != nil {
			t.Fatalf("query variant after archive %d: %v", attempt, err)
		}
		if code != "red-small" || status != "archived" {
			t.Fatalf("variant after archive %d = %q/%q, want red-small/archived", attempt, code, status)
		}
		if err := env.db.Pool.QueryRow(ctx, `SELECT status FROM skus WHERE id = $1`, skuID).Scan(&skuStatus); err != nil {
			t.Fatalf("query sku after archive %d: %v", attempt, err)
		}
		if skuStatus != "inactive" {
			t.Fatalf("active sku after archive %d status = %q, want inactive", attempt, skuStatus)
		}
		if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuCount); err != nil {
			t.Fatalf("count skus after archive %d: %v", attempt, err)
		}
		if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variant_attribute_values WHERE variant_id = $1`, created.ID).Scan(&junctionCount); err != nil {
			t.Fatalf("count junction rows after archive %d: %v", attempt, err)
		}
		if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM inventory_snapshots WHERE sku_id = $1`, skuID).Scan(&snapshotCount); err != nil {
			t.Fatalf("count snapshots after archive %d: %v", attempt, err)
		}
		if skuCount != 1 || junctionCount != 2 || snapshotCount != 1 {
			t.Fatalf("archive dropped historical rows: sku=%d junction=%d snapshot=%d", skuCount, junctionCount, snapshotCount)
		}
	}
}

func TestCreateSKUForSubjectKeepsExactlyOneActiveSKU(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	created, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small", "active", "SKU-RED-S", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}

	// Creating a new active SKU through the service replaces the previously
	// active one in the same transaction: total rows grow, but exactly one
	// stays active and it is the new code.
	replacement, err := env.service.CreateSKUForSubject(ctx, env.subject, env.storeID, env.product, created.ID, "SKU-NEW", "", "active")
	if err != nil {
		t.Fatalf("CreateSKUForSubject: %v", err)
	}
	if replacement.Code != "SKU-NEW" || replacement.Status != "active" {
		t.Fatalf("replacement sku = %+v", replacement)
	}
	var skuCount, activeCount int
	var activeCode string
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuCount); err != nil {
		t.Fatalf("count skus: %v", err)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*), COALESCE(max(code), '') FROM skus WHERE variant_id = $1 AND status = 'active'`, created.ID).Scan(&activeCount, &activeCode); err != nil {
		t.Fatalf("count active skus: %v", err)
	}
	if skuCount != 2 || activeCount != 1 || activeCode != "SKU-NEW" {
		t.Fatalf("one-active invariant broken: total=%d active=%d activeCode=%q", skuCount, activeCount, activeCode)
	}

	// The partial unique index backs the invariant: forcing a second active
	// SKU for the same variant fails at the database level.
	if _, err := env.db.Pool.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status, store_id) VALUES ($1, $2, 'SKU-FORCED', 'active', $3)`, uuid.NewString(), created.ID, env.storeID); err == nil {
		t.Fatal("second active SKU insert must violate the partial unique index")
	}

	// Store-scoped uniqueness still rejects a duplicate code for the same
	// variant through the service, without deactivating the active SKU.
	if _, err := env.service.CreateSKUForSubject(ctx, env.subject, env.storeID, env.product, created.ID, "SKU-RED-S", "", "active"); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate sku code err = %v, want ErrConflict", err)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT code FROM skus WHERE variant_id = $1 AND status = 'active'`, created.ID).Scan(&activeCode); err != nil {
		t.Fatalf("query active sku after conflict: %v", err)
	}
	if activeCode != "SKU-NEW" {
		t.Fatalf("failed duplicate write changed the active SKU: %q", activeCode)
	}
}

func TestProductVariantRejectsMismatchedAttributeValueOwnership(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	// smallID is owned by the size attribute; pairing it with the color
	// attribute must be rejected as a mismatched value-ownership claim.
	_, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "mixed-color", "active", "SKU-MIXED", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.smallID}}, nil, nil, nil, nil, nil)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("mismatched attribute/value pair err = %v, want ErrInvalidInput", err)
	}

	var variantCount, junctionCount int
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variants WHERE product_id = $1 AND code = 'mixed-color'`, env.product).Scan(&variantCount); err != nil {
		t.Fatalf("count variants: %v", err)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variant_attribute_values vav JOIN variants v ON v.id = vav.variant_id WHERE v.product_id = $1 AND v.code = 'mixed-color'`, env.product).Scan(&junctionCount); err != nil {
		t.Fatalf("count junction rows: %v", err)
	}
	if variantCount != 0 || junctionCount != 0 {
		t.Fatalf("mismatched ownership claim persisted state: variant=%d junction=%d", variantCount, junctionCount)
	}
}

func TestProductVariantRejectsInactiveOrStaleAttributeValue(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	grey, err := env.repo.CreateAttributeValue(ctx, env.colorID, "grey", "inactive")
	if err != nil {
		t.Fatalf("CreateAttributeValue grey: %v", err)
	}
	_, _, err = env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "grey-item", "active", "SKU-GREY", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: grey.ID}}, nil, nil, nil, nil, nil)
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("inactive value create err = %v, want ErrInvalidInput", err)
	}
	if err != nil && !strings.Contains(err.Error(), "not active") {
		t.Fatalf("inactive value error should cite availability, got: %v", err)
	}
	var variantCount int
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variants WHERE product_id = $1 AND code = 'grey-item'`, env.product).Scan(&variantCount); err != nil {
		t.Fatalf("count variants: %v", err)
	}
	if variantCount != 0 {
		t.Fatalf("inactive availability rejection persisted a variant, found %d", variantCount)
	}

	// A value that was active when the variant was created becomes stale: an
	// update that still claims it is rejected and must not overwrite anything.
	created, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small", "active", "SKU-RED-S", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("create variant: %v", err)
	}
	if _, err := env.db.Pool.Exec(ctx, `UPDATE attribute_values SET status = 'inactive' WHERE id = $1`, env.redID); err != nil {
		t.Fatalf("deactivate attribute value: %v", err)
	}
	_, err = env.service.UpdateVariantWithAttributesForSubject(ctx, env.subject, env.storeID, env.product, created.ID, "red-small-updated", "inactive", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("stale value update err = %v, want ErrInvalidInput", err)
	}
	var code, status string
	if err := env.db.Pool.QueryRow(ctx, `SELECT code, status FROM variants WHERE id = $1`, created.ID).Scan(&code, &status); err != nil {
		t.Fatalf("query variant after stale update: %v", err)
	}
	if code != "red-small" || status != "active" {
		t.Fatalf("stale availability rejection overwrote the variant: %q/%q", code, status)
	}
	if values := variantJunctionValues(t, env, created.ID); values[env.colorID] != env.redID {
		t.Fatalf("stale availability rejection altered junction rows: %v", values)
	}
}

func TestMigration000045DownGuardsCrossStoreDuplicateSKU(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	insertRawSKUForVariantMgmt(t, env, env.storeID, "SKU-DUP-CODE")

	otherSubject := "subject-" + uuid.NewString()
	otherSeller, err := env.repo.CreateSeller(ctx, "seller-"+uuid.NewString(), "Other Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller other: %v", err)
	}
	if _, err := env.repo.CreateSellerMember(ctx, otherSeller.ID, otherSubject, "manager", "active"); err != nil {
		t.Fatalf("CreateSellerMember other: %v", err)
	}
	otherStore, err := env.repo.CreateStore(ctx, otherSeller.ID, "EG", "store-"+uuid.NewString(), "Other Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore other: %v", err)
	}
	insertRawSKUForVariantMgmt(t, env, otherStore.ID, "SKU-DUP-CODE")

	// The rollback must fail explicitly on the preflight guard: cross-store
	// duplicate codes would violate the legacy global skus.code uniqueness
	// that the down migration restores.
	downPath := filepath.Join("..", "..", "migrations", "000045_product_variant_management.down.sql")
	err = applySQLFileForVariantMgmtErr(t, env.db.Pool, downPath)
	if err == nil {
		t.Fatal("expected rollback preflight to fail for cross-store duplicate SKU codes")
	}
	if !strings.Contains(err.Error(), "preflight failed") || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("rollback failure should cite the preflight guard, got: %v", err)
	}

	// The guard fires before any destructive statement: the store-scoped state
	// is still intact and the legacy constraint is not half-restored.
	var hasStoreID bool
	if err := env.db.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'skus' AND column_name = 'store_id')`).Scan(&hasStoreID); err != nil {
		t.Fatalf("check store_id column after failed rollback: %v", err)
	}
	if !hasStoreID {
		t.Fatal("failed rollback dropped skus.store_id before the preflight guard")
	}
	var skuCount int
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM skus WHERE code = 'SKU-DUP-CODE'`).Scan(&skuCount); err != nil {
		t.Fatalf("count duplicate skus: %v", err)
	}
	if skuCount != 2 {
		t.Fatalf("failed rollback mutated skus, found %d duplicate rows", skuCount)
	}

	// Resolving the duplicates restores a valid rollback invariant and the
	// rollback then completes: the legacy constraint and column state match up.
	if _, err := env.db.Pool.Exec(ctx, `DELETE FROM skus WHERE code = 'SKU-DUP-CODE' AND store_id = $1`, otherStore.ID); err != nil {
		t.Fatalf("delete duplicate sku: %v", err)
	}
	applySQLFileForVariantMgmt(t, env.db.Pool, downPath)
	if err := env.db.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = current_schema() AND table_name = 'skus' AND column_name = 'store_id')`).Scan(&hasStoreID); err != nil {
		t.Fatalf("check store_id column after rollback: %v", err)
	}
	if hasStoreID {
		t.Fatal("expected skus.store_id removed after rollback")
	}
}

func insertRawSKUForVariantMgmt(t *testing.T, env productVariantMgmtEnv, storeID, code string) {
	t.Helper()
	ctx := context.Background()
	productID := uuid.NewString()
	if _, err := env.db.Pool.Exec(ctx, `INSERT INTO products (id, slug, status) VALUES ($1, $2, 'active')`, productID, "product-"+productID); err != nil {
		t.Fatalf("insert raw product: %v", err)
	}
	variantID := uuid.NewString()
	if _, err := env.db.Pool.Exec(ctx, `INSERT INTO variants (id, product_id, code, status) VALUES ($1, $2, $3, 'active')`, variantID, productID, "variant-"+variantID); err != nil {
		t.Fatalf("insert raw variant: %v", err)
	}
	if _, err := env.db.Pool.Exec(ctx, `INSERT INTO skus (id, variant_id, code, status, store_id) VALUES ($1, $2, $3, 'active', $4)`, uuid.NewString(), variantID, code, storeID); err != nil {
		t.Fatalf("insert raw sku: %v", err)
	}
}
