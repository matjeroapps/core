package commerce

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

var scriptTagRegex = regexp.MustCompile(`(?i)<script|javascript:|on\w+=`)

func (s Service) CreateSellerProductForSubject(ctx context.Context, subject, storeID string, draft SellerProductDraft) (SellerProductDetail, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return SellerProductDetail{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return SellerProductDetail{}, err
	}

	prod, _, _, err := s.repo.CreateSellerProductAtomically(ctx, seller.ID, storeID, store.MarketCode, draft)
	if err != nil {
		return SellerProductDetail{}, err
	}

	return s.GetSellerProductDetailForSubject(ctx, subject, storeID, prod.ID)
}

func (s Service) GetSellerProductDetailForSubject(ctx context.Context, subject, storeID, productID string) (SellerProductDetail, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return SellerProductDetail{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return SellerProductDetail{}, err
	}

	// Verify listing belongs to this store
	listing, err := s.repo.GetSellerListingByStoreAndProduct(ctx, storeID, productID)
	if err != nil {
		return SellerProductDetail{}, err
	}

	prod, err := s.repo.GetProductByID(ctx, productID)
	if err != nil {
		return SellerProductDetail{}, err
	}

	source := "seller_owned"
	if listing.SupplierOfferID != nil {
		source = "supplier_backed"
	} else {
		// Verify seller ownership
		if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
			return SellerProductDetail{}, ErrNotFound
		}
	}

	translations, err := s.repo.GetProductTranslations(ctx, productID)
	if err != nil {
		return SellerProductDetail{}, err
	}

	categories, err := s.repo.GetProductCategories(ctx, productID)
	if err != nil {
		return SellerProductDetail{}, err
	}

	variants, err := s.repo.ListVariantsByProductID(ctx, productID)
	if err != nil {
		return SellerProductDetail{}, err
	}

	skus, err := s.repo.ListSKUsByProductID(ctx, productID)
	if err != nil {
		return SellerProductDetail{}, err
	}

	media, err := s.repo.ListMediaByProductID(ctx, productID)
	if err != nil {
		return SellerProductDetail{}, err
	}

	var price *SellerListingPrice
	if pr, err := s.repo.GetSellerListingPrice(ctx, listing.ID); err == nil {
		price = &pr
	}

	presentation, err := s.repo.GetSellerListingPresentation(ctx, listing.ID)
	if err != nil {
		presentation = SellerListingPresentation{
			SellerListingID:  listing.ID,
			SchemaVersion:    1,
			PurchaseBehavior: "inherit",
			Sections:         []ProductPageSection{},
		}
	}

	// Resolve effective purchase behavior
	effectiveBehavior := presentation.PurchaseBehavior
	if effectiveBehavior == "inherit" || effectiveBehavior == "" {
		storeSettings, _ := s.repo.GetStoreSettings(ctx, storeID)
		if pb, ok := storeSettings["purchase_behavior"].(string); ok && pb == "buy_now" {
			effectiveBehavior = "buy_now"
		} else {
			effectiveBehavior = "add_to_cart"
		}
	}

	// Inventory summaries
	inventorySnapshots, _ := s.repo.ListStoreInventorySnapshots(ctx, storeID)
	locations, _ := s.repo.ListStoreFulfillmentLocations(ctx, storeID)
	locNameMap := make(map[string]string)
	for _, l := range locations {
		locNameMap[l.ID] = l.Name
	}
	var inventorySummary []SellerInventorySummary
	for _, snap := range inventorySnapshots {
		// Only include snapshots for SKUs of this product
		for _, sku := range skus {
			if snap.SKUID == sku.ID {
				avail := snap.OnHandQty - snap.ReservedQty
				if avail < 0 {
					avail = 0
				}
				inventorySummary = append(inventorySummary, SellerInventorySummary{
					ID:                    snap.ID,
					FulfillmentLocationID: snap.FulfillmentLocationID,
					LocationName:          locNameMap[snap.FulfillmentLocationID],
					SKUID:                 snap.SKUID,
					OnHandQty:             snap.OnHandQty,
					ReservedQty:           snap.ReservedQty,
					AvailableQty:          avail,
					Version:               snap.Version,
				})
			}
		}
	}

	readiness := s.evaluateReadinessInternal(store, prod, translations, variants, skus, media, listing, price, presentation, locations, inventorySnapshots)

	return SellerProductDetail{
		Product:          prod,
		Source:           source,
		Translations:     translations,
		Categories:       categories,
		Variants:         variants,
		SKUs:             skus,
		Media:            media,
		Listing:          listing,
		Price:            price,
		InventorySummary: inventorySummary,
		Presentation:     &presentation,
		PurchaseBehavior: effectiveBehavior,
		PublishReadiness: readiness,
	}, nil
}

