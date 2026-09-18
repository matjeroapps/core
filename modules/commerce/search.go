package commerce

import (
	"core/packages/events"
	"core/packages/money"
)

func (s Supplier) SearchPayload(marketCodes []string, translations []SearchTranslation) SupplierSearchPayload {
	return SupplierSearchPayload{
		ID:           s.ID,
		Code:         s.Code,
		Name:         s.Name,
		Status:       s.Status,
		MarketCodes:  append([]string(nil), marketCodes...),
		Translations: append([]SearchTranslation(nil), translations...),
		UpdatedAt:    s.UpdatedAt,
	}
}

func (s Supplier) UpsertedEvent(marketCodes []string, translations []SearchTranslation, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return NewSupplierUpsertedEvent(s.SearchPayload(marketCodes, translations), aggregateVersion, correlationID, causationID)
}

func (s Seller) SearchPayload() SellerSearchPayload {
	return SellerSearchPayload{
		ID:        s.ID,
		Code:      s.Code,
		Name:      s.Name,
		Status:    s.Status,
		UpdatedAt: s.UpdatedAt,
	}
}

func (s Seller) UpsertedEvent(aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return NewSellerUpsertedEvent(s.SearchPayload(), aggregateVersion, correlationID, causationID)
}

func (s Store) SearchPayload(domains []string, translations []SearchTranslation) StoreSearchPayload {
	return StoreSearchPayload{
		ID:           s.ID,
		SellerID:     s.SellerID,
		MarketCode:   s.MarketCode,
		Code:         s.Code,
		Name:         s.Name,
		Status:       s.Status,
		Domains:      append([]string(nil), domains...),
		Translations: append([]SearchTranslation(nil), translations...),
		UpdatedAt:    s.UpdatedAt,
	}
}

func (s Store) UpsertedEvent(domains []string, translations []SearchTranslation, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return NewStoreUpsertedEvent(s.SearchPayload(domains, translations), aggregateVersion, correlationID, causationID)
}

func (p Product) SearchPayload(translations []SearchTranslation, categoryIDs, attributeIDs []string) ProductSearchPayload {
	return ProductSearchPayload{
		ID:           p.ID,
		Slug:         p.Slug,
		Status:       p.Status,
		CategoryIDs:  append([]string(nil), categoryIDs...),
		AttributeIDs: append([]string(nil), attributeIDs...),
		Translations: append([]SearchTranslation(nil), translations...),
		UpdatedAt:    p.UpdatedAt,
	}
}

func (p Product) UpsertedEvent(translations []SearchTranslation, categoryIDs, attributeIDs []string, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return NewProductUpsertedEvent(p.SearchPayload(translations, categoryIDs, attributeIDs), aggregateVersion, correlationID, causationID)
}

func (c Category) SearchPayload(translations []SearchTranslation) CategorySearchPayload {
	return CategorySearchPayload{
		ID:               c.ID,
		ParentCategoryID: c.ParentCategoryID,
		Slug:             c.Slug,
		Status:           c.Status,
		Translations:     append([]SearchTranslation(nil), translations...),
		UpdatedAt:        c.UpdatedAt,
	}
}

func (c Category) UpsertedEvent(translations []SearchTranslation, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return NewCategoryUpsertedEvent(c.SearchPayload(translations), aggregateVersion, correlationID, causationID)
}

func (a Attribute) SearchPayload(translations []SearchTranslation) AttributeSearchPayload {
	return AttributeSearchPayload{
		ID:           a.ID,
		Code:         a.Code,
		Status:       a.Status,
		Translations: append([]SearchTranslation(nil), translations...),
		UpdatedAt:    a.UpdatedAt,
	}
}

func (a Attribute) UpsertedEvent(translations []SearchTranslation, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return NewAttributeUpsertedEvent(a.SearchPayload(translations), aggregateVersion, correlationID, causationID)
}

func (v Variant) SearchPayload(skuIDs []string) VariantSearchPayload {
	return VariantSearchPayload{
		ID:        v.ID,
		ProductID: v.ProductID,
		Code:      v.Code,
		Status:    v.Status,
		SKUIDs:    append([]string(nil), skuIDs...),
		UpdatedAt: v.UpdatedAt,
	}
}

func (v Variant) UpsertedEvent(skuIDs []string, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return NewVariantUpsertedEvent(v.SearchPayload(skuIDs), aggregateVersion, correlationID, causationID)
}

func (s SKU) SearchPayload() SKUSearchPayload {
	return SKUSearchPayload{
		ID:        s.ID,
		VariantID: s.VariantID,
		Code:      s.Code,
		Barcode:   s.Barcode,
		Status:    s.Status,
		UpdatedAt: s.UpdatedAt,
	}
}

func (s SKU) UpsertedEvent(aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return NewSKUUpsertedEvent(s.SearchPayload(), aggregateVersion, correlationID, causationID)
}

func (s SupplierOffer) SearchPayload(productID string, supplierCode string, price *money.Money, isAvailable *bool, availableQty *int64) SupplierOfferSearchPayload {
	return SupplierOfferSearchPayload{
		ID:                s.ID,
		SupplierID:        s.SupplierID,
		SupplierProductID: s.SupplierProductID,
		SupplierMarketID:  s.SupplierMarketID,
		MarketCode:        s.MarketCode,
		ProductID:         productID,
		SupplierCode:      supplierCode,
		Status:            s.Status,
		Price:             price,
		IsAvailable:       isAvailable,
		AvailableQty:      availableQty,
		UpdatedAt:         s.UpdatedAt,
	}
}

func (s SupplierOffer) UpsertedEvent(productID string, supplierCode string, price *money.Money, isAvailable *bool, availableQty *int64, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return NewSupplierOfferUpsertedEvent(s.SearchPayload(productID, supplierCode, price, isAvailable, availableQty), aggregateVersion, correlationID, causationID)
}

func (l SellerListing) SearchPayload(price *money.Money, isAvailable *bool) SellerListingSearchPayload {
	return SellerListingSearchPayload{
		ID:              l.ID,
		StoreID:         l.StoreID,
		ProductID:       l.ProductID,
		SupplierOfferID: l.SupplierOfferID,
		MarketCode:      l.MarketCode,
		Status:          l.Status,
		Price:           price,
		IsAvailable:     isAvailable,
		UpdatedAt:       l.UpdatedAt,
	}
}

func (l SellerListing) UpsertedEvent(price *money.Money, isAvailable *bool, aggregateVersion int64, correlationID, causationID string) (events.EventEnvelope, error) {
	return NewSellerListingUpsertedEvent(l.SearchPayload(price, isAvailable), aggregateVersion, correlationID, causationID)
}
