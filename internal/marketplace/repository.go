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
