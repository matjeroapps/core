package catalog_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"core/internal/catalog"
	"core/internal/testdb"
	"core/packages/database"
)

func setupCatalogDB(t *testing.T) (*database.Pool, *catalog.VariantRepository, context.Context) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	ctx := context.Background()

	migrations := []string{
		"000001_event_delivery_foundation",
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000041_catalog_variant_options_and_sku_specs",
	}

	for _, name := range migrations {
		path := filepath.Join("..", "..", "migrations", name+".up.sql")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read migration %s: %v", name, err)
		}
		if _, err := db.Pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply migration %s: %v", name, err)
		}
	}

	repo := catalog.NewVariantRepository(db.Pool)
	return db, repo, ctx
}

func TestVariantOptionsAndSKUSpecs(t *testing.T) {
	db, repo, ctx := setupCatalogDB(t)

	productID := uuid.NewString()
	variantID := uuid.NewString()
	skuID := uuid.NewString()
	attrColorID := uuid.NewString()
	valBlueID := uuid.NewString()
	valRedID := uuid.NewString()

	// Seed product, variant, and sku
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO products (id, slug, status)
		VALUES ($1, 'tshirt', 'active')
	`, productID)
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO variants (id, product_id, code, status)
		VALUES ($1, $2, 'VAR-BLUE', 'active')
	`, variantID, productID)
	if err != nil {
		t.Fatalf("seed variant: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO skus (id, variant_id, code, barcode, status)
		VALUES ($1, $2, 'SKU-BLUE-L', '123456789012', 'active')
	`, skuID, variantID)
	if err != nil {
		t.Fatalf("seed sku: %v", err)
	}

	// Seed attribute "Color"
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO attributes (id, code, status)
		VALUES ($1, 'color', 'active')
	`, attrColorID)
	if err != nil {
		t.Fatalf("seed attribute: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO attribute_translations (attribute_id, locale, name)
		VALUES ($1, 'en', 'Color')
	`, attrColorID)
	if err != nil {
		t.Fatalf("seed attribute translation: %v", err)
	}

	// Seed attribute value "Blue"
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO attribute_values (id, attribute_id, code, status)
		VALUES ($1, $2, 'blue', 'active')
	`, valBlueID, attrColorID)
	if err != nil {
		t.Fatalf("seed attribute value blue: %v", err)
	}

	_, err = db.Pool.Exec(ctx, `
		INSERT INTO attribute_value_translations (attribute_value_id, locale, name)
		VALUES ($1, 'en', 'Navy Blue')
	`, valBlueID)
	if err != nil {
		t.Fatalf("seed attribute value translation blue: %v", err)
	}

	// Seed attribute value "Red"
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO attribute_values (id, attribute_id, code, status)
		VALUES ($1, $2, 'red', 'active')
	`, valRedID, attrColorID)
	if err != nil {
		t.Fatalf("seed attribute value red: %v", err)
	}

	// Step 1: Attach Color = Blue to variant
	err = repo.AttachVariantAttributeValue(ctx, variantID, attrColorID, valBlueID)
	if err != nil {
		t.Fatalf("attach attribute value failed: %v", err)
	}

	// Step 2: List variant attribute values and verify
	values, err := repo.ListVariantAttributeValues(ctx, variantID)
	if err != nil {
		t.Fatalf("list variant attribute values failed: %v", err)
	}
	if len(values) != 1 {
		t.Fatalf("expected 1 attribute value, got %d", len(values))
	}
	if values[0].AttributeName != "Color" || values[0].AttributeValueName != "Navy Blue" {
		t.Errorf("unexpected names: attr=%s, val=%s", values[0].AttributeName, values[0].AttributeValueName)
	}

	// Step 3: Update on conflict: re-attaching Color = Red for the same variant updates the value
	err = repo.AttachVariantAttributeValue(ctx, variantID, attrColorID, valRedID)
	if err != nil {
		t.Fatalf("re-attach attribute value failed: %v", err)
	}

	values, err = repo.ListVariantAttributeValues(ctx, variantID)
	if err != nil {
		t.Fatalf("list after update failed: %v", err)
	}
	if len(values) != 1 || values[0].AttributeValueID != valRedID {
		t.Fatalf("expected 1 updated value with valRedID, got %v", values)
	}

	// Step 4: Update SKU physical specifications
	newBarcode := "987654321098"
	weight := 250
	length := 300
	width := 200
	height := 50
	price := int64(14900)

	err = repo.UpdateSKUPhysicalSpecs(ctx, skuID, &newBarcode, &weight, &length, &width, &height, &price)
	if err != nil {
		t.Fatalf("update sku physical specs failed: %v", err)
	}

	// Step 5: Read SKU back and verify physical specs
	sku, err := repo.GetSKU(ctx, skuID)
	if err != nil {
		t.Fatalf("get sku failed: %v", err)
	}
	if sku.Barcode != newBarcode {
		t.Errorf("expected barcode %s, got %s", newBarcode, sku.Barcode)
	}
	if sku.WeightGrams == nil || *sku.WeightGrams != weight {
		t.Errorf("expected weight %d, got %v", weight, sku.WeightGrams)
	}
	if sku.LengthMM == nil || *sku.LengthMM != length {
		t.Errorf("expected length %d, got %v", length, sku.LengthMM)
	}
	if sku.WidthMM == nil || *sku.WidthMM != width {
		t.Errorf("expected width %d, got %v", width, sku.WidthMM)
	}
	if sku.HeightMM == nil || *sku.HeightMM != height {
		t.Errorf("expected height %d, got %v", height, sku.HeightMM)
	}
	if sku.PriceMinorUnits == nil || *sku.PriceMinorUnits != price {
		t.Errorf("expected price %d, got %v", price, sku.PriceMinorUnits)
	}
}