func (s Service) evaluateReadinessInternal(
	store Store,
	prod Product,
	translations []ProductTranslation,
	variants []Variant,
	skus []SKU,
	media []MediaMetadata,
	listing SellerListing,
	price *SellerListingPrice,
	presentation SellerListingPresentation,
	locations []FulfillmentLocation,
	snapshots []InventorySnapshot,
) PublishReadiness {
	var reasons []string

	// 1. Store must be active
	if store.Status != "active" {
		reasons = append(reasons, "Store must be active")
	}

	// 2. Translations / English or Arabic name exists
	hasName := false
	for _, tr := range translations {
		if strings.TrimSpace(tr.Name) != "" {
			hasName = true
			break
		}
	}
	if !hasName {
		reasons = append(reasons, "Product title/translation is missing")
	}

	// 3. Active Variant
	activeVariants := make([]Variant, 0, len(variants))
	for _, v := range variants {
		if v.Status == "active" {
			activeVariants = append(activeVariants, v)
		}
	}
	if len(activeVariants) == 0 {
		reasons = append(reasons, "At least one active Variant is required")
	}

	// 4. Each active Variant must have exactly one active selectable SKU
	activeSKUByVariant := map[string]int{}
	for _, sku := range skus {
		if sku.Status == "active" {
			activeSKUByVariant[sku.VariantID]++
		}
	}
	activeSKUs := make([]SKU, 0, len(skus))
	for _, v := range activeVariants {
		count := activeSKUByVariant[v.ID]
		if count != 1 {
			reasons = append(reasons, fmt.Sprintf("Variant %s must have exactly one active selectable SKU", v.Code))
		}
	}
	for _, sku := range skus {
		if sku.Status == "active" && activeSKUByVariant[sku.VariantID] > 0 {
			activeSKUs = append(activeSKUs, sku)
		}
	}

	// 5. Canonical Listing: belongs to store, market matches store market
	if listing.ID == "" {
		reasons = append(reasons, "Canonical listing is missing")
	} else {
		if listing.StoreID != store.ID {
			reasons = append(reasons, "Listing does not belong to store")
		}
		if listing.MarketCode != store.MarketCode {
			reasons = append(reasons, fmt.Sprintf("Listing market %s does not match store market %s", listing.MarketCode, store.MarketCode))
		}
	}

	// 6. Current Listing price exists & matches Store market currency
	if price == nil || !price.IsCurrent {
		reasons = append(reasons, "Current retail price is missing")
	} else if price.Price.Currency != store.MarketCode && !isMarketCurrencyMatch(price.Price.Currency, store.MarketCode) {
		reasons = append(reasons, fmt.Sprintf("Price currency %s does not match store market currency %s", price.Price.Currency, store.MarketCode))
	}

	// 7. At least one image exists
	if len(media) == 0 {
		reasons = append(reasons, "At least one product image is required")
	}

	// 8. Presentation must pass typed section validation
	mediaIDs := make(map[string]bool, len(media))
	for _, m := range media {
		mediaIDs[m.ID] = true
	}
	if pbReasons := validatePurchaseBehavior(presentation.PurchaseBehavior); len(pbReasons) > 0 {
		reasons = append(reasons, pbReasons...)
	}
	reasons = append(reasons, ValidateProductPageSections(presentation.Sections, mediaIDs)...)

	// 9. Sellable inventory topology: an inventory snapshot only satisfies
	// readiness when it belongs to an active SKU of this product and sits at
	// an active, store-owned (non-supplier), market-matching location with
	// available quantity satisfying storefront sellability.
	locByID := make(map[string]FulfillmentLocation, len(locations))
	for _, l := range locations {
		locByID[l.ID] = l
	}
	activeSKUIDs := make(map[string]bool, len(activeSKUs))
	for _, sku := range activeSKUs {
		activeSKUIDs[sku.ID] = true
	}
	hasSellableInventory := false
	for _, snap := range snapshots {
		if !activeSKUIDs[snap.SKUID] {
			continue
		}
		loc, ok := locByID[snap.FulfillmentLocationID]
		if !ok || loc.StoreID != store.ID || loc.SupplierID != "" {
			continue
		}
		if loc.Status != "active" || loc.MarketCode != store.MarketCode {
			continue
		}
		if snap.OnHandQty-snap.ReservedQty > 0 {
			hasSellableInventory = true
			break
		}
	}
	if !hasSellableInventory {
		reasons = append(reasons, "Sellable inventory at an active store location is required")
	}

	return PublishReadiness{
		IsReady: len(reasons) == 0,
		Reasons: reasons,
	}
}

func validatePurchaseBehavior(behavior string) []string {
	switch behavior {
	case "", "inherit", "add_to_cart", "buy_now":
		return nil
	default:
		return []string{fmt.Sprintf("Invalid purchase behavior %s", behavior)}
	}
}

func (s Service) ListSellerProductsForSubject(ctx context.Context, subject, storeID, statusFilter, sourceFilter, queryFilter string, limit, offset int) ([]SellerProductListItem, int, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return nil, 0, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return nil, 0, err
	}

	return s.repo.ListStoreProducts(ctx, storeID, statusFilter, sourceFilter, queryFilter, limit, offset)
}

