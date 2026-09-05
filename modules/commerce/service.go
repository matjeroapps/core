package commerce

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type Service struct {
	repo Repository

	CheckoutSessionLifetime   time.Duration
	OrderConfirmationDuration time.Duration

	// PlatformDomain is the base domain under which platform-generated store
	// subdomains are allocated (e.g. "<store-code>.matjero.com"). When empty, no
	// subdomain is auto-allocated at store creation.
	PlatformDomain string
	// ReservedSubdomains are store-code labels that may not be claimed by sellers
	// because they are reserved for platform use.
	ReservedSubdomains []string

	// TXTResolver abstracts DNS TXT lookups for custom domain ownership verification.
	TXTResolver TXTResolver

	// S3Storage handles S3-compatible media uploads and management.
	S3Storage *S3Storage
}

func NewService(repo Repository) Service {
	return Service{
		repo:        repo,
		TXTResolver: DefaultTXTResolver{},
	}
}

func (s Service) CreateSellerListing(ctx context.Context, storeID, productID string, supplierOfferID *string, marketCode, status string) (SellerListing, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return SellerListing{}, err
	}
	if store.MarketCode != marketCode {
		return SellerListing{}, fmt.Errorf("%w: store market %s does not match %s", ErrMarketMismatch, store.MarketCode, marketCode)
	}
	if supplierOfferID != nil {
		offer, err := s.repo.GetSupplierOffer(ctx, *supplierOfferID)
		if err != nil {
			return SellerListing{}, err
		}
		if offer.MarketCode != marketCode {
			return SellerListing{}, fmt.Errorf("%w: supplier offer market %s does not match %s", ErrMarketMismatch, offer.MarketCode, marketCode)
		}
		supplierProduct, err := s.repo.GetSupplierProductByID(ctx, offer.SupplierProductID)
		if err != nil {
			return SellerListing{}, err
		}
		if supplierProduct.ProductID != productID {
			return SellerListing{}, fmt.Errorf("%w: supplier offer product %s does not match listing product %s", ErrInvalidInput, supplierProduct.ProductID, productID)
		}
	} else {
		if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, store.SellerID, productID); err != nil {
			return SellerListing{}, fmt.Errorf("%w: product does not belong to store seller", ErrNotFound)
		}
	}
	return s.repo.CreateSellerListing(ctx, storeID, productID, supplierOfferID, marketCode, status)
}

func (s Service) ReserveInventory(ctx context.Context, snapshotID string, quantity int64, reservationToken string, expiresAt *time.Time) (InventoryReservation, error) {
	if quantity <= 0 {
		return InventoryReservation{}, ErrInvalidInput
	}
	return s.repo.ReserveInventory(ctx, snapshotID, quantity, reservationToken, expiresAt)
}

func (s Service) RequireSupplierAccess(ctx context.Context, subject, supplierID string) (Supplier, error) {
	if supplierID == "" {
		return Supplier{}, ErrInvalidInput
	}
	supplier, err := s.repo.GetSupplierForSubject(ctx, subject)
	if err != nil {
		return Supplier{}, err
	}
	if supplier.ID != supplierID {
		return Supplier{}, ErrNotFound
	}
	return supplier, nil
}

func (s Service) RequireSellerAccess(ctx context.Context, subject, sellerID string) (Seller, error) {
	if sellerID == "" {
		return Seller{}, ErrInvalidInput
	}
	seller, err := s.repo.GetSellerForSubject(ctx, subject)
	if err != nil {
		return Seller{}, err
	}
	if seller.ID != sellerID {
		return Seller{}, ErrNotFound
	}
	return seller, nil
}

func (s Service) RequireSupplierRetailAccess(ctx context.Context, subject, supplierID string) (Seller, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return Seller{}, err
	}
	affiliation, err := s.repo.GetSupplierSellerAffiliationBySupplierID(ctx, supplierID)
	if err != nil {
		return Seller{}, err
	}
	seller, err := s.repo.GetSellerByID(ctx, affiliation.SellerID)
	if err != nil {
		return Seller{}, err
	}
	return seller, nil
}

func (s Service) RequireSupplierRetailStoreAccess(ctx context.Context, subject, supplierID, storeID string) (Store, Seller, error) {
	seller, err := s.RequireSupplierRetailAccess(ctx, subject, supplierID)
	if err != nil {
		return Store{}, Seller{}, err
	}
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return Store{}, Seller{}, err
	}
	if store.SellerID != seller.ID {
		return Store{}, Seller{}, ErrNotFound
	}
	return store, seller, nil
}

func (s Service) RequireSupplierOwnerAccess(ctx context.Context, subject, supplierID string) (Supplier, error) {
	if supplierID == "" || subject == "" {
		return Supplier{}, ErrInvalidInput
	}
	return s.repo.GetSupplierOwnerForSubject(ctx, supplierID, subject)
}

func (s Service) CreateSupplierRetailCapabilityForSubject(ctx context.Context, subject, supplierID string, draft RetailCapabilityDraft) (Seller, SupplierSellerAffiliation, error) {
	if _, err := s.RequireSupplierOwnerAccess(ctx, subject, supplierID); err != nil {
		return Seller{}, SupplierSellerAffiliation{}, err
	}
	return s.repo.CreateSupplierRetailCapabilityForSubject(ctx, subject, supplierID, draft)
}

