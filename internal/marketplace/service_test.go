package marketplace

import (
	"context"
	"testing"

	"core/packages/i18n"
)

type fakeRepository struct {
	gotRequest PageRequest
	gotCursor  *Cursor
	result     Collection
	err        error
}

func (f *fakeRepository) ListCollection(_ context.Context, _ CollectionType, request PageRequest, cursor *Cursor) (Collection, error) {
	f.gotRequest = request
	f.gotCursor = cursor
	return f.result, f.err
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
