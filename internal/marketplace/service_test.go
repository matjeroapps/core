package marketplace

import (
	"context"
	"testing"

	"core/packages/i18n"
)

type fakeRepository struct {
	gotRequest       PageRequest
	gotCursor        *Cursor
	result           Collection
	gotResolveParams ResolveListingParams
	resolveResult    ResolvedListing
	err              error
}

func (f *fakeRepository) ListCollection(_ context.Context, _ CollectionType, request PageRequest, cursor *Cursor) (Collection, error) {
	f.gotRequest = request
	f.gotCursor = cursor
	return f.result, f.err
}

func (f *fakeRepository) ResolveListing(_ context.Context, params ResolveListingParams) (ResolvedListing, error) {
	f.gotResolveParams = params
	return f.resolveResult, f.err
}

func TestServiceReturnsEmptyCollection(t *testing.T) {
	repo := &fakeRepository{result: Collection{Items: []Item{}}}
	svc := NewService(repo)

	got, err := svc.GetCollection(context.Background(), PageRequest{MarketCode: "eg"}, CollectionNewProducts)
	if err != nil {
		t.Fatalf("GetCollection returned error: %v", err)
	}
	if got.Items == nil {
		t.Fatal("empty collection items must be an empty slice")
	}
	if len(got.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(got.Items))
	}
	if repo.gotRequest.Locale != i18n.LocaleEnglish {
		t.Fatalf("default locale = %q, want %q", repo.gotRequest.Locale, i18n.LocaleEnglish)
	}
	if repo.gotRequest.Limit != DefaultLimit {
		t.Fatalf("default limit = %d, want %d", repo.gotRequest.Limit, DefaultLimit)
	}
}

func TestServiceRejectsInvalidCollectionType(t *testing.T) {
	_, err := NewService(&fakeRepository{}).GetCollection(
		context.Background(),
		PageRequest{MarketCode: "EG"},
		CollectionType("not-a-collection"),
	)
	if err == nil || !containsError(err, ErrInvalidCollectionType) {
		t.Fatalf("error = %v, want ErrInvalidCollectionType", err)
	}
}

func TestServiceDefersUnsupportedCollections(t *testing.T) {
	for _, collectionType := range []CollectionType{CollectionOffers, CollectionTrending} {
		_, err := NewService(&fakeRepository{}).GetCollection(
			context.Background(),
			PageRequest{MarketCode: "EG"},
			collectionType,
		)
		if err == nil || !containsError(err, ErrCollectionUnavailable) {
			t.Fatalf("%s error = %v, want ErrCollectionUnavailable", collectionType, err)
		}
	}
}

func TestServicePassesArabicLocaleAndCursor(t *testing.T) {
	repo := &fakeRepository{result: Collection{Items: []Item{}}}
	svc := NewService(repo)

	first, err := encodeCursor(Cursor{SellerListingID: "listing-1"})
	if err != nil {
		t.Fatalf("encode cursor: %v", err)
	}
	if _, err := svc.GetCollection(context.Background(), PageRequest{
		MarketCode: "EG",
		Locale:     i18n.LocaleArabic,
		Limit:      7,
		Cursor:     first,
	}, CollectionBestSellers); err != nil {
		t.Fatalf("GetCollection returned error: %v", err)
	}
	if repo.gotRequest.Locale != i18n.LocaleArabic {
		t.Fatalf("locale = %q, want ar", repo.gotRequest.Locale)
	}
	if repo.gotCursor == nil || repo.gotCursor.SellerListingID != "listing-1" {
		t.Fatalf("cursor = %+v, want listing-1", repo.gotCursor)
	}
}

