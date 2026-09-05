package storefront

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/matjeroapps/core/modules/catalog"
)

// CatalogRepository is the store-scoped public read model for the native
// storefront. It is deliberately separate from commerce.Repository: public
// callers must not reach commerce aggregates, and every projection here is built
// field-by-field for customer consumption rather than by serializing a domain
// struct and hiding parts of it afterwards.
//
// PostgreSQL is the authoritative source today. All parameters crossing this
// boundary are storage-neutral (slugs, minor-unit amounts, domain sort names), so
// a dedicated search read model can replace these queries without changing the
// public API contract.
//
// Every method is read-only: no statement here writes inventory, reservations,
// listing state, product state, or supplier offer state.
type CatalogRepository struct {
	pool *pgxpool.Pool
}

// NewCatalogRepository builds the public catalog read model over a pgx pool.
func NewCatalogRepository(pool *pgxpool.Pool) CatalogRepository {
	return CatalogRepository{pool: pool}
}

func readError(err error, action string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrCatalogNotFound
	}
	// The raw PostgreSQL error is wrapped, never returned as a sentinel, so
	// actor APIs classify failures by errors.Is instead of matching strings.
	return fmt.Errorf("%s: %w", action, err)
}

// eligibleListings is the public projection over the shared Core-owned
// canonical Listing primitive. Its source-aware inventory predicate is applied
// to that selected Listing, never to an arbitrary Listing for the Product.
// $1 store id, $2 market code, $3 locale, $4 fallback locale.
const eligibleListings = `
	SELECT
		l.product_id,
		l.product_slug,
		l.product_created_at,
		COALESCE(t.name, tf.name, l.product_slug) AS name,
		COALESCE(t.description, tf.description, '') AS description,
		l.price_minor,
		l.price_currency,
		(l.supplier_available AND COALESCE(stock.in_stock, false)) AS in_stock
	FROM (` + catalog.CanonicalListingSQL + `) l
	LEFT JOIN product_translations t ON t.product_id = l.product_id AND t.locale = $3
	LEFT JOIN product_translations tf ON tf.product_id = l.product_id AND tf.locale = $4
	LEFT JOIN LATERAL (
		SELECT EXISTS (
			SELECT 1
			FROM variants v
			JOIN skus sk ON sk.variant_id = v.id AND sk.status = 'active'
			JOIN inventory_snapshots inv ON inv.sku_id = sk.id
			JOIN fulfillment_locations fl
				ON fl.id = inv.fulfillment_location_id
				AND fl.status = 'active'
				AND fl.market_code = l.market_code
			WHERE v.product_id = l.product_id
				AND (
					(l.supplier_offer_id IS NULL AND fl.store_id = l.store_id AND fl.supplier_id IS NULL)
					OR
					(l.supplier_offer_id IS NOT NULL AND fl.store_id IS NULL AND fl.supplier_id = l.supplier_id)
				)
				AND (inv.on_hand_qty - inv.reserved_qty) > 0
		) AS in_stock
	) stock ON true
`

func (s CatalogScope) baseArgs() []any {
	return []any{s.storeID, s.marketCode, string(s.locale), string(fallbackLocale(s.locale))}
}

func availabilityState(inStock bool) string {
	if inStock {
		return AvailabilityInStock
	}
	return AvailabilityOutOfStock
}

