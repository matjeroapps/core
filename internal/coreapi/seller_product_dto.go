package coreapi

// Dedicated API DTOs for the internal Seller catalog boundary. Domain structs
// are never serialized directly when their shape differs from the Seller API
// contract (e.g. commerce.SellerProductDetail exposes `categories` and a
// `[]SellerInventorySummary`; the Seller contract requires `category_ids` and
// an aggregate `inventory_summary` object).

import (
	"time"

	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/packages/money"
)

type moneyDTO struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

func toMoneyDTO(m money.Money) moneyDTO {
	return moneyDTO{Amount: m.AmountMinor, Currency: m.Currency}
}

type sellerProductDTO struct {
	ID        string    `json:"id"`
	Slug      string    `json:"slug"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type sellerListingDTO struct {
	ID         string    `json:"id"`
	StoreID    string    `json:"store_id"`
	ProductID  string    `json:"product_id"`
	MarketCode string    `json:"market_code"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type sellerInventoryLocationDTO struct {
	LocationID   string `json:"location_id"`
	LocationName string `json:"location_name"`
	SKUID        string `json:"sku_id"`
	OnHandQty    int64  `json:"on_hand_qty"`
	ReservedQty  int64  `json:"reserved_qty"`
	AvailableQty int64  `json:"available_qty"`
}

type sellerInventoryAggregateDTO struct {
	TotalOnHand    int64                        `json:"total_on_hand"`
	TotalReserved  int64                        `json:"total_reserved"`
	TotalAvailable int64                        `json:"total_available"`
	Locations      []sellerInventoryLocationDTO `json:"locations"`
}

func toInventoryAggregateDTO(agg commerce.SellerInventoryAggregate) sellerInventoryAggregateDTO {
	out := sellerInventoryAggregateDTO{
		TotalOnHand:    agg.TotalOnHand,
		TotalReserved:  agg.TotalReserved,
		TotalAvailable: agg.TotalAvailable,
		Locations:      make([]sellerInventoryLocationDTO, 0, len(agg.Locations)),
	}
	for _, l := range agg.Locations {
		out.Locations = append(out.Locations, sellerInventoryLocationDTO{
			LocationID:   l.LocationID,
			LocationName: l.LocationName,
			SKUID:        l.SKUID,
			OnHandQty:    l.OnHandQty,
			ReservedQty:  l.ReservedQty,
			AvailableQty: l.AvailableQty,
		})
	}
	return out
}

func toInventoryAggregateFromSummaries(items []commerce.SellerInventorySummary) sellerInventoryAggregateDTO {
	agg := commerce.SellerInventoryAggregate{Locations: make([]commerce.SellerInventoryLocation, 0, len(items))}
	for _, item := range items {
		agg.TotalOnHand += item.OnHandQty
		agg.TotalReserved += item.ReservedQty
		agg.TotalAvailable += item.AvailableQty
		agg.Locations = append(agg.Locations, commerce.SellerInventoryLocation{
			LocationID:   item.FulfillmentLocationID,
			LocationName: item.LocationName,
			SKUID:        item.SKUID,
			OnHandQty:    item.OnHandQty,
			ReservedQty:  item.ReservedQty,
			AvailableQty: item.AvailableQty,
		})
	}
	return toInventoryAggregateDTO(agg)
}

type sellerProductDetailResponse struct {
	Product          sellerProductDTO                    `json:"product"`
	Source           string                              `json:"source"`
	Translations     []commerce.ProductTranslation       `json:"translations"`
	CategoryIDs      []string                            `json:"category_ids"`
	Variants         []commerce.Variant                  `json:"variants"`
	SKUs             []commerce.SKU                      `json:"skus"`
	Media            []commerce.MediaMetadata            `json:"media"`
	Listing          sellerListingDTO                    `json:"listing"`
	CurrentPrice     *moneyDTO                           `json:"current_price"`
	InventorySummary sellerInventoryAggregateDTO         `json:"inventory_summary"`
	Presentation     *commerce.SellerListingPresentation `json:"presentation"`
	PurchaseBehavior string                              `json:"purchase_behavior"`
	PublishReadiness commerce.PublishReadiness           `json:"publish_readiness"`
}

func toSellerProductDetailResponse(detail commerce.SellerProductDetail) sellerProductDetailResponse {
	categoryIDs := make([]string, 0, len(detail.Categories))
	for _, c := range detail.Categories {
		categoryIDs = append(categoryIDs, c.ID)
	}

	var currentPrice *moneyDTO
	if detail.Price != nil {
		cp := toMoneyDTO(detail.Price.Price)
		currentPrice = &cp
	}

	var presentation *commerce.SellerListingPresentation
	if detail.Presentation != nil {
		p := *detail.Presentation
		presentation = &p
	}

	return sellerProductDetailResponse{
		Product: sellerProductDTO{
			ID:        detail.Product.ID,
			Slug:      detail.Product.Slug,
			Status:    detail.Product.Status,
			CreatedAt: detail.Product.CreatedAt,
			UpdatedAt: detail.Product.UpdatedAt,
		},
		Source:       detail.Source,
		Translations: detail.Translations,
		CategoryIDs:  categoryIDs,
		Variants:     detail.Variants,
		SKUs:         detail.SKUs,
		Media:        detail.Media,
		Listing: sellerListingDTO{
			ID:         detail.Listing.ID,
			StoreID:    detail.Listing.StoreID,
			ProductID:  detail.Listing.ProductID,
			MarketCode: detail.Listing.MarketCode,
			Status:     detail.Listing.Status,
			CreatedAt:  detail.Listing.CreatedAt,
			UpdatedAt:  detail.Listing.UpdatedAt,
		},
		CurrentPrice:     currentPrice,
		InventorySummary: toInventoryAggregateFromSummaries(detail.InventorySummary),
		Presentation:     presentation,
		PurchaseBehavior: detail.PurchaseBehavior,
		PublishReadiness: detail.PublishReadiness,
	}
}

type sellerProductListItemResponse struct {
	Product          sellerProductDTO            `json:"product"`
	Source           string                      `json:"source"`
	Name             string                      `json:"name"`
	ListingID        string                      `json:"listing_id"`
	ListingStatus    string                      `json:"listing_status"`
	CurrentPrice     *moneyDTO                   `json:"current_price,omitempty"`
	InventorySummary sellerInventoryAggregateDTO `json:"inventory_summary"`
	PublishReadiness commerce.PublishReadiness   `json:"publish_readiness"`
}

func toSellerProductListItemResponse(view commerce.SellerProductListView) sellerProductListItemResponse {
	var currentPrice *moneyDTO
	if view.CurrentPrice != nil {
		cp := toMoneyDTO(view.CurrentPrice.Price)
		currentPrice = &cp
	}
	return sellerProductListItemResponse{
		Product: sellerProductDTO{
			ID:        view.Product.ID,
			Slug:      view.Product.Slug,
			Status:    view.Product.Status,
			CreatedAt: view.Product.CreatedAt,
			UpdatedAt: view.Product.UpdatedAt,
		},
		Source:           view.Source,
		Name:             view.Name,
		ListingID:        view.ListingID,
		ListingStatus:    view.ListingStatus,
		CurrentPrice:     currentPrice,
		InventorySummary: toInventoryAggregateDTO(view.InventorySummary),
		PublishReadiness: view.PublishReadiness,
	}
}

type sellerLocationListResponse struct {
	Locations []commerce.FulfillmentLocation `json:"locations"`
}

type sellerInventoryListResponse struct {
	Inventory []commerce.SellerInventorySummary `json:"inventory"`
}
