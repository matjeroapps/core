package commerce

// Feature 034: per-item batch creation outcomes at the service level. Each
// variant item commits alone (variant + junction rows + SKU) and a failing
// item must not couple or roll back other items.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestBatchVariantItemsHaveMixedOutcomesIndependently(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	redSmall := []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}
	blueSmallAttrs, err := env.repo.CreateAttributeValue(ctx, env.colorID, "blue", "active")
	if err != nil {
		t.Fatalf("CreateAttributeValue blue: %v", err)
	}
	blueSmall := []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: blueSmallAttrs.ID}, {AttributeID: env.sizeID, AttributeValueID: env.smallID}}

	// Item 1: created.
	created, createdNew, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small", "active", "SKU-RED-S", "", redSmall, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("item 1 create: %v", err)
	}
	if !createdNew {
		t.Fatal("first item must report created")
	}

	// Item 2: canonical duplicate returns already_exists with the prior write.
	existing, createdNew, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-small-retry", "active", "SKU-B", "", []VariantAttributeMapping{redSmall[1], redSmall[0]}, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("item 2 already_exists: %v", err)
	}
	if createdNew || existing.ID != created.ID || existing.SKU == nil || existing.SKU.Code != "SKU-RED-S" {
		t.Fatalf("item 2 must be already_exists of %s with SKU-RED-S, got created=%v id=%s sku=%v", created.ID, createdNew, existing.ID, existing.SKU)
	}

	// Item 3: SKU conflict fails this item only, with an actionable message.
	_, _, err = env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "blue-small", "active", "SKU-RED-S", "", blueSmall, nil, nil, nil, nil, nil)
	if !strings.Contains(err.Error(), "SKU-RED-S") || !strings.Contains(err.Error(), "store catalog") {
		t.Fatalf("SKU conflict message not actionable: %v", err)
	}
	var count int
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variants WHERE product_id = $1`, env.product).Scan(&count); err != nil {
		t.Fatalf("count variants: %v", err)
	}
	if count != 1 {
		t.Fatalf("failing item wrote partial state: %d variants", count)
	}

	// Item 4: the combination of item 3 succeeds once its SKU is corrected,
	// while item 1 stays intact and item 2 stays a duplicate-signature lookup.
	fixed, createdNew, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "blue-small", "active", "SKU-BLUE-S", "", blueSmall, nil, nil, nil, nil, nil)
	if err != nil || !createdNew || fixed.SKU == nil || fixed.SKU.Code != "SKU-BLUE-S" {
		t.Fatalf("item 4 create err=%v created=%v detail=%v", err, createdNew, fixed)
	}
	if err := env.db.Pool.QueryRow(ctx, `SELECT count(*) FROM variants WHERE product_id = $1`, env.product).Scan(&count); err != nil {
		t.Fatalf("count variants after item 4: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 variants after batch (item1 + item4), found %d", count)
	}
}

func TestBatchVariantItemVariantCodeConflictIsActionable(t *testing.T) {
	env := newProductVariantMgmtEnv(t)
	ctx := context.Background()
	first := []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: env.redID}}
	if _, _, err := env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-only", "active", "SKU-RED-A", "", first, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("first create: %v", err)
	}
	blue, err := env.repo.CreateAttributeValue(ctx, env.colorID, uuid.NewString()[:8], "active")
	if err != nil {
		t.Fatalf("CreateAttributeValue blue: %v", err)
	}
	_, _, err = env.service.CreateVariantWithAttributesAndSKU(ctx, env.subject, env.storeID, env.product, "red-only", "active", "SKU-BLUE-B", "", []VariantAttributeMapping{{AttributeID: env.colorID, AttributeValueID: blue.ID}}, nil, nil, nil, nil, nil)
	if err == nil {
		t.Fatal("duplicate variant code must fail the item")
	}
	if !strings.Contains(err.Error(), "red-only") || !strings.Contains(err.Error(), "product") {
		t.Fatalf("variant-code conflict message must name the conflicting code: %v", err)
	}
}