// Bootstrap returns everything the storefront needs before rendering: public
// store identity, market/currency/locale context, public settings, and the
// published theme.
//
// Only the "public" object inside store settings is exposed. Store settings are
// free-form seller-controlled JSON that may hold operational configuration, so
// the whole document is never handed to customers.
//
// Only published theme configuration is exposed. Draft configuration stays
// reachable exclusively through the signed preview token.
func (r CatalogRepository) Bootstrap(ctx context.Context, scope CatalogScope) (StoreBootstrap, error) {
	var (
		out          StoreBootstrap
		locales      []string
		settingsJSON []byte
		themeKey     *string
		themeVersion *string
		themeConfig  []byte
		themeRev     *int
	)

	err := r.pool.QueryRow(ctx, `
		SELECT
			s.code, s.name,
			m.code, cur.code, cur.symbol, cur.minor_unit,
			m.timezone, m.default_locale,
			loc.locales,
			COALESCE(ss.settings -> 'public', '{}'::jsonb),
			theme.key, theme.version, theme.published_config, theme.published_revision
		FROM stores s
		JOIN markets m ON m.code = s.market_code
		JOIN currencies cur ON cur.code = m.currency_code
		LEFT JOIN store_settings ss ON ss.store_id = s.id
		LEFT JOIN LATERAL (
			SELECT COALESCE(
				array_agg(ml.locale ORDER BY ml.is_default DESC, ml.sort_order, ml.locale),
				ARRAY[]::text[]
			) AS locales
			FROM market_locales ml
			WHERE ml.market_code = m.code AND ml.is_enabled
		) loc ON true
		LEFT JOIN LATERAL (
			SELECT th.key, tv.version, tc.published_config, tc.published_revision
			FROM theme_installations ti
			JOIN themes th ON th.id = ti.theme_id
			JOIN theme_versions tv ON tv.id = ti.theme_version_id
			LEFT JOIN theme_configurations tc ON tc.installation_id = ti.id
			WHERE ti.store_id = s.id AND ti.status = 'active'
			LIMIT 1
		) theme ON true
		WHERE s.id = $1
	`, scope.storeID).Scan(
		&out.StoreCode, &out.StoreName,
		&out.Market, &out.Currency.Code, &out.Currency.Symbol, &out.Currency.MinorUnit,
		&out.Timezone, &out.DefaultLocale,
		&locales,
		&settingsJSON,
		&themeKey, &themeVersion, &themeConfig, &themeRev,
	)
	if err != nil {
		return StoreBootstrap{}, readError(err, "storefront bootstrap")
	}

	out.Domain = scope.domain
	out.SupportedLocales = locales
	out.Settings = map[string]any{}
	if len(settingsJSON) > 0 {
		if err := json.Unmarshal(settingsJSON, &out.Settings); err != nil {
			return StoreBootstrap{}, fmt.Errorf("decode public store settings: %w", err)
		}
	}

	if themeKey != nil && themeVersion != nil {
		theme := StoreTheme{Key: *themeKey, Version: *themeVersion, Configuration: map[string]any{}}
		if len(themeConfig) > 0 {
			if err := json.Unmarshal(themeConfig, &theme.Configuration); err != nil {
				return StoreBootstrap{}, fmt.Errorf("decode published theme configuration: %w", err)
			}
		}
		if themeRev != nil {
			theme.ConfigurationRevision = *themeRev
		}
		out.Theme = &theme
	}

	return out, nil
}

// publicCategoryTree resolves the categories a store may expose. Categories are
// global commerce records, so a category is public for a store only when that
// store has publicly eligible products in it; ancestors of those categories are
// included so the returned set is a connected tree rather than orphaned nodes.
//
// $5 onward is available to callers appending an extra predicate.
const publicCategoryTree = `
	WITH RECURSIVE listing AS (` + eligibleListings + `
	), direct AS (
		SELECT pc.category_id, COUNT(*) AS product_count
		FROM product_categories pc
		JOIN listing l ON l.product_id = pc.product_id
		JOIN categories c ON c.id = pc.category_id AND c.status = 'active'
		GROUP BY pc.category_id
	), tree(category_id) AS (
		SELECT category_id FROM direct
		UNION
		SELECT c.parent_category_id
		FROM categories c
		JOIN tree t ON t.category_id = c.id
		WHERE c.parent_category_id IS NOT NULL
	)
	SELECT
		c.slug,
		COALESCE(ct.name, ctf.name, c.slug),
		COALESCE(ct.description, ctf.description, ''),
		COALESCE(parent.slug, ''),
		COALESCE(d.product_count, 0)
	FROM tree t
	JOIN categories c ON c.id = t.category_id AND c.status = 'active'
	LEFT JOIN categories parent ON parent.id = c.parent_category_id AND parent.status = 'active'
	LEFT JOIN category_translations ct ON ct.category_id = c.id AND ct.locale = $3
	LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.locale = $4
	LEFT JOIN direct d ON d.category_id = c.id
`

