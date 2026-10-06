package listings

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
)

// CalculateMarginPercentage computes the margin percentage based on wholesale cost:
// ((retail - wholesale) / wholesale) * 100
// If wholesale is 0 (seller-owned product), margin is 100.0%.
func CalculateMarginPercentage(retailMinor, wholesaleMinor int64) float64 {
	if wholesaleMinor <= 0 {
		return 100.0
	}
	ratio := float64(retailMinor-wholesaleMinor) / float64(wholesaleMinor) * 100.0
	return math.Round(ratio*100) / 100
}

// ValidateMarginGuardrail enforces that retail price cannot be lower than wholesale cost
// unless allowSubWholesale is true, the actor is the store owner, and a non-empty audit reason is provided.
func ValidateMarginGuardrail(retailMinor, wholesaleMinor int64, allowSubWholesale bool, isOwner bool, auditReason string) error {
	if retailMinor < 0 {
		return fmt.Errorf("%w: retail price cannot be negative", ErrInvalidInput)
	}

	// If no wholesale cost (seller-owned product), margin guardrail does not apply
	if wholesaleMinor <= 0 {
		return nil
	}

	if retailMinor < wholesaleMinor {
		if !allowSubWholesale {
			return fmt.Errorf("%w: retail price (%d) cannot be lower than wholesale cost (%d) without store owner authorization",
				ErrUnsafeMargin, retailMinor, wholesaleMinor)
		}
		if !isOwner {
			return ErrOwnerOverrideRequired
		}
		if strings.TrimSpace(auditReason) == "" {
			return fmt.Errorf("%w: audit reason is mandatory for sub-wholesale override", ErrInvalidInput)
		}
	}

	return nil
}

type Repository interface {
	GetListingWithWholesalePrice(ctx context.Context, storeID, listingID string) (*ListingWholesaleInfo, error)
	SaveListingPrice(ctx context.Context, listingID string, amountMinor int64, currency string) (time.Time, error)
}

type Service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return Service{repo: repo}
}

func (s Service) SetListingPrice(ctx context.Context, storeID, listingID string, req ListingPricingRequest, isOwner bool) (*ListingPricingResponse, error) {
	if _, err := uuid.Parse(strings.TrimSpace(storeID)); err != nil {
		return nil, fmt.Errorf("%w: invalid store_id", ErrInvalidInput)
	}
	if _, err := uuid.Parse(strings.TrimSpace(listingID)); err != nil {
		return nil, fmt.Errorf("%w: invalid listing_id", ErrInvalidInput)
	}

	info, err := s.repo.GetListingWithWholesalePrice(ctx, storeID, listingID)
	if err != nil {
		return nil, err
	}

	if err := ValidateMarginGuardrail(
		req.RetailPriceMinorUnits,
		info.WholesalePriceMinorUnits,
		req.AllowSubWholesale,
		isOwner,
		req.AuditReason,
	); err != nil {
		return nil, err
	}

	currency := info.WholesaleCurrency
	if currency == "" {
		switch info.MarketCode {
		case "EG":
			currency = "EGP"
		case "SA":
			currency = "SAR"
		case "AE":
			currency = "AED"
		default:
			currency = "SAR"
		}
	}

	updatedAt, err := s.repo.SaveListingPrice(ctx, listingID, req.RetailPriceMinorUnits, currency)
	if err != nil {
		return nil, err
	}

	marginPct := CalculateMarginPercentage(req.RetailPriceMinorUnits, info.WholesalePriceMinorUnits)
	subWholesaleActive := info.WholesalePriceMinorUnits > 0 && req.RetailPriceMinorUnits < info.WholesalePriceMinorUnits

	return &ListingPricingResponse{
		ListingID:                  listingID,
		StoreID:                    storeID,
		RetailPriceMinorUnits:      req.RetailPriceMinorUnits,
		Currency:                   currency,
		WholesalePriceMinorUnits:   info.WholesalePriceMinorUnits,
		MarginPercentage:           marginPct,
		SubWholesaleOverrideActive: subWholesaleActive,
		UpdatedAt:                  updatedAt,
	}, nil
}