func (s Service) UpdateSellerProductForSubject(ctx context.Context, subject, storeID, productID, slug string, translations []ProductTranslation, categoryIDs []string) (SellerProductDetail, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return SellerProductDetail{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return SellerProductDetail{}, err
	}

	// Verify seller ownership (cannot update supplier-backed global products)
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
		return SellerProductDetail{}, ErrNotFound
	}

	if slug != "" {
		if _, err := s.repo.UpdateSellerProduct(ctx, productID, slug); err != nil {
			return SellerProductDetail{}, err
		}
	}

	for _, tr := range translations {
		if tr.Locale != "" && tr.Name != "" {
			tr.ProductID = productID
			if err := s.repo.UpsertProductTranslation(ctx, tr); err != nil {
				return SellerProductDetail{}, err
			}
		}
	}

	if categoryIDs != nil {
		if err := s.repo.SetProductCategories(ctx, productID, categoryIDs); err != nil {
			return SellerProductDetail{}, err
		}
	}

	return s.GetSellerProductDetailForSubject(ctx, subject, storeID, productID)
}

func (s Service) CreateVariantForSubject(ctx context.Context, subject, storeID, productID, code, status string) (Variant, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return Variant{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return Variant{}, err
	}
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
		return Variant{}, ErrNotFound
	}

	return s.repo.CreateVariant(ctx, productID, code, status)
}

func (s Service) UpdateVariantForSubject(ctx context.Context, subject, storeID, productID, variantID, code, status string) (Variant, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return Variant{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return Variant{}, err
	}
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
		return Variant{}, ErrNotFound
	}
	variant, err := s.repo.GetVariantByID(ctx, variantID)
	if err != nil || variant.ProductID != productID {
		return Variant{}, ErrNotFound
	}

	return s.repo.UpdateVariant(ctx, variantID, code, status)
}

func (s Service) CreateSKUForSubject(ctx context.Context, subject, storeID, productID, variantID, code, barcode, status string) (SKU, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return SKU{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return SKU{}, err
	}
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
		return SKU{}, ErrNotFound
	}
	variant, err := s.repo.GetVariantByID(ctx, variantID)
	if err != nil || variant.ProductID != productID {
		return SKU{}, ErrNotFound
	}

	// The 1-active-SKU-per-variant invariant is enforced in one transaction
	// (plus a partial unique index at the DB level), never by a separate
	// deactivate-then-create sequence whose errors can be swallowed.
	return s.repo.CreateSKUReplacingActive(ctx, variantID, code, barcode, status)
}

func (s Service) UpdateSKUForSubject(ctx context.Context, subject, storeID, productID, variantID, skuID, code, barcode, status string) (SKU, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return SKU{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return SKU{}, err
	}
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
		return SKU{}, ErrNotFound
	}
	variant, err := s.repo.GetVariantByID(ctx, variantID)
	if err != nil || variant.ProductID != productID {
		return SKU{}, ErrNotFound
	}
	sku, err := s.repo.GetSKUByID(ctx, skuID)
	if err != nil || sku.VariantID != variantID {
		return SKU{}, ErrNotFound
	}

	return s.repo.UpdateSKUReplacingActive(ctx, skuID, variantID, code, barcode, status)
}

func (s Service) GenerateMediaUploadPresignedURLForSubject(ctx context.Context, subject, storeID, productID string, req MediaUploadRequest) (MediaUploadResponse, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return MediaUploadResponse{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return MediaUploadResponse{}, err
	}
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
		return MediaUploadResponse{}, ErrNotFound
	}

	allowedMIME := map[string]string{
		"image/jpeg": ".jpg",
		"image/png":  ".png",
		"image/webp": ".webp",
	}
	ext, allowed := allowedMIME[strings.ToLower(req.ContentType)]
	if !allowed {
		return MediaUploadResponse{}, fmt.Errorf("%w: invalid image mime type %s", ErrInvalidInput, req.ContentType)
	}

	if req.SizeBytes <= 0 {
		return MediaUploadResponse{}, fmt.Errorf("%w: file size is required", ErrInvalidInput)
	}
	if req.SizeBytes > 10*1024*1024 {
		return MediaUploadResponse{}, fmt.Errorf("%w: file size exceeds 10MB limit", ErrInvalidInput)
	}

	randomID := uuid.NewString()
	storageKey := fmt.Sprintf("products/%s/%s/%s%s", seller.ID, productID, randomID, ext)

	if s.S3Storage == nil {
		return MediaUploadResponse{}, fmt.Errorf("%w: media storage is not configured", ErrInvalidInput)
	}

	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return MediaUploadResponse{}, fmt.Errorf("failed to generate upload token: %w", err)
	}
	rawToken := hex.EncodeToString(tokenBytes)
	digestBytes := sha256.Sum256([]byte(rawToken))
	tokenDigest := hex.EncodeToString(digestBytes[:])

	ttl := s.S3Storage.Config().URLTTL
	intent, err := s.repo.CreateMediaUploadIntent(ctx, MediaUploadIntent{
		SellerID:    seller.ID,
		StoreID:     storeID,
		ProductID:   productID,
		StorageKey:  storageKey,
		ContentType: req.ContentType,
		MaxBytes:    req.SizeBytes,
		TokenDigest: tokenDigest,
		ExpiresAt:   time.Now().Add(ttl),
	})
	if err != nil {
		return MediaUploadResponse{}, err
	}
	_ = intent

	uploadURL, err := s.S3Storage.PresignPutObject(ctx, storageKey, req.ContentType)
	if err != nil {
		return MediaUploadResponse{}, err
	}

	return MediaUploadResponse{
		UploadURL:   uploadURL,
		StorageKey:  storageKey,
		UploadToken: rawToken,
		ExpiresAt:   time.Now().Add(s.S3Storage.Config().URLTTL),
	}, nil
}