func (s Service) CreateStoreForSubject(ctx context.Context, subject, sellerID, marketCode, code, name, status string, settings map[string]any) (Store, error) {
	if _, err := s.RequireSellerAccess(ctx, subject, sellerID); err != nil {
		return Store{}, err
	}
	return s.createStoreForSeller(ctx, sellerID, marketCode, code, name, status, settings)
}

func (s Service) CreateSupplierStoreForSubject(ctx context.Context, subject, supplierID, marketCode, code, name, status string, settings map[string]any) (Store, error) {
	seller, err := s.RequireSupplierRetailAccess(ctx, subject, supplierID)
	if err != nil {
		return Store{}, err
	}
	return s.createStoreForSeller(ctx, seller.ID, marketCode, code, name, status, settings)
}

func (s Service) createStoreForSeller(ctx context.Context, sellerID, marketCode, code, name, status string, settings map[string]any) (Store, error) {
	if s.PlatformDomain == "" {
		return s.repo.CreateStore(ctx, sellerID, marketCode, code, name, status, settings)
	}

	normalizedCode := strings.ToLower(strings.TrimSpace(code))
	for _, reserved := range s.ReservedSubdomains {
		if normalizedCode == strings.ToLower(strings.TrimSpace(reserved)) {
			return Store{}, ErrInvalidInput
		}
	}

	subdomain, err := NormalizeDomain(fmt.Sprintf("%s.%s", normalizedCode, s.PlatformDomain))
	if err != nil {
		return Store{}, ErrInvalidInput
	}

	now := time.Now()
	store, _, err := s.repo.CreateStoreWithDomain(ctx, sellerID, marketCode, code, name, status, settings, subdomain, "platform", "active", true, &now, nil)
	if err != nil {
		return Store{}, err
	}
	return store, nil
}

func (s Service) CreateFulfillmentLocationForSubject(ctx context.Context, subject, supplierID, supplierMarketID, marketCode, code, name, locationType, status string) (FulfillmentLocation, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return FulfillmentLocation{}, err
	}
	return s.repo.CreateFulfillmentLocation(ctx, supplierID, supplierMarketID, marketCode, code, name, locationType, status)
}

func (s Service) CreateStoreFulfillmentLocationForSubject(ctx context.Context, subject, storeID, code, name, locationType, status string) (FulfillmentLocation, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return FulfillmentLocation{}, err
	}
	if _, err := s.RequireSellerAccess(ctx, subject, store.SellerID); err != nil {
		return FulfillmentLocation{}, err
	}
	return s.repo.CreateStoreFulfillmentLocation(ctx, storeID, store.MarketCode, code, name, locationType, status)
}

func (s Service) CreateSupplierProductForSubject(ctx context.Context, subject, supplierID, productID, supplierCode, status string) (SupplierProduct, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return SupplierProduct{}, err
	}
	return s.repo.CreateSupplierProduct(ctx, supplierID, productID, supplierCode, status)
}

// CreateSupplierProductWithDetailsForSubject creates a product, its translations,
// the supplier binding and the category assignments as one atomic operation.
//
// Callers that build these rows through separate repository calls can leave a
// product with no supplier binding, or a binding with no categories, when a later
// step fails. This method cannot.
func (s Service) CreateSupplierProductWithDetailsForSubject(ctx context.Context, subject, supplierID string, draft ProductDraft) (Product, SupplierProduct, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return Product{}, SupplierProduct{}, err
	}
	return s.repo.CreateSupplierProductAtomically(ctx, supplierID, draft)
}

func (s Service) CreateSupplierOfferForSubject(ctx context.Context, subject, supplierID, supplierProductID, supplierMarketID, marketCode, status string) (SupplierOffer, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return SupplierOffer{}, err
	}
	product, err := s.repo.GetSupplierProductByID(ctx, supplierProductID)
	if err != nil {
		return SupplierOffer{}, err
	}
	if product.SupplierID != supplierID {
		return SupplierOffer{}, ErrNotFound
	}
	market, err := s.repo.GetSupplierMarketByID(ctx, supplierMarketID)
	if err != nil {
		return SupplierOffer{}, err
	}
	if market.SupplierID != supplierID || market.MarketCode != marketCode {
		return SupplierOffer{}, ErrMarketMismatch
	}
	return s.repo.CreateSupplierOffer(ctx, supplierID, supplierProductID, supplierMarketID, marketCode, status)
}

