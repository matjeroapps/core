package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/matjeroapps/core/packages/database"
	"github.com/matjeroapps/core/packages/money"
)

func setupP58TestDB(t *testing.T) (*p58Env, string) {
	t.Helper()
	db, service, repo, suffix := setupSellerCatalogTestDB(t)
	// The base harness applies migrations through 000015; the invariants
	// migration adds the concurrency backstops under test here.
	content, err := os.ReadFile("../../migrations/000016_catalog_invariants.up.sql")
	if err != nil {
		t.Fatalf("failed to read migration 000016: %v", err)
	}
	if _, err := db.Pool.Exec(context.Background(), string(content)); err != nil {
		t.Fatalf("failed to execute migration 000016: %v", err)
	}
	return &p58Env{db: db, service: service, repo: repo, suffix: suffix}, suffix
}

type p58Env struct {
	db      *database.Pool
	service Service
	repo    Repository
	suffix  string
}

// buildSellerOwnedCatalog creates seller, store, product, variant, active SKU,
// price and presentation — everything except location/inventory, so each test
// can compose the exact topology it needs.
type catalogIDs struct {
	sellerID  string
	subject   string
	storeID   string
	productID string
	listingID string
	variantID string
	skuID     string
}

func (e *p58Env) buildCatalog(t *testing.T) catalogIDs {
	t.Helper()
	ctx := context.Background()
	suffix := e.suffix + fmt.Sprintf("-%d", time.Now().UnixNano())

	seller, err := e.repo.CreateSeller(ctx, "seller-"+suffix, "Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	subject := "user-" + suffix
	if _, err := e.repo.CreateSellerMember(ctx, seller.ID, subject, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	store, err := e.repo.CreateStore(ctx, seller.ID, "EG", "store-"+suffix, "Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}

	detail, err := e.service.CreateSellerProductForSubject(ctx, subject, store.ID, SellerProductDraft{
		Slug: "prod-" + suffix,
		Translations: []ProductTranslation{
			{Locale: "en", Name: "Product", Description: "desc"},
			{Locale: "ar", Name: "منتج", Description: "وصف"},
		},
	})
	if err != nil {
		t.Fatalf("CreateSellerProductForSubject: %v", err)
	}
	variant, err := e.service.CreateVariantForSubject(ctx, subject, store.ID, detail.Product.ID, "default", "active")
	if err != nil {
		t.Fatalf("CreateVariantForSubject: %v", err)
	}
	sku, err := e.service.CreateSKUForSubject(ctx, subject, store.ID, detail.Product.ID, variant.ID, "SKU-"+suffix, "", "active")
	if err != nil {
		t.Fatalf("CreateSKUForSubject: %v", err)
	}
	if _, err := e.repo.SetSellerListingPrice(ctx, detail.Listing.ID, money.MustNew(12000, "EGP")); err != nil {
		t.Fatalf("SetSellerListingPrice: %v", err)
	}
	if _, err := e.repo.CreateMediaMetadata(ctx, MediaMetadata{
		ProductID: detail.Product.ID,
		MediaType: "image/webp",
		URI:       "https://cdn.matjero.test/" + detail.Product.ID + "/main.webp",
		AltText:   "main",
		IsPrimary: true,
	}); err != nil {
		t.Fatalf("CreateMediaMetadata: %v", err)
	}
	pres := SellerListingPresentation{
		SellerListingID:  detail.Listing.ID,
		PurchaseBehavior: "add_to_cart",
		Sections: []ProductPageSection{{
			ID: "sec-1", Type: "description", Enabled: true, SortOrder: 1,
			Content: map[string]any{
				"en": map[string]any{"heading": "About", "body": "Body text"},
				"ar": map[string]any{"heading": "نبذة", "body": "نص"},
			},
		}},
	}
	if _, err := e.service.UpdateListingPresentationForSubject(ctx, subject, store.ID, detail.Listing.ID, pres); err != nil {
		t.Fatalf("UpdateListingPresentationForSubject: %v", err)
	}

	return catalogIDs{
		sellerID:  seller.ID,
		subject:   subject,
		storeID:   store.ID,
		productID: detail.Product.ID,
		listingID: detail.Listing.ID,
		variantID: variant.ID,
		skuID:     sku.ID,
	}
}

func (e *p58Env) readiness(t *testing.T, ids catalogIDs) PublishReadiness {
	t.Helper()
	detail, err := e.service.GetSellerProductDetailForSubject(context.Background(), ids.subject, ids.storeID, ids.productID)
	if err != nil {
		t.Fatalf("GetSellerProductDetailForSubject: %v", err)
	}
	return detail.PublishReadiness
}

func reasonsContain(reasons []string, needle string) bool {
	for _, r := range reasons {
		if strings.Contains(r, needle) {
			return true
		}
	}
	return false
}

// TestReadinessUsesSellableTopology verifies that publish readiness is only
// satisfied by an inventory snapshot on an active SKU of the product, at an
// active, store-owned, market-matching location.
func TestReadinessUsesSellableTopology(t *testing.T) {
	e, _ := setupP58TestDB(t)
	ctx := context.Background()

	t.Run("no inventory fails", func(t *testing.T) {
		ids := e.buildCatalog(t)
		r := e.readiness(t, ids)
		if r.IsReady || !reasonsContain(r.Reasons, "Sellable inventory") {
			t.Fatalf("expected sellable inventory reason, got %+v", r)
		}
	})

	t.Run("valid store location and active SKU passes", func(t *testing.T) {
		ids := e.buildCatalog(t)
		loc, err := e.service.CreateStoreFulfillmentLocationForSubject(ctx, ids.subject, ids.storeID, "loc-"+e.suffix, "Main", "warehouse", "active")
		if err != nil {
			t.Fatalf("CreateStoreFulfillmentLocationForSubject: %v", err)
		}
		if _, err := e.repo.CreateInventorySnapshot(ctx, loc.ID, ids.skuID, 10); err != nil {
			t.Fatalf("CreateInventorySnapshot: %v", err)
		}
		r := e.readiness(t, ids)
		if !r.IsReady {
			t.Fatalf("expected ready, got reasons: %v", r.Reasons)
		}
	})

	t.Run("inactive location fails", func(t *testing.T) {
		ids := e.buildCatalog(t)
		loc, err := e.service.CreateStoreFulfillmentLocationForSubject(ctx, ids.subject, ids.storeID, "loc-inactive-"+e.suffix, "Broken", "warehouse", "inactive")
		if err != nil {
			t.Fatalf("CreateStoreFulfillmentLocationForSubject: %v", err)
		}
		if _, err := e.repo.CreateInventorySnapshot(ctx, loc.ID, ids.skuID, 10); err != nil {
			t.Fatalf("CreateInventorySnapshot: %v", err)
		}
		r := e.readiness(t, ids)
		if r.IsReady || !reasonsContain(r.Reasons, "Sellable inventory") {
			t.Fatalf("expected inactive location to fail readiness, got %+v", r)
		}
	})

	t.Run("supplier location fails", func(t *testing.T) {
		ids := e.buildCatalog(t)
		supplier, err := e.repo.CreateSupplier(ctx, "supplier-"+e.suffix, "Supplier", "active", nil)
		if err != nil {
			t.Fatalf("CreateSupplier: %v", err)
		}
		market, err := e.repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
		if err != nil {
			t.Fatalf("CreateSupplierMarket: %v", err)
		}
		_, err = e.db.Pool.Exec(ctx, `
			INSERT INTO fulfillment_locations (id, supplier_id, supplier_market_id, market_code, code, name, location_type, status)
			VALUES (gen_random_uuid(), $1, $2, 'EG', $3, 'Supplier Hub', 'warehouse', 'active')
		`, supplier.ID, market.ID, "sup-loc-"+e.suffix)
		if err != nil {
			t.Fatalf("insert supplier location: %v", err)
		}
		var locID string
		if err := e.db.Pool.QueryRow(ctx, `SELECT id FROM fulfillment_locations WHERE code = $1`, "sup-loc-"+e.suffix).Scan(&locID); err != nil {
			t.Fatalf("find supplier location: %v", err)
		}
		if _, err := e.repo.CreateInventorySnapshot(ctx, locID, ids.skuID, 10); err != nil {
			t.Fatalf("CreateInventorySnapshot: %v", err)
		}
		r := e.readiness(t, ids)
		if r.IsReady || !reasonsContain(r.Reasons, "Sellable inventory") {
			t.Fatalf("expected supplier location to fail seller-owned readiness, got %+v", r)
		}
	})

	t.Run("wrong market location fails", func(t *testing.T) {
		ids := e.buildCatalog(t)
		// A location of another store in a different market can never satisfy
		// this store's seller-owned sellability.
		otherStore, err := e.repo.CreateStore(ctx, ids.sellerID, "SA", "store-sa-"+e.suffix, "SA Store", "active", nil)
		if err != nil {
			t.Fatalf("CreateStore SA: %v", err)
		}
		_, err = e.db.Pool.Exec(ctx, `
			INSERT INTO fulfillment_locations (id, store_id, market_code, code, name, location_type, status)
			VALUES (gen_random_uuid(), $1, 'SA', $2, 'Wrong Market', 'warehouse', 'active')
		`, otherStore.ID, "loc-sa-"+e.suffix)
		if err != nil {
			t.Fatalf("insert wrong-market location: %v", err)
		}
		var locID string
		if err := e.db.Pool.QueryRow(ctx, `SELECT id FROM fulfillment_locations WHERE code = $1`, "loc-sa-"+e.suffix).Scan(&locID); err != nil {
			t.Fatalf("find location: %v", err)
		}
		if _, err := e.repo.CreateInventorySnapshot(ctx, locID, ids.skuID, 10); err != nil {
			t.Fatalf("CreateInventorySnapshot: %v", err)
		}
		r := e.readiness(t, ids)
		if r.IsReady || !reasonsContain(r.Reasons, "Sellable inventory") {
			t.Fatalf("expected wrong-market location to fail readiness, got %+v", r)
		}
	})

	t.Run("inventory on inactive SKU fails", func(t *testing.T) {
		ids := e.buildCatalog(t)
		loc, err := e.service.CreateStoreFulfillmentLocationForSubject(ctx, ids.subject, ids.storeID, "loc-x-"+e.suffix, "Main", "warehouse", "active")
		if err != nil {
			t.Fatalf("CreateStoreFulfillmentLocationForSubject: %v", err)
		}
		if _, err := e.repo.CreateInventorySnapshot(ctx, loc.ID, ids.skuID, 10); err != nil {
			t.Fatalf("CreateInventorySnapshot: %v", err)
		}
		// Deactivate the only active SKU: the snapshot no longer sits on an
		// active sellable topology.
		sku, err := e.repo.GetSKUByID(ctx, ids.skuID)
		if err != nil {
			t.Fatalf("GetSKUByID: %v", err)
		}
		if _, err := e.repo.UpdateSKU(ctx, sku.ID, sku.Code, sku.Barcode, "inactive"); err != nil {
			t.Fatalf("UpdateSKU: %v", err)
		}
		r := e.readiness(t, ids)
		if r.IsReady {
			t.Fatalf("expected inactive-SKU inventory to fail readiness, got %+v", r)
		}
		if !reasonsContain(r.Reasons, "Sellable inventory") && !reasonsContain(r.Reasons, "active Variant") {
			t.Fatalf("expected topology reason, got %v", r.Reasons)
		}
	})
}

// TestPublishRaceAtomicReadiness proves the final readiness validation runs
// inside the publish transaction: state invalidated between the pre-check and
// the publish commit prevents publication.
func TestPublishRaceAtomicReadiness(t *testing.T) {
	e, _ := setupP58TestDB(t)
	ctx := context.Background()
	ids := e.buildCatalog(t)

	loc, err := e.service.CreateStoreFulfillmentLocationForSubject(ctx, ids.subject, ids.storeID, "loc-"+e.suffix, "Main", "warehouse", "active")
	if err != nil {
		t.Fatalf("CreateStoreFulfillmentLocationForSubject: %v", err)
	}
	if _, err := e.repo.CreateInventorySnapshot(ctx, loc.ID, ids.skuID, 10); err != nil {
		t.Fatalf("CreateInventorySnapshot: %v", err)
	}

	if !e.readiness(t, ids).IsReady {
		t.Fatalf("catalog should be ready before the race")
	}

	sku, err := e.repo.GetSKUByID(ctx, ids.skuID)
	if err != nil {
		t.Fatalf("GetSKUByID: %v", err)
	}

	publishRaceHook = func() {
		// Concurrent invalidation lands between evaluation and commit.
		if _, err := e.repo.UpdateSKU(ctx, sku.ID, sku.Code, sku.Barcode, "inactive"); err != nil {
			t.Errorf("race hook UpdateSKU: %v", err)
		}
	}
	defer func() { publishRaceHook = nil }()

	if err := e.service.PublishSellerProductForSubject(ctx, ids.subject, ids.storeID, ids.productID); err == nil {
		t.Fatalf("expected publish to fail when readiness was invalidated before commit")
	}

	// Nothing may be published.
	prod, err := e.repo.GetProductByID(ctx, ids.productID)
	if err != nil {
		t.Fatalf("GetProductByID: %v", err)
	}
	if prod.Status != "inactive" {
		t.Fatalf("product must remain inactive, got %s", prod.Status)
	}
	listing, err := e.repo.GetSellerListingByStoreAndProduct(ctx, ids.storeID, ids.productID)
	if err != nil {
		t.Fatalf("GetSellerListingByStoreAndProduct: %v", err)
	}
	if listing.Status != "inactive" {
		t.Fatalf("listing must remain inactive, got %s", listing.Status)
	}

	// Restore the SKU and publish without the invalidation.
	publishRaceHook = nil
	if _, err := e.repo.UpdateSKU(ctx, sku.ID, sku.Code, sku.Barcode, "active"); err != nil {
		t.Fatalf("reactivate SKU: %v", err)
	}
	if err := e.service.PublishSellerProductForSubject(ctx, ids.subject, ids.storeID, ids.productID); err != nil {
		t.Fatalf("PublishSellerProductForSubject: %v", err)
	}
}

// TestSKUActiveInvariantConcurrency hammers SKU creation concurrently and
// requires exactly one active SKU afterwards.
func TestSKUActiveInvariantConcurrency(t *testing.T) {
	e, _ := setupP58TestDB(t)
	ctx := context.Background()
	ids := e.buildCatalog(t)

	const n = 10
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := e.service.CreateSKUForSubject(ctx, ids.subject, ids.storeID, ids.productID, ids.variantID,
				fmt.Sprintf("SKU-RACE-%s-%d", e.suffix, i), "", "active")
			if err != nil {
				errCh <- err
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	// Losing requests surface a conflict error: that is the invariant working.
	// Whatever the split, exactly one active SKU must remain.
	for err := range errCh {
		if err != nil && !strings.Contains(err.Error(), "conflict") {
			t.Logf("concurrent create error: %v", err)
		}
	}

	skus, err := e.repo.ListSKUsByVariantID(ctx, ids.variantID)
	if err != nil {
		t.Fatalf("ListSKUsByVariantID: %v", err)
	}
	active := 0
	for _, sku := range skus {
		if sku.Status == "active" {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("expected exactly one active SKU after %d concurrent creates, got %d", n, active)
	}
}

// TestUploadIntentAuthorizationEnforcement covers the full Complete flow:
// intent scope, expiry, token digest, object verification and idempotency.
func TestUploadIntentAuthorizationEnforcement(t *testing.T) {
	e, _ := setupP58TestDB(t)
	ctx := context.Background()
	ids := e.buildCatalog(t)

	headState := struct {
		contentType   string
		contentLength int64
		fail          bool
	}{contentType: "image/webp", contentLength: 1024, fail: false}

	e.service.S3Storage = &S3Storage{
		cfg: S3Config{URLTTL: 15 * time.Minute},
		MockHeadObject: func(ctx context.Context, storageKey string) (*s3.HeadObjectOutput, error) {
			if headState.fail {
				return nil, fmt.Errorf("boom")
			}
			ct := headState.contentType
			cl := headState.contentLength
			return &s3.HeadObjectOutput{ContentType: &ct, ContentLength: &cl}, nil
		},
		MockPresignPutObject: func(ctx context.Context, storageKey, contentType string) (string, error) {
			return "https://s3.test/bucket/" + storageKey, nil
		},
	}

	presign := func(t *testing.T) MediaUploadResponse {
		t.Helper()
		res, err := e.service.GenerateMediaUploadPresignedURLForSubject(ctx, ids.subject, ids.storeID, ids.productID, MediaUploadRequest{
			ContentType: "image/webp",
			SizeBytes:   1024,
		})
		if err != nil {
			t.Fatalf("presign: %v", err)
		}
		return res
	}

	complete := func(storageKey, token string) error {
		_, err := e.service.CompleteMediaUploadForSubject(ctx, ids.subject, ids.storeID, ids.productID, CompleteMediaUploadRequest{
			StorageKey:  storageKey,
			UploadToken: token,
		})
		return err
	}

	t.Run("success and idempotent retry", func(t *testing.T) {
		res := presign(t)
		m, err := e.service.CompleteMediaUploadForSubject(ctx, ids.subject, ids.storeID, ids.productID, CompleteMediaUploadRequest{
			StorageKey: res.StorageKey, UploadToken: res.UploadToken, AltText: "front", SortOrder: 1, IsPrimary: true,
		})
		if err != nil {
			t.Fatalf("complete: %v", err)
		}
		if m.MediaType != "image/webp" {
			t.Fatalf("media type must come from verified intent metadata, got %s", m.MediaType)
		}
		// Same completed intent + storage key + product → same media record.
		m2, err := e.service.CompleteMediaUploadForSubject(ctx, ids.subject, ids.storeID, ids.productID, CompleteMediaUploadRequest{
			StorageKey: res.StorageKey, UploadToken: res.UploadToken,
		})
		if err != nil {
			t.Fatalf("idempotent complete: %v", err)
		}
		if m2.ID != m.ID {
			t.Fatalf("idempotent completion must return the existing media, got %s vs %s", m2.ID, m.ID)
		}
	})

	t.Run("wrong token fails", func(t *testing.T) {
		res := presign(t)
		if err := complete(res.StorageKey, res.UploadToken+"xx"); err == nil {
			t.Fatalf("expected wrong token to fail")
		}
	})

	t.Run("missing object fails", func(t *testing.T) {
		res := presign(t)
		headState.fail = true
		defer func() { headState.fail = false }()
		if err := complete(res.StorageKey, res.UploadToken); err == nil {
			t.Fatalf("expected missing object to fail")
		}
	})

	t.Run("mime mismatch fails", func(t *testing.T) {
		res := presign(t)
		headState.contentType = "image/png"
		defer func() { headState.contentType = "image/webp" }()
		if err := complete(res.StorageKey, res.UploadToken); err == nil {
			t.Fatalf("expected mime mismatch to fail")
		}
	})

	t.Run("oversized object fails", func(t *testing.T) {
		res := presign(t)
		headState.contentLength = 4096
		defer func() { headState.contentLength = 1024 }()
		if err := complete(res.StorageKey, res.UploadToken); err == nil {
			t.Fatalf("expected oversized object to fail")
		}
	})

	t.Run("zero-byte object fails", func(t *testing.T) {
		res := presign(t)
		headState.contentLength = 0
		defer func() { headState.contentLength = 1024 }()
		if err := complete(res.StorageKey, res.UploadToken); err == nil {
			t.Fatalf("expected zero-byte object to fail")
		}
	})

	t.Run("expired intent fails", func(t *testing.T) {
		rawToken := "expired-case-token"
		digest := sha256.Sum256([]byte(rawToken))
		storageKey := fmt.Sprintf("products/%s/%s/expired-%s.webp", ids.sellerID, ids.productID, e.suffix)
		if _, err := e.repo.CreateMediaUploadIntent(ctx, MediaUploadIntent{
			SellerID:    ids.sellerID,
			StoreID:     ids.storeID,
			ProductID:   ids.productID,
			StorageKey:  storageKey,
			ContentType: "image/webp",
			MaxBytes:    1024,
			TokenDigest: hex.EncodeToString(digest[:]),
			ExpiresAt:   time.Now().Add(-time.Minute),
		}); err != nil {
			t.Fatalf("CreateMediaUploadIntent: %v", err)
		}
		if err := complete(storageKey, rawToken); err == nil {
			t.Fatalf("expected expired intent to fail")
		}
	})

	t.Run("wrong product fails", func(t *testing.T) {
		res := presign(t)
		other, err := e.service.CreateSellerProductForSubject(ctx, ids.subject, ids.storeID, SellerProductDraft{
			Slug:         "other-" + e.suffix,
			Translations: []ProductTranslation{{Locale: "en", Name: "Other"}},
		})
		if err != nil {
			t.Fatalf("create other product: %v", err)
		}
		_, err = e.service.CompleteMediaUploadForSubject(ctx, ids.subject, ids.storeID, other.Product.ID, CompleteMediaUploadRequest{
			StorageKey: res.StorageKey, UploadToken: res.UploadToken,
		})
		if err == nil {
			t.Fatalf("expected wrong product to fail")
		}
	})

	t.Run("wrong store fails", func(t *testing.T) {
		res := presign(t)
		otherStore, err := e.repo.CreateStore(ctx, ids.sellerID, "EG", "store-other-"+e.suffix, "Other", "active", nil)
		if err != nil {
			t.Fatalf("CreateStore: %v", err)
		}
		_, err = e.service.CompleteMediaUploadForSubject(ctx, ids.subject, otherStore.ID, ids.productID, CompleteMediaUploadRequest{
			StorageKey: res.StorageKey, UploadToken: res.UploadToken,
		})
		if err == nil {
			t.Fatalf("expected wrong store to fail")
		}
	})

	t.Run("unknown storage key fails", func(t *testing.T) {
		if err := complete(fmt.Sprintf("products/%s/%s/unknown.webp", ids.sellerID, ids.productID), "whatever"); err == nil {
			t.Fatalf("expected unknown storage key to fail")
		}
	})
}
