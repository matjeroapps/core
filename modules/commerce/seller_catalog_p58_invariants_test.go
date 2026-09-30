package commerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"core/packages/database"
	"core/packages/money"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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

func TestSupplierImportPreservesSellerPresentationBoundary(t *testing.T) {
	e, _ := setupP58TestDB(t)
	ctx := context.Background()
	suffix := e.suffix

	seller, err := e.repo.CreateSeller(ctx, "boundary-seller-"+suffix, "Boundary Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	subject := "boundary-owner-" + suffix
	if _, err := e.repo.CreateSellerMember(ctx, seller.ID, subject, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	store, err := e.repo.CreateStore(ctx, seller.ID, "EG", "boundary-store-"+suffix, "Boundary Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}

	supplier, err := e.repo.CreateSupplier(ctx, "boundary-supplier-"+suffix, "Boundary Supplier", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplier: %v", err)
	}
	market, err := e.repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplierMarket: %v", err)
	}
	product, err := e.repo.CreateProduct(ctx, "boundary-product-"+suffix, "active")
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	if err := e.repo.UpsertProductTranslation(ctx, ProductTranslation{
		ProductID: product.ID, Locale: "en", Name: "Supplier Title", Description: "Supplier Description",
	}); err != nil {
		t.Fatalf("UpsertProductTranslation: %v", err)
	}
	supplierProduct, err := e.repo.CreateSupplierProduct(ctx, supplier.ID, product.ID, "BOUNDARY-SKU", "active")
	if err != nil {
		t.Fatalf("CreateSupplierProduct: %v", err)
	}
	offer, err := e.repo.CreateSupplierOffer(ctx, supplier.ID, supplierProduct.ID, market.ID, "EG", "active")
	if err != nil {
		t.Fatalf("CreateSupplierOffer: %v", err)
	}
	if _, err := e.repo.SetSupplierOfferAvailability(ctx, offer.ID, true, nil); err != nil {
		t.Fatalf("SetSupplierOfferAvailability: %v", err)
	}

	first, err := e.service.ImportSupplierOfferForSubject(ctx, subject, store.ID, offer.ID)
	if err != nil {
		t.Fatalf("first import: %v", err)
	}
	second, err := e.service.ImportSupplierOfferForSubject(ctx, subject, store.ID, offer.ID)
	if err != nil {
		t.Fatalf("second import: %v", err)
	}
	if first.ID != second.ID || first.SupplierOfferID == nil || *first.SupplierOfferID != offer.ID {
		t.Fatalf("import lineage is not idempotent: first=%+v second=%+v", first, second)
	}
	if _, err := e.repo.GetSellerListingPrice(ctx, first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("import created seller price: %v", err)
	}
	presentation, err := e.repo.GetSellerListingPresentation(ctx, first.ID)
	if err != nil {
		t.Fatalf("read imported presentation: %v", err)
	}
	if presentation.PurchaseBehavior != "inherit" || len(presentation.Sections) != 0 {
		t.Fatalf("import created seller presentation content: %+v", presentation)
	}

	if _, err := e.repo.SetSellerListingPrice(ctx, first.ID, money.MustNew(27500, "EGP")); err != nil {
		t.Fatalf("SetSellerListingPrice: %v", err)
	}
	if _, err := e.service.UpdateListingPresentationForSubject(ctx, subject, store.ID, first.ID, SellerListingPresentation{
		PurchaseBehavior: "buy_now",
		Sections: []ProductPageSection{{
			ID: "boundary-section", Type: "description", Enabled: true, SortOrder: 1,
			Content: map[string]any{"en": map[string]any{"heading": "Retail Heading"}},
		}},
	}); err != nil {
		t.Fatalf("UpdateListingPresentationForSubject: %v", err)
	}
	if err := e.repo.UpsertProductTranslation(ctx, ProductTranslation{
		ProductID: product.ID, Locale: "en", Name: "Supplier Title Updated", Description: "Supplier Description Updated",
	}); err != nil {
		t.Fatalf("update live-linked supplier content: %v", err)
	}

	detail, err := e.service.GetSellerProductDetailForSubject(ctx, subject, store.ID, product.ID)
	if err != nil {
		t.Fatalf("GetSellerProductDetailForSubject: %v", err)
	}
	if detail.Translations[0].Name != "Supplier Title Updated" {
		t.Fatalf("supplier title did not remain live-linked: %+v", detail.Translations)
	}
	if detail.Price == nil || detail.Price.Price.AmountMinor != 27500 {
		t.Fatalf("seller price changed after supplier update: %+v", detail.Price)
	}
	if detail.Presentation == nil || detail.Presentation.PurchaseBehavior != "buy_now" {
		t.Fatalf("seller presentation changed after supplier update: %+v", detail.Presentation)
	}
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
		otherStore, err := e.repo.CreateStore(ctx, ids.sellerID, "SA", "store-sa-"+e.suffix, "SA Store", "active", nil, 2)
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
	if prod.Status != "draft" && prod.Status != "inactive" {
		t.Fatalf("product must remain inactive/draft, got %s", prod.Status)
	}
	listing, err := e.repo.GetSellerListingByStoreAndProduct(ctx, ids.storeID, ids.productID)
	if err != nil {
		t.Fatalf("GetSellerListingByStoreAndProduct: %v", err)
	}
	if listing.Status != "draft" && listing.Status != "inactive" && listing.Status != "unpublished" {
		t.Fatalf("listing must remain inactive/draft/unpublished, got %s", listing.Status)
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
			ProductID:   &ids.productID,
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
		otherStore, err := e.repo.CreateStore(ctx, ids.sellerID, "EG", "store-other-"+e.suffix, "Other", "active", nil, 2)
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

// TestPresentationSaveRejectsInvalidSectionsBeforePersistence proves the
// typed validator is authoritative at save time: an invalid section must
// return a validation error and leave the persisted presentation untouched.
func TestPresentationSaveRejectsInvalidSectionsBeforePersistence(t *testing.T) {
	e, _ := setupP58TestDB(t)
	ctx := context.Background()
	ids := e.buildCatalog(t)

	valid := SellerListingPresentation{
		SellerListingID:  ids.listingID,
		PurchaseBehavior: "add_to_cart",
		Sections: []ProductPageSection{{
			ID: "sec-ok", Type: "description", Enabled: true, SortOrder: 1,
			Content: map[string]any{
				"en": map[string]any{"heading": "About", "body": "Body"},
			},
		}},
	}
	if _, err := e.service.UpdateListingPresentationForSubject(ctx, ids.subject, ids.storeID, ids.listingID, valid); err != nil {
		t.Fatalf("valid presentation save failed: %v", err)
	}

	cases := []struct {
		name    string
		section ProductPageSection
	}{
		{
			name: "invalid image_text: cross-product media",
			section: ProductPageSection{ID: "sec-bad", Type: "image_text", Enabled: true, SortOrder: 2,
				Content: map[string]any{
					"media_id": "media-of-another-product", "layout": "left",
					"en": map[string]any{"heading": "H"},
				}},
		},
		{
			name: "invalid image_text: bad layout",
			section: ProductPageSection{ID: "sec-bad", Type: "image_text", Enabled: true, SortOrder: 2,
				Content: map[string]any{
					"media_id": "whatever", "layout": "diagonal",
					"en": map[string]any{"heading": "H"},
				}},
		},
		{
			name: "invalid final_cta: unknown action",
			section: ProductPageSection{ID: "sec-bad", Type: "final_cta", Enabled: true, SortOrder: 3,
				Content: map[string]any{
					"action": "open_external", "en": map[string]any{"title": "T"},
				}},
		},
		{
			name: "invalid final_cta: url destination",
			section: ProductPageSection{ID: "sec-bad", Type: "final_cta", Enabled: true, SortOrder: 3,
				Content: map[string]any{
					"action": "add_to_cart", "url": "https://external.example",
					"en": map[string]any{"title": "T"},
				}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bad := SellerListingPresentation{
				SellerListingID:  ids.listingID,
				PurchaseBehavior: "add_to_cart",
				Sections:         []ProductPageSection{tc.section},
			}
			if _, err := e.service.UpdateListingPresentationForSubject(ctx, ids.subject, ids.storeID, ids.listingID, bad); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}

			// The persisted presentation must be unchanged.
			persisted, err := e.repo.GetSellerListingPresentation(ctx, ids.listingID)
			if err != nil {
				t.Fatalf("GetSellerListingPresentation: %v", err)
			}
			if len(persisted.Sections) != 1 || persisted.Sections[0].ID != "sec-ok" {
				t.Fatalf("invalid section was persisted: %+v", persisted.Sections)
			}
		})
	}
}

// TestPublishRaceInvalidPresentation proves the final publish transaction
// revalidates the persisted presentation: presentation invalidated between the
// pre-check and the publish commit must prevent publication.
func TestPublishRaceInvalidPresentation(t *testing.T) {
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

	publishRaceHook = func() {
		// Concurrent authoring lands an invalid final_cta directly in the
		// database between the pre-check and the publish commit.
		invalid := SellerListingPresentation{
			SellerListingID:  ids.listingID,
			PurchaseBehavior: "add_to_cart",
			Sections: []ProductPageSection{{
				ID: "sec-race", Type: "final_cta", Enabled: true, SortOrder: 9,
				Content: map[string]any{
					"action": "open_external", "en": map[string]any{"title": "T"},
				},
			}},
		}
		if _, err := e.repo.UpsertSellerListingPresentation(ctx, invalid); err != nil {
			t.Errorf("race hook UpsertSellerListingPresentation: %v", err)
		}
	}
	defer func() { publishRaceHook = nil }()

	if err := e.service.PublishSellerProductForSubject(ctx, ids.subject, ids.storeID, ids.productID); err == nil {
		t.Fatalf("expected publish to fail when presentation was invalidated before commit")
	}

	prod, err := e.repo.GetProductByID(ctx, ids.productID)
	if err != nil {
		t.Fatalf("GetProductByID: %v", err)
	}
	if prod.Status != "draft" && prod.Status != "inactive" {
		t.Fatalf("product must remain inactive/draft, got %s", prod.Status)
	}
	listing, err := e.repo.GetSellerListingByStoreAndProduct(ctx, ids.storeID, ids.productID)
	if err != nil {
		t.Fatalf("GetSellerListingByStoreAndProduct: %v", err)
	}
	if listing.Status != "draft" && listing.Status != "inactive" && listing.Status != "unpublished" {
		t.Fatalf("listing must remain inactive/draft/unpublished, got %s", listing.Status)
	}
}

func TestConcurrentSupplierOfferImportIdempotency(t *testing.T) {
	e, _ := setupP58TestDB(t)
	ctx := context.Background()
	suffix := e.suffix

	seller, err := e.repo.CreateSeller(ctx, "concurrent-seller-"+suffix, "Concurrent Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	subject := "concurrent-owner-" + suffix
	if _, err := e.repo.CreateSellerMember(ctx, seller.ID, subject, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	store, err := e.repo.CreateStore(ctx, seller.ID, "EG", "concurrent-store-"+suffix, "Concurrent Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}

	supplier, err := e.repo.CreateSupplier(ctx, "concurrent-supplier-"+suffix, "Concurrent Supplier", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplier: %v", err)
	}
	market, err := e.repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplierMarket: %v", err)
	}
	product, err := e.repo.CreateProduct(ctx, "concurrent-product-"+suffix, "active")
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	supplierProduct, err := e.repo.CreateSupplierProduct(ctx, supplier.ID, product.ID, "CONCURRENT-SKU", "active")
	if err != nil {
		t.Fatalf("CreateSupplierProduct: %v", err)
	}
	offer, err := e.repo.CreateSupplierOffer(ctx, supplier.ID, supplierProduct.ID, market.ID, "EG", "active")
	if err != nil {
		t.Fatalf("CreateSupplierOffer: %v", err)
	}

	const concurrency = 10
	var wg sync.WaitGroup
	errs := make(chan error, concurrency)
	results := make(chan string, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			listing, err := e.service.ImportSupplierOfferForSubject(ctx, subject, store.ID, offer.ID)
			if err != nil {
				errs <- err
				return
			}
			results <- listing.ID
		}()
	}

	wg.Wait()
	close(errs)
	close(results)

	for err := range errs {
		t.Fatalf("concurrent import failed: %v", err)
	}

	var firstListingID string
	var count int
	for id := range results {
		count++
		if firstListingID == "" {
			firstListingID = id
		} else if firstListingID != id {
			t.Fatalf("concurrent imports returned different listing IDs: %s vs %s", firstListingID, id)
		}
	}

	if count != concurrency {
		t.Fatalf("expected %d successful import responses, got %d", concurrency, count)
	}

	// Verify only 1 listing exists in database
	var dbCount int
	if err := e.db.Pool.QueryRow(ctx, "SELECT COUNT(*) FROM seller_listings WHERE store_id = $1 AND supplier_offer_id = $2", store.ID, offer.ID).Scan(&dbCount); err != nil {
		t.Fatalf("count seller_listings: %v", err)
	}
	if dbCount != 1 {
		t.Fatalf("expected 1 listing in DB, got %d", dbCount)
	}
}

func TestSupplierOfferImportEligibilityValidation(t *testing.T) {
	e, _ := setupP58TestDB(t)
	ctx := context.Background()
	suffix := e.suffix

	seller, err := e.repo.CreateSeller(ctx, "elig-seller-"+suffix, "Eligibility Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	ownerSubject := "elig-owner-" + suffix
	if _, err := e.repo.CreateSellerMember(ctx, seller.ID, ownerSubject, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	staffSubject := "elig-staff-" + suffix
	if _, err := e.repo.CreateSellerMember(ctx, seller.ID, staffSubject, "staff", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	storeEG, err := e.repo.CreateStore(ctx, seller.ID, "EG", "elig-store-eg-"+suffix, "EG Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore EG: %v", err)
	}

	sellerSA, err := e.repo.CreateSeller(ctx, "elig-seller-sa-"+suffix, "Eligibility Seller SA", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller SA: %v", err)
	}
	ownerSubjectSA := "elig-owner-sa-" + suffix
	if _, err := e.repo.CreateSellerMember(ctx, sellerSA.ID, ownerSubjectSA, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember SA: %v", err)
	}
	storeSA, err := e.repo.CreateStore(ctx, sellerSA.ID, "SA", "elig-store-sa-"+suffix, "SA Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore SA: %v", err)
	}

	supplier, err := e.repo.CreateSupplier(ctx, "elig-supplier-"+suffix, "Eligibility Supplier", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplier: %v", err)
	}
	marketEG, err := e.repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplierMarket: %v", err)
	}

	productDraft, err := e.repo.CreateProduct(ctx, "draft-product-"+suffix, "draft")
	if err != nil {
		t.Fatalf("CreateProduct draft: %v", err)
	}
	suppProdDraft, err := e.repo.CreateSupplierProduct(ctx, supplier.ID, productDraft.ID, "DRAFT-SKU", "active")
	if err != nil {
		t.Fatalf("CreateSupplierProduct: %v", err)
	}
	offerDraftProd, err := e.repo.CreateSupplierOffer(ctx, supplier.ID, suppProdDraft.ID, marketEG.ID, "EG", "active")
	if err != nil {
		t.Fatalf("CreateSupplierOffer: %v", err)
	}

	// 1. Staff role must be forbidden
	_, err = e.service.ImportSupplierOfferForSubject(ctx, staffSubject, storeEG.ID, offerDraftProd.ID)
	if !errors.Is(err, ErrUnauthorized) && !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected authorization error for staff import, got %v", err)
	}

	// 2. Draft product must fail
	_, err = e.service.ImportSupplierOfferForSubject(ctx, ownerSubject, storeEG.ID, offerDraftProd.ID)
	if !errors.Is(err, ErrOfferUnavailable) {
		t.Fatalf("expected ErrOfferUnavailable for draft product offer, got %v", err)
	}

	// Active product
	productActive, err := e.repo.CreateProduct(ctx, "active-product-"+suffix, "active")
	if err != nil {
		t.Fatalf("CreateProduct active: %v", err)
	}
	suppProdActive, err := e.repo.CreateSupplierProduct(ctx, supplier.ID, productActive.ID, "ACTIVE-SKU", "active")
	if err != nil {
		t.Fatalf("CreateSupplierProduct: %v", err)
	}
	offerEG, err := e.repo.CreateSupplierOffer(ctx, supplier.ID, suppProdActive.ID, marketEG.ID, "EG", "active")
	if err != nil {
		t.Fatalf("CreateSupplierOffer EG: %v", err)
	}

	// 3. Market mismatch (EG offer into SA store)
	_, err = e.service.ImportSupplierOfferForSubject(ctx, ownerSubjectSA, storeSA.ID, offerEG.ID)
	if !errors.Is(err, ErrMarketMismatch) {
		t.Fatalf("expected ErrMarketMismatch for cross-market import, got %v", err)
	}

	// 4. Inactive supplier offer
	productForInactive, err := e.repo.CreateProduct(ctx, "inactive-prod-"+suffix, "active")
	if err != nil {
		t.Fatalf("CreateProduct inactive: %v", err)
	}
	suppProdForInactive, err := e.repo.CreateSupplierProduct(ctx, supplier.ID, productForInactive.ID, "INACTIVE-SKU", "active")
	if err != nil {
		t.Fatalf("CreateSupplierProduct for inactive: %v", err)
	}
	offerInactive, err := e.repo.CreateSupplierOffer(ctx, supplier.ID, suppProdForInactive.ID, marketEG.ID, "EG", "draft")
	if err != nil {
		t.Fatalf("CreateSupplierOffer inactive: %v", err)
	}
	_, err = e.service.ImportSupplierOfferForSubject(ctx, ownerSubject, storeEG.ID, offerInactive.ID)
	if !errors.Is(err, ErrOfferUnavailable) {
		t.Fatalf("expected ErrOfferUnavailable for inactive offer, got %v", err)
	}

	// 5. Valid import succeeds
	listing, err := e.service.ImportSupplierOfferForSubject(ctx, ownerSubject, storeEG.ID, offerEG.ID)
	if err != nil {
		t.Fatalf("valid import failed: %v", err)
	}
	if listing.Status != "draft" {
		t.Fatalf("expected initial status draft, got %s", listing.Status)
	}
	if listing.SupplierOfferID == nil || *listing.SupplierOfferID != offerEG.ID {
		t.Fatalf("expected supplier_offer_id %s, got %v", offerEG.ID, listing.SupplierOfferID)
	}
}

func TestSupplierOfferUnavailableLifecycle(t *testing.T) {
	e, _ := setupP58TestDB(t)
	ctx := context.Background()
	suffix := e.suffix

	seller, err := e.repo.CreateSeller(ctx, "life-seller-"+suffix, "Life Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	subject := "life-owner-" + suffix
	if _, err := e.repo.CreateSellerMember(ctx, seller.ID, subject, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	store, err := e.repo.CreateStore(ctx, seller.ID, "EG", "life-store-"+suffix, "Life Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}

	supplier, err := e.repo.CreateSupplier(ctx, "life-supplier-"+suffix, "Life Supplier", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplier: %v", err)
	}
	market, err := e.repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplierMarket: %v", err)
	}
	product, err := e.repo.CreateProduct(ctx, "life-prod-"+suffix, "active")
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	suppProd, err := e.repo.CreateSupplierProduct(ctx, supplier.ID, product.ID, "LIFE-SKU", "active")
	if err != nil {
		t.Fatalf("CreateSupplierProduct: %v", err)
	}
	offer, err := e.repo.CreateSupplierOffer(ctx, supplier.ID, suppProd.ID, market.ID, "EG", "active")
	if err != nil {
		t.Fatalf("CreateSupplierOffer: %v", err)
	}

	// Add price and availability to supplier offer
	if _, err := e.db.Pool.Exec(ctx, `
		INSERT INTO supplier_offer_prices (id, supplier_offer_id, amount_minor, currency_code, is_current)
		VALUES (gen_random_uuid(), $1, 5000, 'EGP', true)
	`, offer.ID); err != nil {
		t.Fatalf("insert supplier_offer_prices: %v", err)
	}
	if _, err := e.db.Pool.Exec(ctx, `
		INSERT INTO supplier_offer_availability (id, supplier_offer_id, is_available, available_qty)
		VALUES (gen_random_uuid(), $1, true, 20)
	`, offer.ID); err != nil {
		t.Fatalf("insert supplier_offer_availability: %v", err)
	}

	// 1. Import offer
	listing, err := e.service.ImportSupplierOfferForSubject(ctx, subject, store.ID, offer.ID)
	if err != nil {
		t.Fatalf("ImportSupplierOffer: %v", err)
	}

	// 2. Set seller retail price and set listing to published
	if _, err := e.service.SetListingPriceForSubject(ctx, subject, store.ID, listing.ID, 9000, "EGP"); err != nil {
		t.Fatalf("SetListingPrice: %v", err)
	}
	if _, err := e.db.Pool.Exec(ctx, `UPDATE seller_listings SET status = 'published' WHERE id = $1`, listing.ID); err != nil {
		t.Fatalf("update listing status: %v", err)
	}

	// 3. Check lifecycle when active
	status, err := e.service.GetSellerListingLifecycleForSubject(ctx, subject, store.ID, listing.ID)
	if err != nil {
		t.Fatalf("GetSellerListingLifecycleForSubject: %v", err)
	}
	if status.EffectiveAvailability != "available" || !status.IsUpstreamAvailable || status.HasMarginWarning {
		t.Fatalf("expected available active status, got effective=%s upstream=%v margin=%v", status.EffectiveAvailability, status.IsUpstreamAvailable, status.HasMarginWarning)
	}

	// 4. Supplier offer is deactivated
	if _, err := e.db.Pool.Exec(ctx, `UPDATE supplier_offers SET status = 'inactive' WHERE id = $1`, offer.ID); err != nil {
		t.Fatalf("deactivate supplier offer: %v", err)
	}

	statusInactive, err := e.service.GetSellerListingLifecycleForSubject(ctx, subject, store.ID, listing.ID)
	if err != nil {
		t.Fatalf("GetSellerListingLifecycleForSubject: %v", err)
	}
	if statusInactive.EffectiveAvailability != "upstream_unavailable" || statusInactive.IsUpstreamAvailable {
		t.Fatalf("expected upstream_unavailable status, got effective=%s upstream=%v", statusInactive.EffectiveAvailability, statusInactive.IsUpstreamAvailable)
	}

	// Verify seller retail price is strictly preserved
	price, err := e.repo.GetSellerListingPrice(ctx, listing.ID)
	if err != nil {
		t.Fatalf("GetSellerListingPrice: %v", err)
	}
	if price.Price.AmountMinor != 9000 {
		t.Fatalf("expected retail price 9000, got %d", price.Price.AmountMinor)
	}

	// 5. Supplier reactivates offer -> listing automatically restores availability
	if _, err := e.db.Pool.Exec(ctx, `UPDATE supplier_offers SET status = 'active' WHERE id = $1`, offer.ID); err != nil {
		t.Fatalf("reactivate supplier offer: %v", err)
	}

	statusRestored, err := e.service.GetSellerListingLifecycleForSubject(ctx, subject, store.ID, listing.ID)
	if err != nil {
		t.Fatalf("GetSellerListingLifecycleForSubject: %v", err)
	}
	if statusRestored.EffectiveAvailability != "available" || !statusRestored.IsUpstreamAvailable {
		t.Fatalf("expected restored available status, got effective=%s upstream=%v", statusRestored.EffectiveAvailability, statusRestored.IsUpstreamAvailable)
	}
}

func TestWholesalePriceShiftPreservesSellerRetailPrice(t *testing.T) {
	e, _ := setupP58TestDB(t)
	ctx := context.Background()
	suffix := e.suffix

	seller, err := e.repo.CreateSeller(ctx, "price-seller-"+suffix, "Price Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	subject := "price-owner-" + suffix
	if _, err := e.repo.CreateSellerMember(ctx, seller.ID, subject, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	store, err := e.repo.CreateStore(ctx, seller.ID, "EG", "price-store-"+suffix, "Price Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}

	supplier, err := e.repo.CreateSupplier(ctx, "price-supplier-"+suffix, "Price Supplier", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplier: %v", err)
	}
	market, err := e.repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplierMarket: %v", err)
	}
	product, err := e.repo.CreateProduct(ctx, "price-prod-"+suffix, "active")
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	suppProd, err := e.repo.CreateSupplierProduct(ctx, supplier.ID, product.ID, "PRICE-SKU", "active")
	if err != nil {
		t.Fatalf("CreateSupplierProduct: %v", err)
	}
	offer, err := e.repo.CreateSupplierOffer(ctx, supplier.ID, suppProd.ID, market.ID, "EG", "active")
	if err != nil {
		t.Fatalf("CreateSupplierOffer: %v", err)
	}

	// Initial wholesale price = 40.00 EGP
	if _, err := e.db.Pool.Exec(ctx, `
		INSERT INTO supplier_offer_prices (id, supplier_offer_id, amount_minor, currency_code, is_current)
		VALUES (gen_random_uuid(), $1, 4000, 'EGP', true)
	`, offer.ID); err != nil {
		t.Fatalf("insert supplier_offer_prices: %v", err)
	}
	if _, err := e.db.Pool.Exec(ctx, `
		INSERT INTO supplier_offer_availability (id, supplier_offer_id, is_available, available_qty)
		VALUES (gen_random_uuid(), $1, true, 10)
	`, offer.ID); err != nil {
		t.Fatalf("insert supplier_offer_availability: %v", err)
	}

	listing, err := e.service.ImportSupplierOfferForSubject(ctx, subject, store.ID, offer.ID)
	if err != nil {
		t.Fatalf("ImportSupplierOffer: %v", err)
	}

	// Seller sets retail price to 60.00 EGP
	if _, err := e.service.SetListingPriceForSubject(ctx, subject, store.ID, listing.ID, 6000, "EGP"); err != nil {
		t.Fatalf("SetListingPrice: %v", err)
	}

	// Margin warning should be false
	statusBefore, err := e.service.GetSellerListingLifecycleForSubject(ctx, subject, store.ID, listing.ID)
	if err != nil {
		t.Fatalf("GetSellerListingLifecycleForSubject: %v", err)
	}
	if statusBefore.HasMarginWarning {
		t.Fatalf("expected HasMarginWarning false, got true")
	}

	// Supplier increases wholesale price to 75.00 EGP (exceeding retail price)
	if _, err := e.db.Pool.Exec(ctx, `UPDATE supplier_offer_prices SET amount_minor = 7500 WHERE supplier_offer_id = $1`, offer.ID); err != nil {
		t.Fatalf("update supplier_offer_prices: %v", err)
	}

	// Margin warning should now be true
	statusAfter, err := e.service.GetSellerListingLifecycleForSubject(ctx, subject, store.ID, listing.ID)
	if err != nil {
		t.Fatalf("GetSellerListingLifecycleForSubject: %v", err)
	}
	if !statusAfter.HasMarginWarning {
		t.Fatalf("expected HasMarginWarning true, got false")
	}

	// Retail price is unchanged
	retailPrice, err := e.repo.GetSellerListingPrice(ctx, listing.ID)
	if err != nil {
		t.Fatalf("GetSellerListingPrice: %v", err)
	}
	if retailPrice.Price.AmountMinor != 6000 {
		t.Fatalf("expected retail price preserved at 6000, got %d", retailPrice.Price.AmountMinor)
	}
}

func TestStockExhaustionAvailabilityTransition(t *testing.T) {
	e, _ := setupP58TestDB(t)
	ctx := context.Background()
	suffix := e.suffix

	seller, err := e.repo.CreateSeller(ctx, "stock-seller-"+suffix, "Stock Seller", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	subject := "stock-owner-" + suffix
	if _, err := e.repo.CreateSellerMember(ctx, seller.ID, subject, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	store, err := e.repo.CreateStore(ctx, seller.ID, "EG", "stock-store-"+suffix, "Stock Store", "active", nil)
	if err != nil {
		t.Fatalf("CreateStore: %v", err)
	}

	supplier, err := e.repo.CreateSupplier(ctx, "stock-supplier-"+suffix, "Stock Supplier", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplier: %v", err)
	}
	market, err := e.repo.CreateSupplierMarket(ctx, supplier.ID, "EG", "active", nil)
	if err != nil {
		t.Fatalf("CreateSupplierMarket: %v", err)
	}
	product, err := e.repo.CreateProduct(ctx, "stock-prod-"+suffix, "active")
	if err != nil {
		t.Fatalf("CreateProduct: %v", err)
	}
	suppProd, err := e.repo.CreateSupplierProduct(ctx, supplier.ID, product.ID, "STOCK-SKU", "active")
	if err != nil {
		t.Fatalf("CreateSupplierProduct: %v", err)
	}
	offer, err := e.repo.CreateSupplierOffer(ctx, supplier.ID, suppProd.ID, market.ID, "EG", "active")
	if err != nil {
		t.Fatalf("CreateSupplierOffer: %v", err)
	}

	if _, err := e.db.Pool.Exec(ctx, `
		INSERT INTO supplier_offer_availability (id, supplier_offer_id, is_available, available_qty)
		VALUES (gen_random_uuid(), $1, true, 5)
	`, offer.ID); err != nil {
		t.Fatalf("insert supplier_offer_availability: %v", err)
	}

	listing, err := e.service.ImportSupplierOfferForSubject(ctx, subject, store.ID, offer.ID)
	if err != nil {
		t.Fatalf("ImportSupplierOffer: %v", err)
	}
	if _, err := e.db.Pool.Exec(ctx, `UPDATE seller_listings SET status = 'published' WHERE id = $1`, listing.ID); err != nil {
		t.Fatalf("update listing status: %v", err)
	}

	// Stock = 5 -> Available
	s1, err := e.service.GetSellerListingLifecycleForSubject(ctx, subject, store.ID, listing.ID)
	if err != nil {
		t.Fatalf("GetSellerListingLifecycleForSubject: %v", err)
	}
	if s1.EffectiveAvailability != "available" {
		t.Fatalf("expected available, got %s", s1.EffectiveAvailability)
	}

	// Stock = 0 -> Out of stock
	if _, err := e.db.Pool.Exec(ctx, `UPDATE supplier_offer_availability SET available_qty = 0 WHERE supplier_offer_id = $1`, offer.ID); err != nil {
		t.Fatalf("update supplier_offer_availability: %v", err)
	}

	s2, err := e.service.GetSellerListingLifecycleForSubject(ctx, subject, store.ID, listing.ID)
	if err != nil {
		t.Fatalf("GetSellerListingLifecycleForSubject: %v", err)
	}
	if s2.EffectiveAvailability != "out_of_stock" {
		t.Fatalf("expected out_of_stock, got %s", s2.EffectiveAvailability)
	}
}

