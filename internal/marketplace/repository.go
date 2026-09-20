package marketplace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"core/packages/i18n"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return Repository{pool: pool}
}

const collectionBase = `
WITH eligible AS (
	SELECT
		sl.id AS listing_id,
		sl.product_id,
		sl.store_id,
		sl.market_code,
		sl.created_at,
		st.name AS store_name,
		p.slug,
		COALESCE(t.name, tf.name, p.slug) AS title,
		slp.amount_minor,
		slp.currency_code,
		COALESCE(sales.delivered_quantity, 0) AS delivered_quantity,
		COALESCE(local_stock.in_stock, false) AS local_in_stock,
		COALESCE(media.uri, legacy_media.uri, '') AS primary_image_uri
	FROM seller_listings sl
	JOIN stores st
		ON st.id = sl.store_id
		AND st.market_code = sl.market_code
	JOIN products p
		ON p.id = sl.product_id
		AND p.status = 'active'
	JOIN seller_listing_prices slp
		ON slp.seller_listing_id = sl.id
		AND slp.is_current = true
	LEFT JOIN product_translations t
		ON t.product_id = p.id
		AND t.locale = $2
	LEFT JOIN product_translations tf
		ON tf.product_id = p.id
		AND tf.locale = $3
	LEFT JOIN LATERAL (
		SELECT SUM(oi.quantity)::bigint AS delivered_quantity
		FROM order_items oi
		JOIN orders o ON o.id = oi.order_id
		WHERE oi.seller_listing_id = sl.id
			AND oi.seller_listing_id IS NOT NULL
			AND o.market_code = $1
			AND o.status = 'delivered'
	) sales ON true
	LEFT JOIN LATERAL (
		SELECT true AS in_stock
		FROM variants v
		JOIN skus sku
			ON sku.variant_id = v.id
			AND sku.status = 'active'
		JOIN inventory_snapshots inv
			ON inv.sku_id = sku.id
		JOIN fulfillment_locations fl
			ON fl.id = inv.fulfillment_location_id
			AND fl.store_id = sl.store_id
			AND fl.market_code = sl.market_code
			AND fl.status = 'active'
		WHERE v.product_id = sl.product_id
			AND (inv.on_hand_qty - inv.reserved_qty) > 0
		LIMIT 1
	) local_stock ON true
	LEFT JOIN LATERAL (
		SELECT sma.storage_key AS uri
		FROM product_media_references pmr
		JOIN store_media_assets sma
			ON sma.id = pmr.asset_id
			AND sma.status = 'ready'
		WHERE pmr.product_id = p.id
			AND pmr.store_id = sl.store_id
		ORDER BY pmr.is_primary DESC, pmr.sort_order ASC, pmr.id ASC
		LIMIT 1
	) media ON true
	LEFT JOIN LATERAL (
		SELECT mm.uri
		FROM media_metadata mm
		WHERE mm.product_id = p.id
		ORDER BY mm.is_primary DESC, mm.sort_order ASC, mm.id ASC
		LIMIT 1
	) legacy_media ON true
	WHERE sl.market_code = $1
		AND sl.status = 'published'
)
`

func (r Repository) ListCollection(ctx context.Context, collectionType CollectionType, request PageRequest, cursor *Cursor) (Collection, error) {
	if r.pool == nil {
		return Collection{}, errors.New("marketplace repository is not configured")
	}
	query, args, err := buildCollectionQuery(collectionType, request, cursor)
	if err != nil {
		return Collection{}, err
	}
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return Collection{}, fmt.Errorf("list marketplace collection: %w", err)
	}
	defer rows.Close()
	return scanCollectionRows(rows, collectionType, request)
}

type collectionQuerySpec struct {
	condition string
	orderBy   string
}

