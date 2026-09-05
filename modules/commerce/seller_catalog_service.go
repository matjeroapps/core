package commerce

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
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
					FulfillmentLocationID: snap.FulfillmentLocationID,
					LocationName:          locNameMap[snap.FulfillmentLocationID],
					SKUID:                 snap.SKUID,
					OnHandQty:             snap.OnHandQty,
					ReservedQty:           snap.ReservedQty,
					AvailableQty:          avail,
				})
			}
		}
	}

	readiness := s.evaluateReadinessInternal(store, prod, translations, variants, skus, media, listing, price, presentation, inventorySummary)

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
	inventorySummary []SellerInventorySummary,
) PublishReadiness {
	var reasons []string

	// 1. Translations / English or Arabic name exists
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

	// 2. Active Variant
	hasActiveVariant := false
	for _, v := range variants {
		if v.Status == "active" {
			hasActiveVariant = true
			break
		}
	}
	if !hasActiveVariant {
		reasons = append(reasons, "At least one active Variant is required")
	}

	// 3. Active SKU
	hasActiveSKU := false
	for _, sku := range skus {
		if sku.Status == "active" {
			hasActiveSKU = true
			break
		}
	}
	if !hasActiveSKU {
		reasons = append(reasons, "At least one active selectable SKU is required")
	}

	// 4. Current Listing price exists & matches Store market currency
	if price == nil || !price.IsCurrent {
		reasons = append(reasons, "Current retail price is missing")
	} else if price.Price.Currency != store.MarketCode && !isMarketCurrencyMatch(price.Price.Currency, store.MarketCode) {
		reasons = append(reasons, fmt.Sprintf("Price currency %s does not match store market currency %s", price.Price.Currency, store.MarketCode))
	}

	// 5. At least one image exists
	if len(media) == 0 {
		reasons = append(reasons, "At least one product image is required")
	}

	// 6. Listing belongs to store
	if listing.StoreID != store.ID {
		reasons = append(reasons, "Listing does not belong to store")
	}

	// 7. Inventory topology exists
	if len(inventorySummary) == 0 {
		reasons = append(reasons, "Inventory location and snapshot topology required")
	}

	return PublishReadiness{
		IsReady: len(reasons) == 0,
		Reasons: reasons,
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

	// MVP Invariant: keep 1 active selectable SKU per variant
	if status == "active" {
		existingSKUs, _ := s.repo.ListSKUsByVariantID(ctx, variantID)
		for _, sk := range existingSKUs {
			if sk.Status == "active" {
				// Deactivate previous active SKU to maintain 1 active SKU invariant
				_, _ = s.repo.UpdateSKU(ctx, sk.ID, sk.Code, sk.Barcode, "inactive")
			}
		}
	}

	return s.repo.CreateSKU(ctx, variantID, code, barcode, status)
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

	if status == "active" {
		existingSKUs, _ := s.repo.ListSKUsByVariantID(ctx, variantID)
		for _, sk := range existingSKUs {
			if sk.ID != skuID && sk.Status == "active" {
				_, _ = s.repo.UpdateSKU(ctx, sk.ID, sk.Code, sk.Barcode, "inactive")
			}
		}
	}

	return s.repo.UpdateSKU(ctx, skuID, code, barcode, status)
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

	if req.SizeBytes > 10*1024*1024 {
		return MediaUploadResponse{}, fmt.Errorf("%w: file size exceeds 10MB limit", ErrInvalidInput)
	}

	randomID := uuid.NewString()
	storageKey := fmt.Sprintf("products/%s/%s/%s%s", seller.ID, productID, randomID, ext)

	if s.S3Storage == nil {
		// Mock presign fallback for test environments without S3 config
		mockURL := fmt.Sprintf("http://localhost:9000/media-bucket/%s", storageKey)
		return MediaUploadResponse{
			UploadURL:   mockURL,
			StorageKey:  storageKey,
			UploadToken: randomID,
			ExpiresAt:   time.Now().Add(15 * time.Minute),
		}, nil
	}

	uploadURL, err := s.S3Storage.PresignPutObject(ctx, storageKey, req.ContentType)
	if err != nil {
		return MediaUploadResponse{}, err
	}

	return MediaUploadResponse{
		UploadURL:   uploadURL,
		StorageKey:  storageKey,
		UploadToken: randomID,
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

	expectedPrefix := fmt.Sprintf("products/%s/%s/", seller.ID, productID)
	if !strings.HasPrefix(req.StorageKey, expectedPrefix) {
		return MediaMetadata{}, fmt.Errorf("%w: invalid storage key prefix", ErrInvalidInput)
	}

	var publicURI string
	if s.S3Storage != nil {
		if _, err := s.S3Storage.HeadObject(ctx, req.StorageKey); err != nil {
			return MediaMetadata{}, fmt.Errorf("%w: uploaded object does not exist in storage: %v", ErrInvalidInput, err)
		}
		publicURI = s.S3Storage.ResolvePublicURI(req.StorageKey)
	} else {
		publicURI = fmt.Sprintf("http://localhost:9000/media-bucket/%s", req.StorageKey)
	}

	ext := strings.ToLower(filepath.Ext(req.StorageKey))
	mediaType := "image/jpeg"
	if ext == ".png" {
		mediaType = "image/png"
	} else if ext == ".webp" {
		mediaType = "image/webp"
	}

	m := MediaMetadata{
		ProductID:  productID,
		MediaType:  mediaType,
		URI:        publicURI,
		AltText:    req.AltText,
		SortOrder:  req.SortOrder,
		StorageKey: &req.StorageKey,
		IsPrimary:  req.IsPrimary,
	}

	return s.repo.CreateMediaMetadata(ctx, m)
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

	if m.StorageKey != nil && *m.StorageKey != "" && s.S3Storage != nil {
		_ = s.S3Storage.DeleteObject(ctx, *m.StorageKey)
	}

	return s.repo.DeleteMediaMetadata(ctx, productID, mediaID)
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

	// Validate sections
	if len(pres.Sections) > 20 {
		return SellerListingPresentation{}, fmt.Errorf("%w: maximum 20 sections allowed", ErrInvalidInput)
	}

	allowedSectionTypes := map[string]bool{
		"description":    true,
		"highlights":     true,
		"image_text":     true,
		"specifications": true,
		"faq":            true,
		"final_cta":      true,
	}

	for _, sec := range pres.Sections {
		if !allowedSectionTypes[sec.Type] {
			return SellerListingPresentation{}, fmt.Errorf("%w: invalid section type %s", ErrInvalidInput, sec.Type)
		}
		// Security check: reject script tags or raw injected HTML
		if jsonBytes, err := json.Marshal(sec.Content); err == nil {
			if scriptTagRegex.Match(jsonBytes) {
				return SellerListingPresentation{}, fmt.Errorf("%w: section content contains disallowed html/script tags", ErrInvalidInput)
			}
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
			FulfillmentLocationID: snap.FulfillmentLocationID,
			LocationName:          locNameMap[snap.FulfillmentLocationID],
			SKUID:                 snap.SKUID,
			OnHandQty:             snap.OnHandQty,
			ReservedQty:           snap.ReservedQty,
			AvailableQty:          avail,
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

	return s.repo.PublishSellerProduct(ctx, storeID, productID)
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

func (s Service) GetStoreOrderForSubject(ctx context.Context, subject, storeID, orderID string) (Order, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return Order{}, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return Order{}, err
	}

	order, err := s.repo.GetOrderByID(ctx, nil, storeID, orderID)
	if err != nil {
		return Order{}, err
	}
	return order, nil
}

func (s Service) TransitionStoreOrderForSubject(ctx context.Context, subject, storeID, orderID, targetStatus string, reason *string, correlationID string) (Order, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return Order{}, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return Order{}, err
	}

	order, err := s.repo.GetOrderByID(ctx, nil, storeID, orderID)
	if err != nil || order.StoreID != storeID {
		return Order{}, ErrNotFound
	}

	// Dispatch to existing lifecycle primitives
	switch targetStatus {
	case "confirmed":
		if order.Status != "pending" {
			return Order{}, fmt.Errorf("%w: cannot confirm order in status %s", ErrInvalidTransition, order.Status)
		}
		return s.ConfirmOrder(ctx, storeID, orderID, &subject, correlationID)

	case "cancelled":
		if order.Status == "pending" {
			return s.CancelPendingOrder(ctx, storeID, orderID, AuthoritySeller, &subject, reason, correlationID)
		} else if order.Status == "confirmed" || order.Status == "processing" {
			return s.CancelConfirmedOrder(ctx, storeID, orderID, AuthoritySeller, &subject, reason, correlationID)
		} else {
			return Order{}, fmt.Errorf("%w: cannot cancel order in status %s", ErrInvalidTransition, order.Status)
		}

	case "processing":
		if order.Status != "confirmed" {
			return Order{}, fmt.Errorf("%w: cannot transition to processing from status %s", ErrInvalidTransition, order.Status)
		}
		return s.AdvanceOrderStatus(ctx, storeID, orderID, "processing", AuthoritySeller, &subject, reason, correlationID)

	case "ready_for_shipping":
		if order.Status != "processing" {
			return Order{}, fmt.Errorf("%w: cannot transition to ready_for_shipping from status %s", ErrInvalidTransition, order.Status)
		}
		return s.AdvanceOrderStatus(ctx, storeID, orderID, "ready_for_shipping", AuthoritySeller, &subject, reason, correlationID)

	case "shipped", "delivered":
		return Order{}, fmt.Errorf("%w: shipping transitions are deferred beyond P5.8", ErrInvalidTransition)

	default:
		return Order{}, fmt.Errorf("%w: unknown target status %s", ErrInvalidTransition, targetStatus)
	}
}

func isMarketCurrencyMatch(currency, marketCode string) bool {
	if currency == marketCode {
		return true
	}
	marketCurrencies := map[string]string{
		"EG": "EGP",
		"SA": "SAR",
		"US": "USD",
		"AE": "AED",
		"KW": "KWD",
	}
	return marketCurrencies[marketCode] == currency
}