// Categories returns the store's public category tree ordered by localized name.
// ParentSlug lets a client rebuild the hierarchy without further requests.
func (r CatalogRepository) Categories(ctx context.Context, scope CatalogScope) ([]CategoryNode, error) {
	rows, err := r.pool.Query(ctx, publicCategoryTree+`
		ORDER BY COALESCE(ct.name, ctf.name, c.slug) ASC, c.slug ASC
	`, scope.baseArgs()...)
	if err != nil {
		return nil, readError(err, "list public categories")
	}
	defer rows.Close()

	nodes := make([]CategoryNode, 0, 16)
	for rows.Next() {
		var node CategoryNode
		if err := rows.Scan(&node.Slug, &node.Name, &node.Description, &node.ParentSlug, &node.ProductCount); err != nil {
			return nil, readError(err, "scan public category")
		}
		nodes = append(nodes, node)
	}
	if rows.Err() != nil {
		return nil, readError(rows.Err(), "iterate public categories")
	}
	return nodes, nil
}

// CategoryBySlug resolves one category inside the store's public tree. A slug
// that exists globally but has no publicly eligible products in this store is
// reported as not found, which is what keeps another store's category from
// resolving here.
func (r CatalogRepository) CategoryBySlug(ctx context.Context, scope CatalogScope, slug string) (CategoryNode, error) {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return CategoryNode{}, fmt.Errorf("%w: category slug is required", ErrInvalidQuery)
	}

	args := append(scope.baseArgs(), slug)
	var node CategoryNode
	err := r.pool.QueryRow(ctx, publicCategoryTree+`
		WHERE c.slug = $5
		LIMIT 1
	`, args...).Scan(&node.Slug, &node.Name, &node.Description, &node.ParentSlug, &node.ProductCount)
	if err != nil {
		return CategoryNode{}, readError(err, "get public category")
	}
	return node, nil
}

// productFilters translates a normalized query into predicates over the eligible
// listing set. Filters are appended as bound parameters; no caller value is ever
// interpolated into SQL text.
func productFilters(query ProductQuery, args []any) ([]string, []any) {
	where := []string{"1=1"}

	if query.CategorySlug != "" {
		args = append(args, strings.ToLower(query.CategorySlug))
		where = append(where, fmt.Sprintf(`EXISTS (
			SELECT 1 FROM product_categories pc
			JOIN categories c ON c.id = pc.category_id AND c.status = 'active'
			WHERE pc.product_id = l.product_id AND c.slug = $%d
		)`, len(args)))
	}
	if keyword := strings.TrimSpace(strings.ToLower(query.Keyword)); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf(
			"(LOWER(l.name) LIKE $%d OR LOWER(l.description) LIKE $%d OR LOWER(l.product_slug) LIKE $%d)",
			len(args), len(args), len(args)))
	}
	if query.MinPriceMinor != nil {
		args = append(args, *query.MinPriceMinor)
		where = append(where, fmt.Sprintf("l.price_minor >= $%d", len(args)))
	}
	if query.MaxPriceMinor != nil {
		args = append(args, *query.MaxPriceMinor)
		where = append(where, fmt.Sprintf("l.price_minor <= $%d", len(args)))
	}
	switch query.Availability {
	case AvailabilityInStock:
		where = append(where, "l.in_stock")
	case AvailabilityOutOfStock:
		where = append(where, "NOT l.in_stock")
	}

	return where, args
}

// productOrder returns a deterministic ORDER BY for each public sort option. The
// product id tiebreaker keeps paging stable, so a row can neither be skipped nor
// repeated across pages when two products share a sort value.
func productOrder(sort string) string {
	switch sort {
	case SortPriceAsc:
		return "l.price_minor ASC, l.product_id ASC"
	case SortPriceDesc:
		return "l.price_minor DESC, l.product_id ASC"
	case SortNameAsc:
		return "l.name ASC, l.product_id ASC"
	default:
		return "l.product_created_at DESC, l.product_id DESC"
	}
}