// CreateSupplierOfferWithDetailsForSubject creates an offer together with its
// price and availability as one atomic operation, so an offer is never left
// unpriced because a later step failed.
func (s Service) CreateSupplierOfferWithDetailsForSubject(ctx context.Context, subject, supplierID string, draft OfferDraft) (SupplierOffer, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return SupplierOffer{}, err
	}
	product, err := s.repo.GetSupplierProductByID(ctx, draft.SupplierProductID)
	if err != nil {
		return SupplierOffer{}, err
	}
	if product.SupplierID != supplierID {
		return SupplierOffer{}, ErrNotFound
	}
	market, err := s.repo.GetSupplierMarketByID(ctx, draft.SupplierMarketID)
	if err != nil {
		return SupplierOffer{}, err
	}
	if market.SupplierID != supplierID || market.MarketCode != draft.MarketCode {
		return SupplierOffer{}, ErrMarketMismatch
	}
	return s.repo.CreateSupplierOfferAtomically(ctx, supplierID, draft)
}

func (s Service) CreateSellerListingForSubject(ctx context.Context, subject, storeID, productID string, supplierOfferID *string, marketCode, status string) (SellerListing, error) {
	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return SellerListing{}, err
	}
	seller, err := s.RequireSellerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return SellerListing{}, err
	}
	if supplierOfferID == nil {
		if _, err := s.repo.GetSellerProductBySellerAndProduct(ctx, seller.ID, productID); err != nil {
			return SellerListing{}, ErrNotFound
		}
	}
	return s.CreateSellerListing(ctx, storeID, productID, supplierOfferID, marketCode, status)
}

func (s Service) SetProductCategoriesForSubject(ctx context.Context, subject, supplierID, productID string, categoryIDs []string) error {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return err
	}
	product, err := s.repo.GetSupplierProductBySupplierAndProduct(ctx, supplierID, productID)
	if err != nil {
		return err
	}
	if product.SupplierID != supplierID {
		return ErrNotFound
	}
	return s.repo.SetProductCategories(ctx, productID, categoryIDs)
}

func (s Service) AdjustInventoryForSubject(ctx context.Context, subject, supplierID, snapshotID string, quantityDelta int64, movementType, reason, correlationID, causationID string) (InventorySnapshot, InventoryMovement, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return InventorySnapshot{}, InventoryMovement{}, err
	}
	snapshot, err := s.repo.GetInventorySnapshot(ctx, snapshotID)
	if err != nil {
		return InventorySnapshot{}, InventoryMovement{}, err
	}
	location, err := s.repo.GetFulfillmentLocationByID(ctx, snapshot.FulfillmentLocationID)
	if err != nil {
		return InventorySnapshot{}, InventoryMovement{}, err
	}
	if location.SupplierID != supplierID {
		return InventorySnapshot{}, InventoryMovement{}, ErrNotFound
	}
	return s.repo.AdjustInventory(ctx, snapshotID, quantityDelta, movementType, reason, subject, correlationID, causationID)
}

func (s Service) UpdateSupplierStatusForSubject(ctx context.Context, subject, supplierID, status string) error {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return err
	}
	return s.repo.UpdateSupplierStatus(ctx, supplierID, status)
}

func (s Service) UpdateSellerStatusForSubject(ctx context.Context, subject, sellerID, status string) error {
	if _, err := s.RequireSellerAccess(ctx, subject, sellerID); err != nil {
		return err
	}
	return s.repo.UpdateSellerStatus(ctx, sellerID, status)
}

func (s Service) ResolveSupplierIDForSubject(ctx context.Context, subject string) (string, error) {
	supplier, err := s.repo.GetSupplierForSubject(ctx, subject)
	if err != nil {
		return "", err
	}
	return supplier.ID, nil
}

func (s Service) ResolveSellerIDForSubject(ctx context.Context, subject string) (string, error) {
	seller, err := s.repo.GetSellerForSubject(ctx, subject)
	if err != nil {
		return "", err
	}
	return seller.ID, nil
}

func (s Service) ConfirmOrder(ctx context.Context, storeID, orderID string, actorSubject *string, correlationID string) (Order, error) {
	return s.repo.ConfirmOrder(ctx, nil, storeID, orderID, actorSubject, correlationID)
}

func (s Service) CancelPendingOrder(ctx context.Context, storeID, orderID string, authority TransitionAuthority, actorSubject *string, reason *string, correlationID string) (Order, error) {
	return s.repo.CancelPendingOrder(ctx, nil, storeID, orderID, authority, actorSubject, reason, correlationID)
}

func (s Service) CancelConfirmedOrder(ctx context.Context, storeID, orderID string, authority TransitionAuthority, actorSubject *string, reason *string, correlationID string) (Order, error) {
	return s.repo.CancelConfirmedOrder(ctx, nil, storeID, orderID, authority, actorSubject, reason, correlationID)
}

func (s Service) AdvanceOrderStatus(ctx context.Context, storeID, orderID string, targetStatus string, authority TransitionAuthority, actorSubject *string, reason *string, correlationID string) (Order, error) {
	return s.repo.AdvanceOrderStatus(ctx, nil, storeID, orderID, targetStatus, authority, actorSubject, reason, correlationID)
}

func (s Service) DiscoverExpiryCandidates(ctx context.Context, limit int) ([]string, error) {
	return s.repo.DiscoverExpiryCandidates(ctx, nil, limit)
}

func (s Service) ExpirePendingOrder(ctx context.Context, orderID string) (Order, error) {
	return s.repo.ExpirePendingOrder(ctx, nil, orderID)
}