func (s Service) CompleteMediaUploadForSubject(ctx context.Context, subject, storeID, productID string, req CompleteMediaUploadRequest) (MediaMetadata, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return MediaMetadata{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return MediaMetadata{}, err
	}
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
		return MediaMetadata{}, ErrNotFound
	}
	if s.S3Storage == nil {
		return MediaMetadata{}, fmt.Errorf("%w: media storage is not configured", ErrInvalidInput)
	}

	// The upload intent is the authorization record: it binds the presigned
	// upload to one seller, store, product, content type, size limit and
	// token. A storage-key prefix check alone is not authorization.
	intent, err := s.repo.GetMediaUploadIntentByStorageKey(ctx, req.StorageKey)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return MediaMetadata{}, fmt.Errorf("%w: upload intent not found for storage key", ErrInvalidInput)
		}
		return MediaMetadata{}, err
	}
	if intent.SellerID != seller.ID {
		return MediaMetadata{}, fmt.Errorf("%w: upload intent belongs to a different seller", ErrInvalidInput)
	}
	if intent.StoreID != storeID {
		return MediaMetadata{}, fmt.Errorf("%w: upload intent belongs to a different store", ErrInvalidInput)
	}
	if intent.ProductID != productID {
		return MediaMetadata{}, fmt.Errorf("%w: upload intent belongs to a different product", ErrInvalidInput)
	}
	if time.Now().After(intent.ExpiresAt) {
		return MediaMetadata{}, fmt.Errorf("%w: upload intent expired", ErrInvalidInput)
	}

	// Idempotent completion: a completed intent for the same storage key and
	// product resolves to the media record created by the successful attempt.
	if intent.CompletedAt != nil {
		existing, err := s.repo.GetMediaMetadataByStorageKey(ctx, productID, req.StorageKey)
		if err != nil {
			return MediaMetadata{}, fmt.Errorf("%w: upload already completed", ErrInvalidInput)
		}
		return existing, nil
	}

	// Verify the upload token: SHA-256 of the raw token, compared in constant
	// time against the digest persisted at presign time. The raw token is
	// never logged.
	digest := sha256.Sum256([]byte(req.UploadToken))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(digest[:])), []byte(intent.TokenDigest)) != 1 {
		return MediaMetadata{}, fmt.Errorf("%w: upload token verification failed", ErrInvalidInput)
	}

	// Verify the actually uploaded object against the intent.
	head, err := s.S3Storage.HeadObject(ctx, req.StorageKey)
	if err != nil {
		return MediaMetadata{}, fmt.Errorf("%w: uploaded object does not exist in storage: %v", ErrInvalidInput, err)
	}
	if head.ContentLength == nil || *head.ContentLength <= 0 {
		return MediaMetadata{}, fmt.Errorf("%w: uploaded object is empty", ErrInvalidInput)
	}
	if intent.MaxBytes > 0 && *head.ContentLength > intent.MaxBytes {
		return MediaMetadata{}, fmt.Errorf("%w: uploaded object size %d exceeds limit %d", ErrInvalidInput, *head.ContentLength, intent.MaxBytes)
	}
	if head.ContentType != nil && intent.ContentType != "" && !strings.EqualFold(*head.ContentType, intent.ContentType) {
		return MediaMetadata{}, fmt.Errorf("%w: uploaded object content type %s does not match %s", ErrInvalidInput, *head.ContentType, intent.ContentType)
	}

	// The media type comes from the verified S3 metadata, never inferred from
	// the storage key extension.
	mediaType := strings.ToLower(intent.ContentType)

	m := MediaMetadata{
		ProductID:  productID,
		MediaType:  mediaType,
		URI:        s.S3Storage.ResolvePublicURI(req.StorageKey),
		AltText:    req.AltText,
		SortOrder:  req.SortOrder,
		StorageKey: &req.StorageKey,
		IsPrimary:  req.IsPrimary,
	}

	// Media insert + intent completion are one DB transaction; the storage
	// upload itself is inherently non-transactional and is verified above.
	created, err := s.repo.CompleteMediaUpload(ctx, m, intent.ID)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			// A concurrent completion won; return the record it created.
			return s.repo.GetMediaMetadataByStorageKey(ctx, productID, req.StorageKey)
		}
		return MediaMetadata{}, err
	}
	return created, nil
}

func (s Service) UpdateMediaMetadataForSubject(ctx context.Context, subject, storeID, productID, mediaID string, altText string, sortOrder int, isPrimary bool) (MediaMetadata, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return MediaMetadata{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return MediaMetadata{}, err
	}
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
		return MediaMetadata{}, ErrNotFound
	}
	m, err := s.repo.GetMediaMetadataByID(ctx, mediaID)
	if err != nil || m.ProductID != productID {
		return MediaMetadata{}, ErrNotFound
	}

	m.AltText = altText
	m.SortOrder = sortOrder
	m.IsPrimary = isPrimary

	return s.repo.UpdateMediaMetadata(ctx, m)
}

