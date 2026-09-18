package commerce

import (
	"testing"
	"time"

	"core/packages/money"
)

func TestProductSearchPayloadAndEvent(t *testing.T) {
	product := Product{
		ID:        "product-1",
		Slug:      "espresso-machine",
		Status:    "active",
		UpdatedAt: time.Now().UTC(),
	}

	payload := product.SearchPayload(
		[]SearchTranslation{
			{Locale: "ar", Name: "ماكينة إسبرسو"},
			{Locale: "en", Name: "Espresso Machine"},
		},
		[]string{"category-1"},
		[]string{"attribute-1"},
	)
	if payload.ID != product.ID {
		t.Fatalf("payload.ID = %q", payload.ID)
	}
	if len(payload.Translations) != 2 {
		t.Fatalf("payload.Translations = %d", len(payload.Translations))
	}
	if len(payload.CategoryIDs) != 1 || payload.CategoryIDs[0] != "category-1" {
		t.Fatalf("payload.CategoryIDs = %#v", payload.CategoryIDs)
	}

	event, err := product.UpsertedEvent(payload.Translations, payload.CategoryIDs, payload.AttributeIDs, 5, "corr", "cause")
	if err != nil {
		t.Fatalf("UpsertedEvent returned error: %v", err)
	}
	if event.AggregateType != "product" {
		t.Fatalf("AggregateType = %q", event.AggregateType)
	}
}

func TestSupplierOfferAndListingSearchPayloads(t *testing.T) {
	offer := SupplierOffer{
		ID:                "offer-1",
		SupplierID:        "supplier-1",
		SupplierProductID: "supplier-product-1",
		SupplierMarketID:  "supplier-market-1",
		MarketCode:        "EG",
		Status:            "active",
		UpdatedAt:         time.Now().UTC(),
	}
	price := money.MustNew(1250, "EGP")
	available := true
	availableQty := int64(9)
	offerPayload := offer.SearchPayload("product-1", "SUP-1", &price, &available, &availableQty)
	if offerPayload.MarketCode != "EG" {
		t.Fatalf("offer payload market code = %q", offerPayload.MarketCode)
	}
	if offerPayload.Price == nil || offerPayload.Price.AmountMinor != 1250 {
		t.Fatalf("offer payload price = %#v", offerPayload.Price)
	}

	listing := SellerListing{
		ID:         "listing-1",
		StoreID:    "store-1",
		ProductID:  "product-1",
		MarketCode: "EG",
		Status:     "active",
		UpdatedAt:  time.Now().UTC(),
	}
	listingPayload := listing.SearchPayload(&price, &available)
	if listingPayload.MarketCode != "EG" {
		t.Fatalf("listing payload market code = %q", listingPayload.MarketCode)
	}
	if listingPayload.Price == nil || listingPayload.Price.AmountMinor != 1250 {
		t.Fatalf("listing payload price = %#v", listingPayload.Price)
	}
}
