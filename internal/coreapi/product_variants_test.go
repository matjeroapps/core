package coreapi

// Feature 034: canonical already-exists retry and actionable per-item error
// details over the real internal Seller variant creation route. The route is
// the compatibility boundary the Seller batch coordinator calls once per item;
// mixed outcomes are therefore per-item by construction.

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"core/modules/commerce"
)

type productVariantsEnv struct {
	p58ContractEnv
	productID string
	colorID   string
	redID     string
	blueID    string
	sizeID    string
	smallID   string
}

func newProductVariantsEnv(t *testing.T) productVariantsEnv {
	t.Helper()
	env := productVariantsEnv{p58ContractEnv: setupP58Contract(t)}
	ctx := context.Background()
	for _, migration := range []string{
		"000041_catalog_variant_options_and_sku_specs.up.sql",
		"000045_product_variant_management.up.sql",
	} {
		content, err := os.ReadFile(filepath.Join("..", "..", "migrations", migration))
		if err != nil {
			t.Fatalf("read %s: %v", migration, err)
		}
		// Both migrations are idempotent (IF NOT EXISTS / guarded), so reapplying
		// alongside the P5.8 base fixture is safe.
		if _, err := env.db.Pool.Exec(ctx, string(content)); err != nil {
			t.Fatalf("apply %s: %v", migration, err)
		}
	}

	productRec := env.do(t, "POST", "/internal/v1/stores/"+env.storeID+"/products", StoreProductCreateRequest{
		Slug:         "product-variants-" + uuid.NewString(),
		Translations: []commerce.ProductTranslation{{Locale: "en", Name: "Product Variants"}},
	})
	if productRec.Code != 201 {
		t.Fatalf("create product status = %d, body %s", productRec.Code, productRec.Body.String())
	}
	var productBody struct {
		Product struct {
			ID string `json:"id"`
		} `json:"product"`
	}
	if err := json.Unmarshal(productRec.Body.Bytes(), &productBody); err != nil {
		t.Fatalf("decode product response: %v", err)
	}
	env.productID = productBody.Product.ID

	color, err := env.repo.CreateAttribute(ctx, "color-"+uuid.NewString(), "active")
	if err != nil {
		t.Fatalf("create color attribute: %v", err)
	}
	red, err := env.repo.CreateAttributeValue(ctx, color.ID, "red", "active")
	if err != nil {
		t.Fatalf("create red value: %v", err)
	}
	blue, err := env.repo.CreateAttributeValue(ctx, color.ID, "blue", "active")
	if err != nil {
		t.Fatalf("create blue value: %v", err)
	}
	size, err := env.repo.CreateAttribute(ctx, "size-"+uuid.NewString(), "active")
	if err != nil {
		t.Fatalf("create size attribute: %v", err)
	}
	small, err := env.repo.CreateAttributeValue(ctx, size.ID, "small", "active")
	if err != nil {
		t.Fatalf("create small value: %v", err)
	}
	env.colorID, env.redID, env.blueID, env.sizeID, env.smallID = color.ID, red.ID, blue.ID, size.ID, small.ID
	return env
}

type variantResponseShape struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	SKU  *struct {
		Code string `json:"code"`
	} `json:"sku"`
	AttributeValues []struct {
		AttributeID      string `json:"attribute_id"`
		AttributeValueID string `json:"attribute_value_id"`
	} `json:"attribute_values"`
}