func (s Service) DeleteMediaMetadataForSubject(ctx context.Context, subject, storeID, productID, mediaID string) error {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return err
	}
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
		return ErrNotFound
	}
	m, err := s.repo.GetMediaMetadataByID(ctx, mediaID)
	if err != nil || m.ProductID != productID {
		return ErrNotFound
	}

	storageKey := m.StorageKey

	if err := s.repo.DeleteMediaMetadata(ctx, productID, mediaID); err != nil {
		return err
	}

	// Deterministic state after deleting the primary image: the earliest
	// remaining image becomes primary; if none remain there is no primary.
	if m.IsPrimary {
		remaining, err := s.repo.ListMediaByProductID(ctx, productID)
		if err == nil && len(remaining) > 0 {
			earliest := remaining[0]
			earliest.IsPrimary = true
			if _, err := s.repo.UpdateMediaMetadata(ctx, earliest); err != nil {
				return err
			}
		}
	}

	if storageKey != nil && *storageKey != "" && s.S3Storage != nil {
		_ = s.S3Storage.DeleteObject(ctx, *storageKey)
	}

	return nil
}

func (s Service) GetListingPresentationForSubject(ctx context.Context, subject, storeID, listingID string) (SellerListingPresentation, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return SellerListingPresentation{}, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return SellerListingPresentation{}, err
	}
	listing, err := s.repo.GetSellerListingByID(ctx, listingID)
	if err != nil || listing.StoreID != storeID {
		return SellerListingPresentation{}, ErrNotFound
	}

	return s.repo.GetSellerListingPresentation(ctx, listingID)
}

func (s Service) UpdateListingPresentationForSubject(ctx context.Context, subject, storeID, listingID string, pres SellerListingPresentation) (SellerListingPresentation, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return SellerListingPresentation{}, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return SellerListingPresentation{}, err
	}
	listing, err := s.repo.GetSellerListingByID(ctx, listingID)
	if err != nil || listing.StoreID != storeID {
		return SellerListingPresentation{}, ErrNotFound
	}

	// The typed validator is authoritative at save time: nothing invalid is
	// ever persisted through this API. Defense-in-depth (the script-tag
	// regex sweep) still runs after the structural validation.
	var reasons []string
	reasons = append(reasons, validatePurchaseBehavior(pres.PurchaseBehavior)...)

	media, err := s.repo.ListMediaByProductID(ctx, listing.ProductID)
	if err != nil {
		return SellerListingPresentation{}, err
	}
	mediaIDs := make(map[string]bool, len(media))
	for _, m := range media {
		mediaIDs[m.ID] = true
	}
	reasons = append(reasons, ValidateProductPageSections(pres.Sections, mediaIDs)...)
	if len(reasons) > 0 {
		return SellerListingPresentation{}, fmt.Errorf("%w: invalid presentation: %s", ErrInvalidInput, strings.Join(reasons, "; "))
	}

	if jsonBytes, err := json.Marshal(pres.Sections); err == nil {
		if scriptTagRegex.Match(jsonBytes) {
			return SellerListingPresentation{}, fmt.Errorf("%w: section content contains disallowed html/script tags", ErrInvalidInput)
		}
	}

	pres.SellerListingID = listingID
	return s.repo.UpsertSellerListingPresentation(ctx, pres)
}

func (s Service) ListStoreFulfillmentLocationsForSubject(ctx context.Context, subject, storeID string) ([]FulfillmentLocation, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return nil, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return nil, err
	}

	return s.repo.ListStoreFulfillmentLocations(ctx, storeID)
}

func (s Service) ListStoreInventoryForSubject(ctx context.Context, subject, storeID string) ([]SellerInventorySummary, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return nil, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return nil, err
	}

	snapshots, err := s.repo.ListStoreInventorySnapshots(ctx, storeID)
	if err != nil {
		return nil, err
	}

	locations, err := s.repo.ListStoreFulfillmentLocations(ctx, storeID)
	if err != nil {
		return nil, err
	}
	locNameMap := make(map[string]string)
	for _, l := range locations {
		locNameMap[l.ID] = l.Name
	}

	var result []SellerInventorySummary
	for _, snap := range snapshots {
		avail := snap.OnHandQty - snap.ReservedQty
		if avail < 0 {
			avail = 0
		}
		result = append(result, SellerInventorySummary{
			ID:                    snap.ID,
			FulfillmentLocationID: snap.FulfillmentLocationID,
			LocationName:          locNameMap[snap.FulfillmentLocationID],
			SKUID:                 snap.SKUID,
			OnHandQty:             snap.OnHandQty,
			ReservedQty:           snap.ReservedQty,
			AvailableQty:          avail,
			Version:               snap.Version,
		})
	}
	return result, nil
}

