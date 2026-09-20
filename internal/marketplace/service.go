package marketplace

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"core/packages/i18n"
)

const (
	DefaultLimit = 20
	MaxLimit     = 100
)

type RepositoryReader interface {
	ListCollection(ctx context.Context, collectionType CollectionType, request PageRequest, cursor *Cursor) (Collection, error)
	ResolveListing(ctx context.Context, params ResolveListingParams) (ResolvedListing, error)
}

type Service interface {
	GetCollection(ctx context.Context, request PageRequest, collectionType CollectionType) (Collection, error)
	ResolveListing(ctx context.Context, params ResolveListingParams) (ResolvedListing, error)
}

type service struct {
	repository RepositoryReader
}

func NewService(repository RepositoryReader) Service {
	return service{repository: repository}
}

func (s service) GetCollection(ctx context.Context, request PageRequest, collectionType CollectionType) (Collection, error) {
	if err := validateCollectionType(collectionType); err != nil {
		return Collection{}, err
	}
	request, err := normalizeRequest(request)
	if err != nil {
		return Collection{}, err
	}
	cursor, err := decodeCursor(request.Cursor)
	if err != nil {
		return Collection{}, err
	}
	return s.repository.ListCollection(ctx, collectionType, request, cursor)
}

func (s service) ResolveListing(ctx context.Context, params ResolveListingParams) (ResolvedListing, error) {
	params, err := normalizeResolveParams(params)
	if err != nil {
		return ResolvedListing{}, err
	}
	return s.repository.ResolveListing(ctx, params)
}

func validateCollectionType(collectionType CollectionType) error {
	if !collectionType.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidCollectionType, collectionType)
	}
	if !collectionType.Implemented() {
		return ErrCollectionUnavailable
	}
	return nil
}

func normalizeRequest(request PageRequest) (PageRequest, error) {
	request.MarketCode = strings.ToUpper(strings.TrimSpace(request.MarketCode))
	if len(request.MarketCode) != 2 {
		return PageRequest{}, fmt.Errorf("%w: market code", ErrInvalidInput)
	}
	if request.Locale == "" {
		request.Locale = i18n.Default()
	}
	if request.Locale != i18n.LocaleArabic && request.Locale != i18n.LocaleEnglish {
		return PageRequest{}, fmt.Errorf("%w: locale", ErrInvalidInput)
	}
	if request.Limit == 0 {
		request.Limit = DefaultLimit
	}
	if request.Limit < 1 || request.Limit > MaxLimit {
		return PageRequest{}, fmt.Errorf("%w: limit", ErrInvalidInput)
	}
	return request, nil
}

func normalizeResolveParams(params ResolveListingParams) (ResolveListingParams, error) {
	params.MarketCode = strings.ToUpper(strings.TrimSpace(params.MarketCode))
	if len(params.MarketCode) != 2 {
		return ResolveListingParams{}, fmt.Errorf("%w: market code", ErrInvalidInput)
	}
	params.SellerListingID = strings.TrimSpace(params.SellerListingID)
	if params.SellerListingID == "" {
		return ResolveListingParams{}, fmt.Errorf("%w: seller listing id", ErrInvalidInput)
	}
	if params.Quantity == 0 {
		params.Quantity = 1
	}
	if params.Quantity <= 0 || params.Quantity > 10000 {
		return ResolveListingParams{}, fmt.Errorf("%w: quantity must be between 1 and 10000", ErrQuantityInvalid)
	}
	if params.SourceCollection != "" {
		if !params.SourceCollection.Valid() {
			return ResolveListingParams{}, fmt.Errorf("%w: %q", ErrInvalidCollectionType, params.SourceCollection)
		}
	}
	if params.Locale == "" {
		params.Locale = i18n.Default()
	}
	if params.Locale != i18n.LocaleArabic && params.Locale != i18n.LocaleEnglish {
		return ResolveListingParams{}, fmt.Errorf("%w: locale", ErrInvalidInput)
	}
	return params, nil
}

func encodeCursor(cursor Cursor) (string, error) {
	cursorJSON, err := json.Marshal(cursor)
	if err != nil {
		return "", fmt.Errorf("encode marketplace cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(cursorJSON), nil
}

func decodeCursor(encodedCursor string) (*Cursor, error) {
	if strings.TrimSpace(encodedCursor) == "" {
		return nil, nil
	}
	cursorJSON, err := base64.RawURLEncoding.DecodeString(encodedCursor)
	if err != nil {
		return nil, fmt.Errorf("%w: cursor", ErrInvalidInput)
	}
	var cursor Cursor
	if err := json.Unmarshal(cursorJSON, &cursor); err != nil || cursor.SellerListingID == "" {
		return nil, fmt.Errorf("%w: cursor", ErrInvalidInput)
	}
	return &cursor, nil
}
