package commerce

import (
	"testing"
	"time"

	"core/packages/money"
)

func TestNewProductUpsertedEvent(t *testing.T) {
	updatedAt := time.Now().UTC()
	envelope, err := NewProductUpsertedEvent(ProductSearchPayload{
		ID:     "product-1",
		Slug:   "espresso-machine",
		Status: "active",
		Translations: []SearchTranslation{
			{Locale: "ar", Name: "ماكينة إسبرسو", Description: "ماكينة تحضير القهوة"},
			{Locale: "en", Name: "Espresso Machine", Description: "Coffee machine"},
		},
		UpdatedAt: updatedAt,
	}, 7, "corr-1", "cause-1")
	if err != nil {
		t.Fatalf("NewProductUpsertedEvent returned error: %v", err)
	}

	if envelope.EventType != EventTypeProductUpserted {
		t.Fatalf("EventType = %q", envelope.EventType)
	}
	if envelope.AggregateType != "product" {
		t.Fatalf("AggregateType = %q", envelope.AggregateType)
	}
	if envelope.AggregateID != "product-1" {
		t.Fatalf("AggregateID = %q", envelope.AggregateID)
	}
	if envelope.AggregateVersion != 7 {
		t.Fatalf("AggregateVersion = %d", envelope.AggregateVersion)
	}
	if envelope.Payload["slug"] != "espresso-machine" {
		t.Fatalf("slug payload = %#v", envelope.Payload["slug"])
	}
	if envelope.Payload["updated_at"] == nil {
		t.Fatal("expected updated_at payload")
	}
}

func TestNewSellerListingUpsertedEventCarriesSearchFields(t *testing.T) {
	price := money.MustNew(1550, "EGP")
	isAvailable := true
	envelope, err := NewSellerListingUpsertedEvent(SellerListingSearchPayload{
		ID:          "listing-1",
		StoreID:     "store-1",
		ProductID:   "product-1",
		MarketCode:  "EG",
		Status:      "active",
		Price:       &price,
		IsAvailable: &isAvailable,
		UpdatedAt:   time.Now().UTC(),
	}, 3, "", "")
	if err != nil {
		t.Fatalf("NewSellerListingUpsertedEvent returned error: %v", err)
	}

	if envelope.AggregateType != "seller_listing" {
		t.Fatalf("AggregateType = %q", envelope.AggregateType)
	}
	if envelope.Payload["market_code"] != "EG" {
		t.Fatalf("market_code payload = %#v", envelope.Payload["market_code"])
	}
	if envelope.Payload["status"] != "active" {
		t.Fatalf("status payload = %#v", envelope.Payload["status"])
	}
	if envelope.Payload["price"] == nil {
		t.Fatal("expected price payload")
	}
}