func TestProductVariantsCreateAlreadyExistsAndActionableConflicts(t *testing.T) {
	env := newProductVariantsEnv(t)
	base := "/internal/v1/stores/" + env.storeID + "/products/" + env.productID + "/variants"
	redSmall := []commerce.VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}
	blueSmall := []commerce.VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.blueID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}

	// Item 1: created.
	rec := env.do(t, "POST", base, VariantCreateRequest{
		Code: "red-small", Status: "active", SKUCode: variantPtr("SKU-RED-S"),
		AttributeValues: redSmall,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("first item status = %d body %s", rec.Code, rec.Body.String())
	}
	var created variantResponseShape
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created item: %v", err)
	}
	if created.SKU == nil || created.SKU.Code != "SKU-RED-S" || len(created.AttributeValues) != 2 {
		t.Fatalf("created item must carry sku_code and attribute_values, got %s", rec.Body.String())
	}

	// Item 2: canonical already_exists retry — a different proposed code and a
	// reordered attribute selection for the same canonical combination return
	// the prior successful write with 200 instead of a second variant.
	rec = env.do(t, "POST", base, VariantCreateRequest{
		Code: "red-small-retry", Status: "active", SKUCode: variantPtr("SKU-B"),
		AttributeValues: []commerce.VariantAttributeMapping{redSmall[1], redSmall[0]},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("already_exists item status = %d body %s", rec.Code, rec.Body.String())
	}
	var existing variantResponseShape
	if err := json.Unmarshal(rec.Body.Bytes(), &existing); err != nil {
		t.Fatalf("decode already_exists item: %v", err)
	}
	if existing.ID != created.ID || existing.Code != "red-small" {
		t.Fatalf("already_exists returned %q (%s), want existing red-small (%s)", existing.Code, existing.ID, created.ID)
	}

	// Item 3 (mixed outcome): a duplicate variant code for a new combination
	// fails with a 409 whose message names the conflicting code and the next
	// step, while the earlier successful items remain untouched.
	rec = env.do(t, "POST", base, VariantCreateRequest{
		Code: "red-small", Status: "active", SKUCode: variantPtr("SKU-BLUE-S"),
		AttributeValues: blueSmall,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate variant code status = %d body %s", rec.Code, rec.Body.String())
	}
	var conflictEnvelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &conflictEnvelope); err != nil {
		t.Fatalf("decode conflict envelope: %v", err)
	}
	if conflictEnvelope.Error.Code != "conflict" {
		t.Fatalf("conflict code = %q, want conflict", conflictEnvelope.Error.Code)
	}
	if !strings.Contains(conflictEnvelope.Error.Message, "red-small") || !strings.Contains(conflictEnvelope.Error.Message, "choose a different code") {
		t.Fatalf("variant-code conflict message must be actionable, got %q", conflictEnvelope.Error.Message)
	}

	// Item 4: a duplicate store-catalog SKU for a new combination also fails
	// with an actionable message without discarding item 1.
	rec = env.do(t, "POST", base, VariantCreateRequest{
		Code: "blue-small", Status: "active", SKUCode: variantPtr("SKU-RED-S"),
		AttributeValues: blueSmall,
	})
	if rec.Code != http.StatusConflict {
		t.Fatalf("duplicate SKU status = %d body %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &conflictEnvelope); err != nil {
		t.Fatalf("decode SKU conflict envelope: %v", err)
	}
	if !strings.Contains(conflictEnvelope.Error.Message, "SKU-RED-S") || !strings.Contains(conflictEnvelope.Error.Message, "store catalog") {
		t.Fatalf("SKU-code conflict message must be actionable, got %q", conflictEnvelope.Error.Message)
	}

	var variantCount int
	if err := env.db.Pool.QueryRow(env.ctx, `SELECT count(*) FROM variants WHERE product_id = $1`, env.productID).Scan(&variantCount); err != nil {
		t.Fatalf("count variants: %v", err)
	}
	if variantCount != 1 {
		t.Fatalf("failed items must not write partial variants, found %d", variantCount)
	}
	var skuCode string
	if err := env.db.Pool.QueryRow(env.ctx, `SELECT code FROM skus WHERE variant_id = $1`, created.ID).Scan(&skuCode); err != nil {
		t.Fatalf("query SKU for created item: %v", err)
	}
	if skuCode != "SKU-RED-S" {
		t.Fatalf("SKU of the created item changed: %q", skuCode)
	}
}

func variantPtr[T any](v T) *T { return &v }