func (s Service) CreateInventorySnapshotForSubject(ctx context.Context, subject, storeID, locationID, skuID string, onHandQty int64) (InventorySnapshot, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return InventorySnapshot{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return InventorySnapshot{}, err
	}

	// Verify location belongs to store
	loc, err := s.repo.GetFulfillmentLocationByID(ctx, locationID)
	if err != nil || loc.StoreID != storeID || loc.SupplierID != "" {
		return InventorySnapshot{}, ErrNotFound
	}

	// Verify SKU belongs to a seller-owned product
	sku, err := s.repo.GetSKUByID(ctx, skuID)
	if err != nil {
		return InventorySnapshot{}, ErrNotFound
	}
	variant, err := s.repo.GetVariantByID(ctx, sku.VariantID)
	if err != nil {
		return InventorySnapshot{}, ErrNotFound
	}
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, variant.ProductID); err != nil {
		return InventorySnapshot{}, ErrNotFound
	}

	return s.repo.CreateInventorySnapshot(ctx, locationID, skuID, onHandQty)
}

func (s Service) AdjustStoreInventoryForSubject(ctx context.Context, subject, storeID, snapshotID string, quantityDelta int64, reason, correlationID string) (InventorySnapshot, InventoryMovement, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return InventorySnapshot{}, InventoryMovement{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return InventorySnapshot{}, InventoryMovement{}, err
	}

	snap, err := s.repo.GetInventorySnapshot(ctx, snapshotID)
	if err != nil {
		return InventorySnapshot{}, InventoryMovement{}, err
	}

	loc, err := s.repo.GetFulfillmentLocationByID(ctx, snap.FulfillmentLocationID)
	if err != nil || loc.StoreID != storeID || loc.SupplierID != "" {
		return InventorySnapshot{}, InventoryMovement{}, ErrNotFound
	}

	sku, err := s.repo.GetSKUByID(ctx, snap.SKUID)
	if err != nil {
		return InventorySnapshot{}, InventoryMovement{}, ErrNotFound
	}
	variant, err := s.repo.GetVariantByID(ctx, sku.VariantID)
	if err != nil {
		return InventorySnapshot{}, InventoryMovement{}, ErrNotFound
	}
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, variant.ProductID); err != nil {
		return InventorySnapshot{}, InventoryMovement{}, ErrNotFound
	}

	// Prevent reducing on_hand_qty below reserved_qty
	if snap.OnHandQty+quantityDelta < snap.ReservedQty {
		return InventorySnapshot{}, InventoryMovement{}, fmt.Errorf("%w: on_hand_qty cannot drop below reserved_qty (%d)", ErrInvalidInput, snap.ReservedQty)
	}

	return s.repo.AdjustInventory(ctx, snapshotID, quantityDelta, "adjustment", reason, subject, correlationID, "")
}

// publishRaceHook, when set, runs between the pre-transaction readiness
// evaluation and the atomic publish transaction. Tests use it to
// deterministically invalidate catalog state inside that window, proving that
// the final readiness revalidation inside the publish transaction catches it.
var publishRaceHook func()

func (s Service) PublishSellerProductForSubject(ctx context.Context, subject, storeID, productID string) error {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return err
	}
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
		return ErrNotFound
	}

	detail, err := s.GetSellerProductDetailForSubject(ctx, subject, storeID, productID)
	if err != nil {
		return err
	}

	if !detail.PublishReadiness.IsReady {
		return fmt.Errorf("%w: publish readiness failed: %s", ErrInvalidInput, strings.Join(detail.PublishReadiness.Reasons, "; "))
	}

	if publishRaceHook != nil {
		publishRaceHook()
	}

	// Final authoritative readiness validation happens inside the publish
	// transaction while product, listing and SKU rows are locked.
	return s.repo.PublishSellerProductAtomically(ctx, storeID, productID, store.MarketCode, marketCurrency(store.MarketCode))
}

func (s Service) UnpublishSellerProductForSubject(ctx context.Context, subject, storeID, productID string) error {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return err
	}
	if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
		return ErrNotFound
	}

	return s.repo.UnpublishSellerProduct(ctx, storeID, productID)
}

func (s Service) ListStoreOrdersForSubject(ctx context.Context, subject, storeID, statusFilter string, limit, offset int) ([]Order, int, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return nil, 0, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return nil, 0, err
	}

	return s.repo.ListStoreOrders(ctx, storeID, statusFilter, limit, offset)
}