// Products returns a bounded page of browse results plus the total match count.
// Keyword, category, price, availability, and sort all resolve inside the
// store-scoped eligible listing set, so a keyword can never surface another
// store's products.
func (r CatalogRepository) Products(ctx context.Context, scope CatalogScope, query ProductQuery) (ProductPage, error) {
	query, err := query.normalize()
	if err != nil {
		return ProductPage{}, err
	}

	where, args := productFilters(query, scope.baseArgs())
	predicate := strings.Join(where, " AND ")

	var total int64
	countSQL := `WITH listing AS (` + eligibleListings + `
		)
		SELECT COUNT(*) FROM listing l WHERE ` + predicate
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return ProductPage{}, readError(err, "count public products")
	}

	limitIndex := len(args) + 1
	args = append(args, query.Page.Limit, query.Page.Offset)
	listSQL := fmt.Sprintf(`WITH listing AS (`+eligibleListings+`
		)
		SELECT
			l.product_slug, l.name, LEFT(l.description, 240),
			l.price_minor, l.price_currency, l.in_stock,
			img.uri, img.alt_text,
			cat.slug, cat.name,
			vc.variant_count
		FROM listing l
		LEFT JOIN LATERAL (
			SELECT uri, alt_text
			FROM media_metadata
			WHERE product_id = l.product_id
			ORDER BY sort_order ASC, id ASC
			LIMIT 1
		) img ON true
		LEFT JOIN LATERAL (
			SELECT c.slug, COALESCE(ct.name, ctf.name, c.slug) AS name
			FROM product_categories pc
			JOIN categories c ON c.id = pc.category_id AND c.status = 'active'
			LEFT JOIN category_translations ct ON ct.category_id = c.id AND ct.locale = $3
			LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.locale = $4
			WHERE pc.product_id = l.product_id
			ORDER BY pc.sort_order ASC, pc.category_id ASC
			LIMIT 1
		) cat ON true
		LEFT JOIN LATERAL (
			SELECT COUNT(*) AS variant_count
			FROM variants v
			WHERE v.product_id = l.product_id AND v.status = 'active'
		) vc ON true
		WHERE %s
		ORDER BY %s
		LIMIT $%d OFFSET $%d
	`, predicate, productOrder(query.Sort), limitIndex, limitIndex+1)

	rows, err := r.pool.Query(ctx, listSQL, args...)
	if err != nil {
		return ProductPage{}, readError(err, "list public products")
	}
	defer rows.Close()

	page := ProductPage{
		Items:  make([]ProductListItem, 0, query.Page.Limit),
		Total:  total,
		Limit:  query.Page.Limit,
		Offset: query.Page.Offset,
	}
	for rows.Next() {
		var (
			item         ProductListItem
			inStock      bool
			imageURI     *string
			imageAlt     *string
			categorySlug *string
			categoryName *string
		)
		if err := rows.Scan(
			&item.Slug, &item.Name, &item.Summary,
			&item.Price.AmountMinor, &item.Price.Currency, &inStock,
			&imageURI, &imageAlt,
			&categorySlug, &categoryName,
			&item.VariantCount,
		); err != nil {
			return ProductPage{}, readError(err, "scan public product")
		}
		item.Availability = availabilityState(inStock)
		if imageURI != nil {
			image := ProductImage{URI: *imageURI}
			if imageAlt != nil {
				image.AltText = *imageAlt
			}
			item.Image = &image
		}
		if categorySlug != nil {
			ref := CategoryRef{Slug: *categorySlug}
			if categoryName != nil {
				ref.Name = *categoryName
			}
			item.Category = &ref
		}
		page.Items = append(page.Items, item)
	}
	if rows.Err() != nil {
		return ProductPage{}, readError(rows.Err(), "iterate public products")
	}
	return page, nil
}

// Search is browse with a keyword. It exists as its own operation so the public
// contract stays stable if search moves to a dedicated read model later.
func (r CatalogRepository) Search(ctx context.Context, scope CatalogScope, keyword string, query ProductQuery) (ProductPage, error) {
	query.Keyword = keyword
	return r.Products(ctx, scope, query)
}