func TestServiceRejectsMalformedCursor(t *testing.T) {
	_, err := NewService(&fakeRepository{}).GetCollection(
		context.Background(),
		PageRequest{MarketCode: "EG", Cursor: "not-base64"},
		CollectionNewProducts,
	)
	if err == nil || !containsError(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestServiceResolveListingValidParams(t *testing.T) {
	repo := &fakeRepository{
		resolveResult: ResolvedListing{
			SellerListingID: "listing-1",
			StoreID:         "store-1",
			MarketCode:      "EG",
			SKUID:           "sku-1",
			Quantity:        2,
		},
	}
	svc := NewService(repo)

	got, err := svc.ResolveListing(context.Background(), ResolveListingParams{
		MarketCode:       "eg",
		SellerListingID:  "listing-1",
		Quantity:         2,
		SourceCollection: CollectionFastDelivery,
	})
	if err != nil {
		t.Fatalf("ResolveListing failed: %v", err)
	}
	if got.SellerListingID != "listing-1" {
		t.Fatalf("got listing id = %q, want listing-1", got.SellerListingID)
	}
	if repo.gotResolveParams.MarketCode != "EG" {
		t.Fatalf("normalized market = %q, want EG", repo.gotResolveParams.MarketCode)
	}
	if repo.gotResolveParams.Quantity != 2 {
		t.Fatalf("quantity = %d, want 2", repo.gotResolveParams.Quantity)
	}
	if repo.gotResolveParams.Locale != i18n.Default() {
		t.Fatalf("default locale = %q, want %q", repo.gotResolveParams.Locale, i18n.Default())
	}
}

func TestServiceResolveListingDefaultQuantity(t *testing.T) {
	repo := &fakeRepository{
		resolveResult: ResolvedListing{
			SellerListingID: "listing-1",
			Quantity:        1,
		},
	}
	svc := NewService(repo)

	_, err := svc.ResolveListing(context.Background(), ResolveListingParams{
		MarketCode:      "EG",
		SellerListingID: "listing-1",
	})
	if err != nil {
		t.Fatalf("ResolveListing failed: %v", err)
	}
	if repo.gotResolveParams.Quantity != 1 {
		t.Fatalf("default quantity = %d, want 1", repo.gotResolveParams.Quantity)
	}
}

func TestServiceResolveListingRejectsInvalidQuantity(t *testing.T) {
	svc := NewService(&fakeRepository{})

	for _, qty := range []int64{-1, -100, 10001, 50000} {
		_, err := svc.ResolveListing(context.Background(), ResolveListingParams{
			MarketCode:      "EG",
			SellerListingID: "listing-1",
			Quantity:        qty,
		})
		if err == nil || !containsError(err, ErrQuantityInvalid) {
			t.Fatalf("qty %d: error = %v, want ErrQuantityInvalid", qty, err)
		}
	}
}

func TestServiceResolveListingRejectsInvalidMarketCode(t *testing.T) {
	svc := NewService(&fakeRepository{})

	for _, market := range []string{"", "E", "EGP", "123"} {
		_, err := svc.ResolveListing(context.Background(), ResolveListingParams{
			MarketCode:      market,
			SellerListingID: "listing-1",
		})
		if err == nil || !containsError(err, ErrInvalidInput) {
			t.Fatalf("market %q: error = %v, want ErrInvalidInput", market, err)
		}
	}
}

func TestServiceResolveListingRejectsEmptyListingID(t *testing.T) {
	svc := NewService(&fakeRepository{})

	_, err := svc.ResolveListing(context.Background(), ResolveListingParams{
		MarketCode:      "EG",
		SellerListingID: "   ",
	})
	if err == nil || !containsError(err, ErrInvalidInput) {
		t.Fatalf("error = %v, want ErrInvalidInput", err)
	}
}

func TestServiceResolveListingRejectsInvalidSourceCollection(t *testing.T) {
	svc := NewService(&fakeRepository{})

	_, err := svc.ResolveListing(context.Background(), ResolveListingParams{
		MarketCode:       "EG",
		SellerListingID:  "listing-1",
		SourceCollection: CollectionType("invalid-collection"),
	})
	if err == nil || !containsError(err, ErrInvalidCollectionType) {
		t.Fatalf("error = %v, want ErrInvalidCollectionType", err)
	}
}

func containsError(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}