func (s Service) TransitionStoreOrderForSubject(ctx context.Context, subject, storeID, orderID, targetStatus string, reason *string, correlationID string) (SellerOrderView, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return SellerOrderView{}, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return SellerOrderView{}, err
	}

	order, err := s.repo.GetOrderByID(ctx, nil, storeID, orderID)
	if err != nil || order.StoreID != storeID {
		return SellerOrderView{}, ErrNotFound
	}

	// Dispatch to existing lifecycle primitives
	switch targetStatus {
	case "confirmed":
		if order.Status != "pending" {
			return SellerOrderView{}, fmt.Errorf("%w: cannot confirm order in status %s", ErrInvalidTransition, order.Status)
		}
		order, err = s.ConfirmOrder(ctx, storeID, orderID, &subject, correlationID)

	case "cancelled":
		if order.Status == "pending" {
			order, err = s.CancelPendingOrder(ctx, storeID, orderID, AuthoritySeller, &subject, reason, correlationID)
		} else if order.Status == "confirmed" || order.Status == "processing" {
			order, err = s.CancelConfirmedOrder(ctx, storeID, orderID, AuthoritySeller, &subject, reason, correlationID)
		} else {
			return SellerOrderView{}, fmt.Errorf("%w: cannot cancel order in status %s", ErrInvalidTransition, order.Status)
		}

	case "processing":
		if order.Status != "confirmed" {
			return SellerOrderView{}, fmt.Errorf("%w: cannot transition to processing from status %s", ErrInvalidTransition, order.Status)
		}
		order, err = s.AdvanceOrderStatus(ctx, storeID, orderID, "processing", AuthoritySeller, &subject, reason, correlationID)

	case "ready_for_shipping":
		if order.Status != "processing" {
			return SellerOrderView{}, fmt.Errorf("%w: cannot transition to ready_for_shipping from status %s", ErrInvalidTransition, order.Status)
		}
		order, err = s.AdvanceOrderStatus(ctx, storeID, orderID, "ready_for_shipping", AuthoritySeller, &subject, reason, correlationID)

	case "shipped", "delivered":
		return SellerOrderView{}, fmt.Errorf("%w: shipping transitions are deferred beyond P5.8", ErrInvalidTransition)

	default:
		return SellerOrderView{}, fmt.Errorf("%w: unknown target status %s", ErrInvalidTransition, targetStatus)
	}
	if err != nil {
		return SellerOrderView{}, err
	}
	return s.sellerOrderView(ctx, order)
}

func marketCurrency(marketCode string) string {
	marketCurrencies := map[string]string{
		"EG": "EGP",
		"SA": "SAR",
		"US": "USD",
		"AE": "AED",
		"KW": "KWD",
	}
	if c, ok := marketCurrencies[marketCode]; ok {
		return c
	}
	return marketCode
}

func isMarketCurrencyMatch(currency, marketCode string) bool {
	return marketCurrency(marketCode) == currency
}

// SellerOrderView is the buyer-safe projection a Seller app receives for one
// of its store's orders. ContactEmail comes from the checkout session that
// produced the order and Timeline from the immutable order_timeline log; no
// supplier cost, reservation, fulfillment location or internal event metadata
// crosses this boundary.
type SellerOrderView struct {
	Order        Order           `json:"order"`
	ContactEmail string          `json:"contact_email"`
	Timeline     []OrderTimeline `json:"timeline"`
}

func (s Service) sellerOrderView(ctx context.Context, order Order) (SellerOrderView, error) {
	view := SellerOrderView{Order: order, Timeline: []OrderTimeline{}}

	email, err := s.repo.GetOrderContactEmail(ctx, order.CheckoutSessionID)
	if err == nil {
		view.ContactEmail = email
	}

	timeline, err := s.repo.ListOrderTimeline(ctx, order.ID)
	if err != nil {
		return SellerOrderView{}, err
	}
	view.Timeline = timeline
	return view, nil
}

func (s Service) GetStoreOrderForSubject(ctx context.Context, subject, storeID, orderID string) (SellerOrderView, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return SellerOrderView{}, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return SellerOrderView{}, err
	}

	order, err := s.repo.GetOrderByID(ctx, nil, storeID, orderID)
	if err != nil {
		return SellerOrderView{}, err
	}
	return s.sellerOrderView(ctx, order)
}

// aggregateInventorySummary rolls per-SKU inventory summaries up into the
// product-level aggregate exposed at the internal Seller API boundary.
func aggregateInventorySummary(items []SellerInventorySummary) SellerInventoryAggregate {
	agg := SellerInventoryAggregate{Locations: []SellerInventoryLocation{}}
	for _, item := range items {
		agg.TotalOnHand += item.OnHandQty
		agg.TotalReserved += item.ReservedQty
		agg.TotalAvailable += item.AvailableQty
		agg.Locations = append(agg.Locations, SellerInventoryLocation{
			LocationID:   item.FulfillmentLocationID,
			LocationName: item.LocationName,
			SKUID:        item.SKUID,
			OnHandQty:    item.OnHandQty,
			ReservedQty:  item.ReservedQty,
			AvailableQty: item.AvailableQty,
		})
	}
	return agg
}