func collectionQuerySpecFor(collectionType CollectionType) (collectionQuerySpec, error) {
	var (
		spec collectionQuerySpec
	)
	switch collectionType {
	case CollectionBestSellers:
		spec.condition = `e.delivered_quantity > 0`
		spec.orderBy = `e.delivered_quantity DESC, e.listing_id ASC`
	case CollectionNewProducts:
		spec.orderBy = `e.created_at DESC, e.listing_id ASC`
	case CollectionUniqueProducts:
		spec.condition = `
NOT EXISTS (
	SELECT 1
	FROM seller_listings other
	JOIN products other_product
		ON other_product.id = other.product_id
		AND other_product.status = 'active'
	JOIN seller_listing_prices other_price
		ON other_price.seller_listing_id = other.id
		AND other_price.is_current = true
	WHERE other.product_id = e.product_id
		AND other.market_code = e.market_code
		AND other.status = 'published'
		AND other.id <> e.listing_id
)`
		spec.orderBy = `e.created_at DESC, e.listing_id ASC`
	case CollectionFastDelivery:
		spec.condition = `e.local_in_stock = true`
		spec.orderBy = `e.created_at DESC, e.listing_id ASC`
	default:
		return collectionQuerySpec{}, ErrInvalidCollectionType
	}
	return spec, nil
}

func buildCollectionQuery(collectionType CollectionType, request PageRequest, cursor *Cursor) (string, []any, error) {
	spec, err := collectionQuerySpecFor(collectionType)
	if err != nil {
		return "", nil, err
	}
	args := []any{request.MarketCode, string(request.Locale), string(fallbackLocale(request.Locale))}
	conditions := []string{}
	if strings.TrimSpace(spec.condition) != "" {
		conditions = append(conditions, strings.TrimSpace(spec.condition))
	}
	cursorClause := ""
	if cursor != nil {
		switch collectionType {
		case CollectionBestSellers:
			cursorClause = `(e.delivered_quantity < $4 OR (e.delivered_quantity = $4 AND e.listing_id > $5))`
			args = append(args, cursor.DeliveredQty, cursor.SellerListingID)
		default:
			cursorClause = `(e.created_at < $4 OR (e.created_at = $4 AND e.listing_id > $5))`
			args = append(args, cursor.CreatedAt, cursor.SellerListingID)
		}
	}
	if cursorClause != "" {
		conditions = append(conditions, cursorClause)
	}
	where := collectionWhereClause(conditions)
	limitPosition := len(args) + 1
	args = append(args, request.Limit+1)

	return collectionBase + fmt.Sprintf(`
SELECT
	e.listing_id,
	e.product_id,
	e.store_id,
	e.store_name,
	e.title,
	e.slug,
	e.amount_minor,
	e.currency_code,
	e.primary_image_uri,
	e.local_in_stock,
	e.delivered_quantity,
	e.created_at
FROM eligible e
%s
ORDER BY %s
LIMIT $%d
`, where, spec.orderBy, limitPosition), args, nil
}

func collectionWhereClause(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(conditions, "\n\tAND ")
}

func scanCollectionRows(rows pgx.Rows, collectionType CollectionType, request PageRequest) (Collection, error) {
	items := make([]Item, 0, request.Limit)
	for rows.Next() {
		collectionItem, err := scanCollectionItem(rows, collectionType)
		if err != nil {
			return Collection{}, err
		}
		items = append(items, collectionItem)
	}
	if err := rows.Err(); err != nil {
		return Collection{}, fmt.Errorf("iterate marketplace collection: %w", err)
	}

	items, nextCursor, err := pageCollectionItems(items, request.Limit)
	if err != nil {
		return Collection{}, err
	}
	return Collection{
		CollectionType: collectionType,
		MarketCode:     request.MarketCode,
		Items:          items,
		NextCursor:     nextCursor,
	}, nil
}