// ProductBySlug returns the product page payload for a slug inside the resolved
// store. A slug that exists globally but is not publicly listed by this store is
// reported as not found.
//
// Detail is assembled with a fixed number of queries (product, images,
// categories, variants+SKUs) rather than one query per related row.
func (r CatalogRepository) ProductBySlug(ctx context.Context, scope CatalogScope, slug string) (ProductDetail, error) {
	slug = strings.TrimSpace(strings.ToLower(slug))
	if slug == "" {
		return ProductDetail{}, fmt.Errorf("%w: product slug is required", ErrInvalidQuery)
	}

	args := append(scope.baseArgs(), slug)
	var (
		detail    ProductDetail
		productID string
		inStock   bool
	)
	err := r.pool.QueryRow(ctx, `WITH listing AS (`+eligibleListings+`
		)
		SELECT l.product_id, l.product_slug, l.name, l.description, l.price_minor, l.price_currency, l.in_stock
		FROM listing l
		WHERE l.product_slug = $5
	`, args...).Scan(
		&productID, &detail.Slug, &detail.Name, &detail.Description,
		&detail.Price.AmountMinor, &detail.Price.Currency, &inStock,
	)
	if err != nil {
		return ProductDetail{}, readError(err, "get public product")
	}
	detail.Availability = availabilityState(inStock)

	if detail.Images, err = r.productImages(ctx, productID); err != nil {
		return ProductDetail{}, err
	}
	if detail.Categories, err = r.productCategories(ctx, scope, productID); err != nil {
		return ProductDetail{}, err
	}
	if detail.Variants, err = r.productVariants(ctx, scope, productID); err != nil {
		return ProductDetail{}, err
	}

	var (
		rawPb   sql.NullString
		rawSecs []byte
	)
	_ = r.pool.QueryRow(ctx, `
		SELECT p.purchase_behavior, p.sections
		FROM seller_listings l
		LEFT JOIN seller_listing_presentations p ON p.seller_listing_id = l.id
		WHERE l.store_id = $1 AND l.product_id = $2
	`, scope.storeID, productID).Scan(&rawPb, &rawSecs)

	pb := "add_to_cart"
	if rawPb.Valid && rawPb.String != "" && rawPb.String != "inherit" {
		pb = rawPb.String
	} else {
		var storeSettingsJSON []byte
		_ = r.pool.QueryRow(ctx, `SELECT settings FROM store_settings WHERE store_id = $1`, scope.storeID).Scan(&storeSettingsJSON)
		if len(storeSettingsJSON) > 0 {
			var st map[string]any
			_ = json.Unmarshal(storeSettingsJSON, &st)
			if b, ok := st["purchase_behavior"].(string); ok && b == "buy_now" {
				pb = "buy_now"
			}
		}
	}
	detail.PurchaseBehavior = pb

	sections := make([]any, 0)
	if len(rawSecs) > 0 {
		var secList []struct {
			ID        string         `json:"id"`
			Type      string         `json:"type"`
			Enabled   bool           `json:"enabled"`
			SortOrder int            `json:"sort_order"`
			Content   map[string]any `json:"content"`
		}
		if err := json.Unmarshal(rawSecs, &secList); err == nil {
			locStr := string(scope.locale)
			fallbackLocStr := string(fallbackLocale(scope.locale))
			for _, s := range secList {
				if !s.Enabled {
					continue
				}
				secData := map[string]any{
					"id":         s.ID,
					"type":       s.Type,
					"sort_order": s.SortOrder,
				}
				if s.Content != nil {
					if locContent, ok := s.Content[locStr]; ok {
						secData["content"] = locContent
					} else if fallbackContent, ok := s.Content[fallbackLocStr]; ok {
						secData["content"] = fallbackContent
					} else {
						secData["content"] = s.Content
					}
				}
				sections = append(sections, secData)
			}
		}
	}
	detail.Sections = sections

	return detail, nil
}