// ListSellerProductViewsForSubject builds the product list projection for the
// internal Seller API: one row per listing carrying the display name, current
// price, inventory aggregate and publish readiness. Readiness here mirrors the
// detail-page topology rules using batched reads.
func (s Service) ListSellerProductViewsForSubject(ctx context.Context, subject, storeID, statusFilter, sourceFilter, queryFilter string, limit, offset int) ([]SellerProductListView, int, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return nil, 0, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return nil, 0, err
	}

	items, total, err := s.repo.ListStoreProducts(ctx, storeID, statusFilter, sourceFilter, queryFilter, limit, offset)
	if err != nil {
		return nil, 0, err
	}

	productIDs := make([]string, 0, len(items))
	for _, item := range items {
		productIDs = append(productIDs, item.Product.ID)
	}

	skusByProduct, err := s.repo.ListSKUsByProductIDs(ctx, productIDs)
	if err != nil {
		return nil, 0, err
	}
	variantCounts, err := s.repo.ListActiveVariantSKUCountsByProductIDs(ctx, productIDs)
	if err != nil {
		return nil, 0, err
	}
	names, err := s.repo.ListProductNamesByProductIDs(ctx, productIDs)
	if err != nil {
		return nil, 0, err
	}
	mediaCounts, err := s.repo.CountMediaByProductIDs(ctx, productIDs)
	if err != nil {
		return nil, 0, err
	}
	snapshots, _ := s.repo.ListStoreInventorySnapshots(ctx, storeID)
	locations, _ := s.repo.ListStoreFulfillmentLocations(ctx, storeID)

	validLocationByID := make(map[string]bool, len(locations))
	for _, l := range locations {
		validLocationByID[l.ID] = l.StoreID == store.ID && l.SupplierID == "" &&
			l.Status == "active" && l.MarketCode == store.MarketCode
	}
	activeVariantsByProduct := map[string]int{}
	okVariantsByProduct := map[string]int{}
	for _, c := range variantCounts {
		activeVariantsByProduct[c.ProductID]++
		if c.ActiveSKUQty == 1 {
			okVariantsByProduct[c.ProductID]++
		}
	}
	activeSKUIDsByProduct := map[string]map[string]bool{}
	skuToProduct := map[string]string{}
	for productID, skus := range skusByProduct {
		activeSKUIDsByProduct[productID] = map[string]bool{}
		for _, sku := range skus {
			skuToProduct[sku.ID] = productID
			if sku.Status == "active" {
				activeSKUIDsByProduct[productID][sku.ID] = true
			}
		}
	}
	sellableSKUsByProduct := map[string]bool{}
	for _, snap := range snapshots {
		productID, ok := skuToProduct[snap.SKUID]
		if !ok || !activeSKUIDsByProduct[productID][snap.SKUID] {
			continue
		}
		if !validLocationByID[snap.FulfillmentLocationID] {
			continue
		}
		if snap.OnHandQty-snap.ReservedQty > 0 {
			sellableSKUsByProduct[productID] = true
		}
	}

	views := make([]SellerProductListView, 0, len(items))
	for _, item := range items {
		view := SellerProductListView{
			Product:       item.Product,
			Source:        item.Source,
			Name:          names[item.Product.ID],
			ListingID:     item.Listing.ID,
			ListingStatus: item.Listing.Status,
			CurrentPrice:  item.Price,
		}

		// Inventory aggregate for this product's SKUs.
		locName := map[string]string{}
		for _, l := range locations {
			locName[l.ID] = l.Name
		}
		agg := SellerInventoryAggregate{Locations: []SellerInventoryLocation{}}
		for _, snap := range snapshots {
			if skuToProduct[snap.SKUID] != item.Product.ID {
				continue
			}
			avail := snap.OnHandQty - snap.ReservedQty
			if avail < 0 {
				avail = 0
			}
			agg.TotalOnHand += snap.OnHandQty
			agg.TotalReserved += snap.ReservedQty
			agg.TotalAvailable += avail
			agg.Locations = append(agg.Locations, SellerInventoryLocation{
				LocationID:   snap.FulfillmentLocationID,
				LocationName: locName[snap.FulfillmentLocationID],
				SKUID:        snap.SKUID,
				OnHandQty:    snap.OnHandQty,
				ReservedQty:  snap.ReservedQty,
				AvailableQty: avail,
			})
		}
		view.InventorySummary = agg

		var reasons []string
		if store.Status != "active" {
			reasons = append(reasons, "Store must be active")
		}
		if names[item.Product.ID] == "" {
			reasons = append(reasons, "Product title/translation is missing")
		}
		if activeVariantsByProduct[item.Product.ID] == 0 {
			reasons = append(reasons, "At least one active Variant is required")
		} else if okVariantsByProduct[item.Product.ID] != activeVariantsByProduct[item.Product.ID] {
			reasons = append(reasons, "Each active Variant must have exactly one active selectable SKU")
		}
		if item.Listing.StoreID != store.ID {
			reasons = append(reasons, "Listing does not belong to store")
		}
		if item.Listing.MarketCode != store.MarketCode {
			reasons = append(reasons, "Listing market does not match store market")
		}
		if item.Price == nil || !item.Price.IsCurrent {
			reasons = append(reasons, "Current retail price is missing")
		} else if item.Price.Price.Currency != store.MarketCode && !isMarketCurrencyMatch(item.Price.Price.Currency, store.MarketCode) {
			reasons = append(reasons, "Price currency does not match store market currency")
		}
		if mediaCounts[item.Product.ID] == 0 {
			reasons = append(reasons, "At least one product image is required")
		}
		if !sellableSKUsByProduct[item.Product.ID] {
			reasons = append(reasons, "Sellable inventory at an active store location is required")
		}
		view.PublishReadiness = PublishReadiness{IsReady: len(reasons) == 0, Reasons: reasons}

		views = append(views, view)
	}

	return views, total, nil
}