func scanCollectionItem(rows pgx.Rows, collectionType CollectionType) (Item, error) {
	var (
		collectionItem Item
		currency       string
		inStock        bool
	)
	if err := rows.Scan(
		&collectionItem.ListingID,
		&collectionItem.ProductID,
		&collectionItem.StoreID,
		&collectionItem.StoreName,
		&collectionItem.Title,
		&collectionItem.Slug,
		&collectionItem.Price.AmountMinor,
		&currency,
		&collectionItem.PrimaryImageURI,
		&inStock,
		&collectionItem.DeliveredQty,
		&collectionItem.CreatedAt,
	); err != nil {
		return Item{}, fmt.Errorf("scan marketplace collection: %w", err)
	}
	collectionItem.Price.Currency = strings.TrimSpace(currency)
	collectionItem.Availability = AvailabilityOutStock
	if inStock {
		collectionItem.Availability = AvailabilityInStock
	}
	collectionItem.Badges = badgesFor(collectionType)
	return collectionItem, nil
}

func pageCollectionItems(items []Item, limit int) ([]Item, string, error) {
	if len(items) <= limit {
		return items, "", nil
	}
	lastItem := items[limit-1]
	nextCursor, err := encodeCursor(Cursor{
		CreatedAt:       lastItem.CreatedAt,
		DeliveredQty:    lastItem.DeliveredQty,
		SellerListingID: lastItem.ListingID,
	})
	if err != nil {
		return nil, "", err
	}
	return items[:limit], nextCursor, nil
}

func fallbackLocale(locale i18n.Locale) i18n.Locale {
	if locale == i18n.LocaleArabic {
		return i18n.LocaleEnglish
	}
	return i18n.LocaleArabic
}

func badgesFor(collectionType CollectionType) []string {
	switch collectionType {
	case CollectionBestSellers:
		return []string{BadgeBestSeller}
	case CollectionNewProducts:
		return []string{BadgeNew}
	case CollectionUniqueProducts:
		return []string{BadgeUnique}
	case CollectionFastDelivery:
		return []string{BadgeFastDelivery}
	default:
		return []string{}
	}
}

var _ RepositoryReader = Repository{}