func (r CatalogRepository) productImages(ctx context.Context, productID string) ([]ProductImage, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT uri, alt_text
		FROM media_metadata
		WHERE product_id = $1
		ORDER BY sort_order ASC, id ASC
	`, productID)
	if err != nil {
		return nil, readError(err, "list public product media")
	}
	defer rows.Close()

	images := make([]ProductImage, 0, 4)
	for rows.Next() {
		var image ProductImage
		if err := rows.Scan(&image.URI, &image.AltText); err != nil {
			return nil, readError(err, "scan public product media")
		}
		images = append(images, image)
	}
	return images, readError(rows.Err(), "iterate public product media")
}

func (r CatalogRepository) productCategories(ctx context.Context, scope CatalogScope, productID string) ([]CategoryRef, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.slug, COALESCE(ct.name, ctf.name, c.slug)
		FROM product_categories pc
		JOIN categories c ON c.id = pc.category_id AND c.status = 'active'
		LEFT JOIN category_translations ct ON ct.category_id = c.id AND ct.locale = $2
		LEFT JOIN category_translations ctf ON ctf.category_id = c.id AND ctf.locale = $3
		WHERE pc.product_id = $1
		ORDER BY pc.sort_order ASC, pc.category_id ASC
	`, productID, string(scope.locale), string(fallbackLocale(scope.locale)))
	if err != nil {
		return nil, readError(err, "list public product categories")
	}
	defer rows.Close()

	refs := make([]CategoryRef, 0, 4)
	for rows.Next() {
		var ref CategoryRef
		if err := rows.Scan(&ref.Slug, &ref.Name); err != nil {
			return nil, readError(err, "scan public product category")
		}
		refs = append(refs, ref)
	}
	return refs, readError(rows.Err(), "iterate public product categories")
}

// productVariants returns selectable variants with their SKUs. Availability is
// derived per SKU from the market's active fulfillment locations; on-hand
// quantities, reserved quantities, fulfillment locations, and SKU codes stay
// internal. The SKU id is exposed deliberately as the future cart selection
// handle.
func (r CatalogRepository) productVariants(ctx context.Context, scope CatalogScope, productID string) ([]PublicVariant, error) {
	rows, err := r.pool.Query(ctx, `
		WITH listing AS (`+catalog.CanonicalListingSQL+`)
		SELECT
			v.code,
			sk.id,
			(l.supplier_available AND COALESCE(stock.in_stock, false))
		FROM variants v
		JOIN listing l ON l.product_id = v.product_id
		LEFT JOIN skus sk ON sk.variant_id = v.id AND sk.status = 'active'
		LEFT JOIN LATERAL (
			SELECT EXISTS (
				SELECT 1
				FROM inventory_snapshots inv
				JOIN fulfillment_locations fl
					ON fl.id = inv.fulfillment_location_id
					AND fl.status = 'active'
					AND fl.market_code = l.market_code
				WHERE inv.sku_id = sk.id
					AND (
						(l.supplier_offer_id IS NULL AND fl.store_id = l.store_id AND fl.supplier_id IS NULL)
						OR
						(l.supplier_offer_id IS NOT NULL AND fl.store_id IS NULL AND fl.supplier_id = l.supplier_id)
					)
					AND (inv.on_hand_qty - inv.reserved_qty) > 0
			) AS in_stock
		) stock ON true
		WHERE v.product_id = $3 AND v.status = 'active'
		ORDER BY v.code ASC, sk.id ASC
	`, scope.storeID, scope.marketCode, productID)
	if err != nil {
		return nil, readError(err, "list public product variants")
	}
	defer rows.Close()

	variants := make([]PublicVariant, 0, 4)
	index := map[string]int{}
	for rows.Next() {
		var (
			code    string
			skuID   *string
			inStock bool
		)
		if err := rows.Scan(&code, &skuID, &inStock); err != nil {
			return nil, readError(err, "scan public product variant")
		}
		position, ok := index[code]
		if !ok {
			variants = append(variants, PublicVariant{
				Code:         code,
				Availability: AvailabilityOutOfStock,
				SKUs:         make([]PublicSKU, 0, 2),
			})
			position = len(variants) - 1
			index[code] = position
		}
		if skuID == nil {
			continue
		}
		variants[position].SKUs = append(variants[position].SKUs, PublicSKU{
			ID:           *skuID,
			Availability: availabilityState(inStock),
		})
		if inStock {
			variants[position].Availability = AvailabilityInStock
		}
	}
	return variants, readError(rows.Err(), "iterate public product variants")
}