func (r Repository) ResolveListing(ctx context.Context, params ResolveListingParams) (ResolvedListing, error) {
	if r.pool == nil {
		return ResolvedListing{}, errors.New("marketplace repository is not configured")
	}

	var (
		listingID       string
		storeID         string
		productID       string
		supplierOfferID *string
		marketCode      string
		listingStatus   string
		storeStatus     string
		productStatus   string
		productSlug     string
		productTitle    string
		priceAmount     *int64
		priceCurrency   *string
		marketCurrency  string
	)

	query := `
		SELECT
			sl.id,
			sl.store_id,
			sl.product_id,
			sl.supplier_offer_id,
			sl.market_code,
			sl.status AS listing_status,
			st.status AS store_status,
			p.status AS product_status,
			p.slug AS product_slug,
			COALESCE(t.name, tf.name, p.slug) AS product_title,
			slp.amount_minor,
			slp.currency_code,
			m.currency_code AS market_currency
		FROM seller_listings sl
		JOIN stores st ON st.id = sl.store_id AND st.market_code = sl.market_code
		JOIN markets m ON m.code = sl.market_code
		JOIN products p ON p.id = sl.product_id
		LEFT JOIN seller_listing_prices slp ON slp.seller_listing_id = sl.id AND slp.is_current = true
		LEFT JOIN product_translations t ON t.product_id = p.id AND t.locale = $2
		LEFT JOIN product_translations tf ON tf.product_id = p.id AND tf.locale = $3
		WHERE sl.id = $1
	`

	err := r.pool.QueryRow(ctx, query, params.SellerListingID, string(params.Locale), string(fallbackLocale(params.Locale))).Scan(
		&listingID,
		&storeID,
		&productID,
		&supplierOfferID,
		&marketCode,
		&listingStatus,
		&storeStatus,
		&productStatus,
		&productSlug,
		&productTitle,
		&priceAmount,
		&priceCurrency,
		&marketCurrency,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			var existingMarket string
			checkErr := r.pool.QueryRow(ctx, `SELECT market_code FROM seller_listings WHERE id = $1`, params.SellerListingID).Scan(&existingMarket)
			if checkErr == nil {
				if strings.ToUpper(strings.TrimSpace(existingMarket)) != params.MarketCode {
					return ResolvedListing{}, ErrCrossMarketAccess
				}
			}
			return ResolvedListing{}, ErrListingNotFound
		}
		return ResolvedListing{}, fmt.Errorf("query listing for resolution: %w", err)
	}

	if marketCode != params.MarketCode {
		return ResolvedListing{}, ErrCrossMarketAccess
	}
	if listingStatus != "published" {
		return ResolvedListing{}, ErrListingNotPublished
	}
	if storeStatus != "active" {
		return ResolvedListing{}, ErrListingNotPublished
	}
	if productStatus != "active" {
		return ResolvedListing{}, ErrProductUnavailable
	}
	if priceAmount == nil || priceCurrency == nil || *priceAmount < 0 || strings.TrimSpace(*priceCurrency) != strings.TrimSpace(marketCurrency) {
		return ResolvedListing{}, ErrPriceUnavailable
	}

	type activeSKU struct {
		variantID string
		skuID     string
		skuCode   string
	}

	skuRows, err := r.pool.Query(ctx, `
		SELECT v.id, sk.id, sk.code
		FROM variants v
		JOIN skus sk ON sk.variant_id = v.id
		WHERE v.product_id = $1
		  AND v.status = 'active'
		  AND sk.status = 'active'
		ORDER BY sk.created_at ASC, sk.id ASC
	`, productID)
	if err != nil {
		return ResolvedListing{}, fmt.Errorf("query active skus: %w", err)
	}
	defer skuRows.Close()

	var candidateSKUs []activeSKU
	for skuRows.Next() {
		var s activeSKU
		if err := skuRows.Scan(&s.variantID, &s.skuID, &s.skuCode); err != nil {
			return ResolvedListing{}, fmt.Errorf("scan active sku: %w", err)
		}
		candidateSKUs = append(candidateSKUs, s)
	}
	if err := skuRows.Err(); err != nil {
		return ResolvedListing{}, fmt.Errorf("iterate active skus: %w", err)
	}

	if len(candidateSKUs) == 0 {
		return ResolvedListing{}, ErrProductUnavailable
	}

	if params.SKUID != "" {
		var matched *activeSKU
		for _, s := range candidateSKUs {
			if s.skuID == params.SKUID {
				matched = &s
				break
			}
		}
		if matched == nil {
			return ResolvedListing{}, ErrProductUnavailable
		}
		candidateSKUs = []activeSKU{*matched}
	}

	var sourceSupplierID *string
	if supplierOfferID != nil {
		var (
			soSupplierID string
			soStatus     string
			soMarket     string
			spID         *string
			soAvailable  bool
		)
		err := r.pool.QueryRow(ctx, `
			SELECT so.supplier_id, so.status, so.market_code, sp.id, COALESCE(soa.is_available, true)
			FROM supplier_offers so
			JOIN supplier_products sp ON sp.id = so.supplier_product_id AND sp.supplier_id = so.supplier_id AND sp.product_id = $2
			LEFT JOIN supplier_offer_availability soa ON soa.supplier_offer_id = so.id
			WHERE so.id = $1
		`, *supplierOfferID, productID).Scan(&soSupplierID, &soStatus, &soMarket, &spID, &soAvailable)
		if err != nil || soStatus != "active" || soMarket != marketCode || spID == nil || !soAvailable {
			return ResolvedListing{}, ErrListingNotPublished
		}
		sourceSupplierID = &soSupplierID
	}

	type locCandidate struct {
		sku          activeSKU
		locationID   string
		availableQty int64
	}

	var eligibleCandidates []locCandidate
	var anyLocationExists bool

	for _, sku := range candidateSKUs {
		var locRows pgx.Rows
		var err error

		if params.SourceCollection == CollectionFastDelivery {
			locRows, err = r.pool.Query(ctx, `
				SELECT fl.id, (inv.on_hand_qty - inv.reserved_qty) AS available_qty
				FROM fulfillment_locations fl
				JOIN inventory_snapshots inv ON inv.fulfillment_location_id = fl.id AND inv.sku_id = $1
				WHERE fl.store_id = $2
				  AND fl.supplier_id IS NULL
				  AND fl.status = 'active'
				  AND fl.market_code = $3
				ORDER BY (inv.on_hand_qty - inv.reserved_qty) DESC, fl.id ASC
			`, sku.skuID, storeID, marketCode)
		} else if sourceSupplierID != nil {
			locRows, err = r.pool.Query(ctx, `
				SELECT fl.id, (inv.on_hand_qty - inv.reserved_qty) AS available_qty
				FROM fulfillment_locations fl
				JOIN inventory_snapshots inv ON inv.fulfillment_location_id = fl.id AND inv.sku_id = $1
				WHERE fl.supplier_id = $2
				  AND fl.store_id IS NULL
				  AND fl.status = 'active'
				  AND fl.market_code = $3
				ORDER BY (inv.on_hand_qty - inv.reserved_qty) DESC, fl.id ASC
			`, sku.skuID, *sourceSupplierID, marketCode)
		} else {
			locRows, err = r.pool.Query(ctx, `
				SELECT fl.id, (inv.on_hand_qty - inv.reserved_qty) AS available_qty
				FROM fulfillment_locations fl
				JOIN inventory_snapshots inv ON inv.fulfillment_location_id = fl.id AND inv.sku_id = $1
				WHERE fl.store_id = $2
				  AND fl.supplier_id IS NULL
				  AND fl.status = 'active'
				  AND fl.market_code = $3
				ORDER BY (inv.on_hand_qty - inv.reserved_qty) DESC, fl.id ASC
			`, sku.skuID, storeID, marketCode)
		}

		if err != nil {
			return ResolvedListing{}, fmt.Errorf("query fulfillment locations: %w", err)
		}

		for locRows.Next() {
			var locID string
			var availQty int64
			if err := locRows.Scan(&locID, &availQty); err != nil {
				locRows.Close()
				return ResolvedListing{}, fmt.Errorf("scan fulfillment location: %w", err)
			}
			anyLocationExists = true
			if availQty >= params.Quantity {
				eligibleCandidates = append(eligibleCandidates, locCandidate{
					sku:          sku,
					locationID:   locID,
					availableQty: availQty,
				})
			}
		}
		locRows.Close()
	}

	if !anyLocationExists {
		return ResolvedListing{}, ErrNoEligibleLocation
	}
	if len(eligibleCandidates) == 0 {
		return ResolvedListing{}, ErrInventoryUnavailable
	}

	selected := eligibleCandidates[0]
	for _, cand := range eligibleCandidates[1:] {
		if cand.availableQty > selected.availableQty {
			selected = cand
		}
	}

	var sourceCollectionPtr *string
	if params.SourceCollection != "" {
		s := string(params.SourceCollection)
		sourceCollectionPtr = &s
	}

	return ResolvedListing{
		SellerListingID:       listingID,
		ProductID:             productID,
		StoreID:               storeID,
		MarketCode:            marketCode,
		SKUID:                 selected.sku.skuID,
		Quantity:              params.Quantity,
		UnitPriceMinor:        *priceAmount,
		CurrencyCode:          strings.TrimSpace(*priceCurrency),
		FulfillmentLocationID: selected.locationID,
		ProductTitle:          productTitle,
		SKUCode:               selected.sku.skuCode,
		Attribution: MarketplaceAttribution{
			SellerListingID:  listingID,
			StoreID:          storeID,
			MarketCode:       marketCode,
			SourceCollection: sourceCollectionPtr,
		},
	}, nil
}
