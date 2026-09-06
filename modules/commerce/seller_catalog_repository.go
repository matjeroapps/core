package commerce

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/matjeroapps/core/packages/money"
)

type SellerProductListItem struct {
	Product       Product             `json:"product"`
	SellerProduct *SellerProduct      `json:"seller_product,omitempty"`
	Listing       SellerListing       `json:"listing"`
	Price         *SellerListingPrice `json:"price,omitempty"`
	Source        string              `json:"source"` // seller_owned, supplier_backed
}

func (r Repository) CreateSellerProduct(ctx context.Context, sellerID, productID string, sellerCode *string) (SellerProduct, error) {
	if sellerID == "" || productID == "" {
		return SellerProduct{}, ErrInvalidInput
	}

	var created SellerProduct
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO seller_products (id, seller_id, product_id, seller_code)
			VALUES ($1, $2, $3, $4)
			RETURNING created_at, updated_at
		`, id, sellerID, productID, sellerCode).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create seller product")
		}

		created = SellerProduct{
			ID:         id,
			SellerID:   sellerID,
			ProductID:  productID,
			SellerCode: sellerCode,
			CreatedAt:  created.CreatedAt,
			UpdatedAt:  created.UpdatedAt,
		}
		return nil
	})
	return created, err
}

func (r Repository) GetSellerProductByProductID(ctx context.Context, productID string) (SellerProduct, error) {
	if productID == "" {
		return SellerProduct{}, ErrInvalidInput
	}

	var sp SellerProduct
	var sellerCode sql.NullString
	err := r.pool.QueryRow(ctx, `
		SELECT id, seller_id, product_id, seller_code, created_at, updated_at
		FROM seller_products
		WHERE product_id = $1
	`, productID).Scan(&sp.ID, &sp.SellerID, &sp.ProductID, &sellerCode, &sp.CreatedAt, &sp.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SellerProduct{}, ErrNotFound
		}
		return SellerProduct{}, fmt.Errorf("get seller product by product id: %w", err)
	}
	if sellerCode.Valid {
		sp.SellerCode = &sellerCode.String
	}
	return sp, nil
}

func (r Repository) GetSellerProductBySellerAndProduct(ctx context.Context, sellerID, productID string) (SellerProduct, error) {
	if sellerID == "" || productID == "" {
		return SellerProduct{}, ErrInvalidInput
	}

	var sp SellerProduct
	var sellerCode sql.NullString
	err := r.pool.QueryRow(ctx, `
		SELECT id, seller_id, product_id, seller_code, created_at, updated_at
		FROM seller_products
		WHERE seller_id = $1 AND product_id = $2
	`, sellerID, productID).Scan(&sp.ID, &sp.SellerID, &sp.ProductID, &sellerCode, &sp.CreatedAt, &sp.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SellerProduct{}, ErrNotFound
		}
		return SellerProduct{}, fmt.Errorf("get seller product by seller and product: %w", err)
	}
	if sellerCode.Valid {
		sp.SellerCode = &sellerCode.String
	}
	return sp, nil
}

func (r Repository) CreateSellerProductAtomically(ctx context.Context, sellerID, storeID, marketCode string, draft SellerProductDraft) (Product, SellerProduct, SellerListing, error) {
	if sellerID == "" || storeID == "" || marketCode == "" || draft.Slug == "" {
		return Product{}, SellerProduct{}, SellerListing{}, ErrInvalidInput
	}

	var createdProduct Product
	var createdSellerProduct SellerProduct
	var createdListing SellerListing

	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		// 1. Create Product
		productID := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO products (id, slug, status)
			VALUES ($1, $2, 'inactive')
			RETURNING created_at, updated_at
		`, productID, draft.Slug).Scan(&createdProduct.CreatedAt, &createdProduct.UpdatedAt); err != nil {
			return translatePGError(err, "create product atomically")
		}
		createdProduct = Product{
			ID:        productID,
			Slug:      draft.Slug,
			Status:    "inactive",
			CreatedAt: createdProduct.CreatedAt,
			UpdatedAt: createdProduct.UpdatedAt,
		}

		// 2. Create SellerProduct
		spID := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO seller_products (id, seller_id, product_id)
			VALUES ($1, $2, $3)
			RETURNING created_at, updated_at
		`, spID, sellerID, productID).Scan(&createdSellerProduct.CreatedAt, &createdSellerProduct.UpdatedAt); err != nil {
			return translatePGError(err, "create seller product atomically")
		}
		createdSellerProduct = SellerProduct{
			ID:        spID,
			SellerID:  sellerID,
			ProductID: productID,
			CreatedAt: createdSellerProduct.CreatedAt,
			UpdatedAt: createdSellerProduct.UpdatedAt,
		}

		// 3. Translations
		for _, tr := range draft.Translations {
			if tr.Locale == "" || tr.Name == "" {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO product_translations (product_id, locale, name, description)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (product_id, locale)
				DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description
			`, productID, tr.Locale, tr.Name, tr.Description); err != nil {
				return translatePGError(err, "upsert product translation atomically")
			}
		}

		// 4. Categories
		if len(draft.CategoryIDs) > 0 {
			if _, err := tx.Exec(ctx, `DELETE FROM product_categories WHERE product_id = $1`, productID); err != nil {
				return translatePGError(err, "clear categories atomically")
			}
			for _, catID := range draft.CategoryIDs {
				if catID == "" {
					continue
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO product_categories (product_id, category_id)
					VALUES ($1, $2)
					ON CONFLICT DO NOTHING
				`, productID, catID); err != nil {
					return translatePGError(err, "insert product category atomically")
				}
			}
		}

		// 5. Create Seller Listing
		listingID := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO seller_listings (id, store_id, product_id, supplier_offer_id, market_code, status)
			VALUES ($1, $2, $3, NULL, $4, 'inactive')
			RETURNING created_at, updated_at
		`, listingID, storeID, productID, marketCode).Scan(&createdListing.CreatedAt, &createdListing.UpdatedAt); err != nil {
			return translatePGError(err, "create seller listing atomically")
		}
		createdListing = SellerListing{
			ID:         listingID,
			StoreID:    storeID,
			ProductID:  productID,
			MarketCode: marketCode,
			Status:     "inactive",
			CreatedAt:  createdListing.CreatedAt,
			UpdatedAt:  createdListing.UpdatedAt,
		}

		return bumpStorefrontRevisions(ctx, tx, revisionStoreItself, storeID)
	})

	return createdProduct, createdSellerProduct, createdListing, err
}

func (r Repository) UpdateSellerProduct(ctx context.Context, productID string, slug string) (Product, error) {
	if productID == "" || slug == "" {
		return Product{}, ErrInvalidInput
	}

	var updated Product
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			UPDATE products
			SET slug = $2, updated_at = now()
			WHERE id = $1
			RETURNING id, slug, status, created_at, updated_at
		`, productID, slug).Scan(&updated.ID, &updated.Slug, &updated.Status, &updated.CreatedAt, &updated.UpdatedAt); err != nil {
			return translatePGError(err, "update seller product")
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByProduct, productID)
	})
	return updated, err
}

func (r Repository) GetProductByID(ctx context.Context, productID string) (Product, error) {
	if productID == "" {
		return Product{}, ErrInvalidInput
	}

	var p Product
	err := r.pool.QueryRow(ctx, `
		SELECT id, slug, status, created_at, updated_at
		FROM products
		WHERE id = $1
	`, productID).Scan(&p.ID, &p.Slug, &p.Status, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Product{}, ErrNotFound
		}
		return Product{}, fmt.Errorf("get product by id: %w", err)
	}
	return p, nil
}

func (r Repository) GetSellerListingByStoreAndProduct(ctx context.Context, storeID, productID string) (SellerListing, error) {
	if storeID == "" || productID == "" {
		return SellerListing{}, ErrInvalidInput
	}

	var l SellerListing
	var suppOfferID sql.NullString
	err := r.pool.QueryRow(ctx, `
		SELECT id, store_id, product_id, supplier_offer_id, market_code, status, created_at, updated_at
		FROM seller_listings
		WHERE store_id = $1 AND product_id = $2
	`, storeID, productID).Scan(&l.ID, &l.StoreID, &l.ProductID, &suppOfferID, &l.MarketCode, &l.Status, &l.CreatedAt, &l.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SellerListing{}, ErrNotFound
		}
		return SellerListing{}, fmt.Errorf("get seller listing by store and product: %w", err)
	}
	if suppOfferID.Valid {
		l.SupplierOfferID = &suppOfferID.String
	}
	return l, nil
}

func (r Repository) GetSellerListingPrice(ctx context.Context, sellerListingID string) (SellerListingPrice, error) {
	if sellerListingID == "" {
		return SellerListingPrice{}, ErrInvalidInput
	}

	var pr SellerListingPrice
	var amountMinor int64
	var currency string
	err := r.pool.QueryRow(ctx, `
		SELECT id, seller_listing_id, amount_minor, currency_code, is_current, created_at, updated_at
		FROM seller_listing_prices
		WHERE seller_listing_id = $1 AND is_current = true
	`, sellerListingID).Scan(&pr.ID, &pr.SellerListingID, &amountMinor, &currency, &pr.IsCurrent, &pr.CreatedAt, &pr.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SellerListingPrice{}, ErrNotFound
		}
		return SellerListingPrice{}, fmt.Errorf("get seller listing price: %w", err)
	}
	mObj, _ := money.New(amountMinor, currency)
	pr.Price = mObj
	return pr, nil
}

func (r Repository) GetProductTranslations(ctx context.Context, productID string) ([]ProductTranslation, error) {
	if productID == "" {
		return nil, ErrInvalidInput
	}

	rows, err := r.pool.Query(ctx, `
		SELECT product_id, locale, name, description
		FROM product_translations
		WHERE product_id = $1
		ORDER BY locale ASC
	`, productID)
	if err != nil {
		return nil, fmt.Errorf("query product translations: %w", err)
	}
	defer rows.Close()

	var result []ProductTranslation
	for rows.Next() {
		var tr ProductTranslation
		if err := rows.Scan(&tr.ProductID, &tr.Locale, &tr.Name, &tr.Description); err != nil {
			return nil, fmt.Errorf("scan product translation: %w", err)
		}
		result = append(result, tr)
	}
	return result, nil
}

func (r Repository) GetProductCategories(ctx context.Context, productID string) ([]Category, error) {
	if productID == "" {
		return nil, ErrInvalidInput
	}

	rows, err := r.pool.Query(ctx, `
		SELECT c.id, c.parent_category_id, c.slug, c.status, c.created_at, c.updated_at
		FROM categories c
		JOIN product_categories pc ON pc.category_id = c.id
		WHERE pc.product_id = $1
		ORDER BY c.slug ASC
	`, productID)
	if err != nil {
		return nil, fmt.Errorf("query product categories: %w", err)
	}
	defer rows.Close()

	var result []Category
	for rows.Next() {
		var c Category
		var parentID sql.NullString
		if err := rows.Scan(&c.ID, &parentID, &c.Slug, &c.Status, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan product category: %w", err)
		}
		if parentID.Valid {
			c.ParentCategoryID = &parentID.String
		}
		result = append(result, c)
	}
	return result, nil
}

func (r Repository) ListVariantsByProductID(ctx context.Context, productID string) ([]Variant, error) {
	if productID == "" {
		return nil, ErrInvalidInput
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, product_id, code, status, created_at, updated_at
		FROM variants
		WHERE product_id = $1
		ORDER BY created_at ASC, code ASC
	`, productID)
	if err != nil {
		return nil, fmt.Errorf("query variants: %w", err)
	}
	defer rows.Close()

	var result []Variant
	for rows.Next() {
		var v Variant
		if err := rows.Scan(&v.ID, &v.ProductID, &v.Code, &v.Status, &v.CreatedAt, &v.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan variant: %w", err)
		}
		result = append(result, v)
	}
	return result, nil
}

func (r Repository) GetVariantByID(ctx context.Context, variantID string) (Variant, error) {
	if variantID == "" {
		return Variant{}, ErrInvalidInput
	}

	var v Variant
	err := r.pool.QueryRow(ctx, `
		SELECT id, product_id, code, status, created_at, updated_at
		FROM variants
		WHERE id = $1
	`, variantID).Scan(&v.ID, &v.ProductID, &v.Code, &v.Status, &v.CreatedAt, &v.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Variant{}, ErrNotFound
		}
		return Variant{}, fmt.Errorf("get variant by id: %w", err)
	}
	return v, nil
}

func (r Repository) UpdateVariant(ctx context.Context, variantID, code, status string) (Variant, error) {
	if variantID == "" || code == "" || status == "" {
		return Variant{}, ErrInvalidInput
	}

	var updated Variant
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			UPDATE variants
			SET code = $2, status = $3, updated_at = now()
			WHERE id = $1
			RETURNING id, product_id, code, status, created_at, updated_at
		`, variantID, code, status).Scan(&updated.ID, &updated.ProductID, &updated.Code, &updated.Status, &updated.CreatedAt, &updated.UpdatedAt); err != nil {
			return translatePGError(err, "update variant")
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByVariant, variantID)
	})
	return updated, err
}

func (r Repository) ListSKUsByVariantID(ctx context.Context, variantID string) ([]SKU, error) {
	if variantID == "" {
		return nil, ErrInvalidInput
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, variant_id, code, barcode, status, created_at, updated_at
		FROM skus
		WHERE variant_id = $1
		ORDER BY created_at ASC, code ASC
	`, variantID)
	if err != nil {
		return nil, fmt.Errorf("query skus: %w", err)
	}
	defer rows.Close()

	var result []SKU
	for rows.Next() {
		var s SKU
		var bc sql.NullString
		if err := rows.Scan(&s.ID, &s.VariantID, &s.Code, &bc, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan sku: %w", err)
		}
		if bc.Valid {
			s.Barcode = bc.String
		}
		result = append(result, s)
	}
	return result, nil
}

func (r Repository) ListSKUsByProductID(ctx context.Context, productID string) ([]SKU, error) {
	if productID == "" {
		return nil, ErrInvalidInput
	}

	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.variant_id, s.code, s.barcode, s.status, s.created_at, s.updated_at
		FROM skus s
		JOIN variants v ON v.id = s.variant_id
		WHERE v.product_id = $1
		ORDER BY s.created_at ASC, s.code ASC
	`, productID)
	if err != nil {
		return nil, fmt.Errorf("query skus by product id: %w", err)
	}
	defer rows.Close()

	var result []SKU
	for rows.Next() {
		var s SKU
		var bc sql.NullString
		if err := rows.Scan(&s.ID, &s.VariantID, &s.Code, &bc, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan sku: %w", err)
		}
		if bc.Valid {
			s.Barcode = bc.String
		}
		result = append(result, s)
	}
	return result, nil
}

func (r Repository) GetSKUByID(ctx context.Context, skuID string) (SKU, error) {
	if skuID == "" {
		return SKU{}, ErrInvalidInput
	}

	var s SKU
	var bc sql.NullString
	err := r.pool.QueryRow(ctx, `
		SELECT id, variant_id, code, barcode, status, created_at, updated_at
		FROM skus
		WHERE id = $1
	`, skuID).Scan(&s.ID, &s.VariantID, &s.Code, &bc, &s.Status, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SKU{}, ErrNotFound
		}
		return SKU{}, fmt.Errorf("get sku by id: %w", err)
	}
	if bc.Valid {
		s.Barcode = bc.String
	}
	return s, nil
}

func (r Repository) UpdateSKU(ctx context.Context, skuID, code, barcode, status string) (SKU, error) {
	if skuID == "" || code == "" || status == "" {
		return SKU{}, ErrInvalidInput
	}

	var updated SKU
	var bc sql.NullString
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var barcodeParam *string
		if barcode != "" {
			barcodeParam = &barcode
		}
		if err := tx.QueryRow(ctx, `
			UPDATE skus
			SET code = $2, barcode = $3, status = $4, updated_at = now()
			WHERE id = $1
			RETURNING id, variant_id, code, barcode, status, created_at, updated_at
		`, skuID, code, barcodeParam, status).Scan(&updated.ID, &updated.VariantID, &updated.Code, &bc, &updated.Status, &updated.CreatedAt, &updated.UpdatedAt); err != nil {
			return translatePGError(err, "update sku")
		}
		if bc.Valid {
			updated.Barcode = bc.String
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresBySKU, skuID)
	})
	return updated, err
}

func (r Repository) ListMediaByProductID(ctx context.Context, productID string) ([]MediaMetadata, error) {
	if productID == "" {
		return nil, ErrInvalidInput
	}

	rows, err := r.pool.Query(ctx, `
		SELECT id, product_id, media_type, uri, alt_text, sort_order, metadata, storage_key, is_primary, created_at, updated_at
		FROM media_metadata
		WHERE product_id = $1
		ORDER BY sort_order ASC, created_at ASC
	`, productID)
	if err != nil {
		return nil, fmt.Errorf("query media metadata: %w", err)
	}
	defer rows.Close()

	var result []MediaMetadata
	for rows.Next() {
		var m MediaMetadata
		var metadataJSON []byte
		var sk sql.NullString
		if err := rows.Scan(&m.ID, &m.ProductID, &m.MediaType, &m.URI, &m.AltText, &m.SortOrder, &metadataJSON, &sk, &m.IsPrimary, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan media metadata: %w", err)
		}
		if len(metadataJSON) > 0 {
			_ = json.Unmarshal(metadataJSON, &m.Metadata)
		}
		if sk.Valid {
			m.StorageKey = &sk.String
		}
		result = append(result, m)
	}
	return result, nil
}

func (r Repository) GetMediaMetadataByID(ctx context.Context, mediaID string) (MediaMetadata, error) {
	if mediaID == "" {
		return MediaMetadata{}, ErrInvalidInput
	}

	var m MediaMetadata
	var metadataJSON []byte
	var sk sql.NullString
	err := r.pool.QueryRow(ctx, `
		SELECT id, product_id, media_type, uri, alt_text, sort_order, metadata, storage_key, is_primary, created_at, updated_at
		FROM media_metadata
		WHERE id = $1
	`, mediaID).Scan(&m.ID, &m.ProductID, &m.MediaType, &m.URI, &m.AltText, &m.SortOrder, &metadataJSON, &sk, &m.IsPrimary, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MediaMetadata{}, ErrNotFound
		}
		return MediaMetadata{}, fmt.Errorf("get media metadata by id: %w", err)
	}
	if len(metadataJSON) > 0 {
		_ = json.Unmarshal(metadataJSON, &m.Metadata)
	}
	if sk.Valid {
		m.StorageKey = &sk.String
	}
	return m, nil
}

func (r Repository) CreateMediaMetadata(ctx context.Context, m MediaMetadata) (MediaMetadata, error) {
	if m.ProductID == "" || m.MediaType == "" || m.URI == "" {
		return MediaMetadata{}, ErrInvalidInput
	}

	var created MediaMetadata
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		metadataJSON, _ := json.Marshal(m.Metadata)
		if m.IsPrimary {
			if _, err := tx.Exec(ctx, `UPDATE media_metadata SET is_primary = false WHERE product_id = $1`, m.ProductID); err != nil {
				return translatePGError(err, "clear previous primary media")
			}
		}

		if m.StorageKey != nil && *m.StorageKey != "" {
			if err := tx.QueryRow(ctx, `
				INSERT INTO media_metadata (id, product_id, media_type, uri, alt_text, sort_order, metadata, storage_key, is_primary)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				RETURNING created_at, updated_at
			`, id, m.ProductID, m.MediaType, m.URI, m.AltText, m.SortOrder, metadataJSON, *m.StorageKey, m.IsPrimary).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
				return translatePGError(err, "create media metadata")
			}
		} else {
			if err := tx.QueryRow(ctx, `
				INSERT INTO media_metadata (id, product_id, media_type, uri, alt_text, sort_order, metadata, is_primary)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				RETURNING created_at, updated_at
			`, id, m.ProductID, m.MediaType, m.URI, m.AltText, m.SortOrder, metadataJSON, m.IsPrimary).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
				return translatePGError(err, "create media metadata")
			}
		}

		created = MediaMetadata{
			ID:         id,
			ProductID:  m.ProductID,
			MediaType:  m.MediaType,
			URI:        m.URI,
			AltText:    m.AltText,
			SortOrder:  m.SortOrder,
			Metadata:   m.Metadata,
			StorageKey: m.StorageKey,
			IsPrimary:  m.IsPrimary,
			CreatedAt:  created.CreatedAt,
			UpdatedAt:  created.UpdatedAt,
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByProduct, m.ProductID)
	})

	return created, err
}

func (r Repository) UpdateMediaMetadata(ctx context.Context, m MediaMetadata) (MediaMetadata, error) {
	if m.ID == "" || m.ProductID == "" {
		return MediaMetadata{}, ErrInvalidInput
	}

	var updated MediaMetadata
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if m.IsPrimary {
			if _, err := tx.Exec(ctx, `UPDATE media_metadata SET is_primary = false WHERE product_id = $1 AND id != $2`, m.ProductID, m.ID); err != nil {
				return translatePGError(err, "clear previous primary media on update")
			}
		}

		var metadataJSON []byte
		if m.Metadata != nil {
			metadataJSON, _ = json.Marshal(m.Metadata)
		}

		var sk sql.NullString
		if err := tx.QueryRow(ctx, `
			UPDATE media_metadata
			SET alt_text = $3, sort_order = $4, is_primary = $5, updated_at = now()
			WHERE id = $1 AND product_id = $2
			RETURNING id, product_id, media_type, uri, alt_text, sort_order, metadata, storage_key, is_primary, created_at, updated_at
		`, m.ID, m.ProductID, m.AltText, m.SortOrder, m.IsPrimary).Scan(&updated.ID, &updated.ProductID, &updated.MediaType, &updated.URI, &updated.AltText, &updated.SortOrder, &metadataJSON, &sk, &updated.IsPrimary, &updated.CreatedAt, &updated.UpdatedAt); err != nil {
			return translatePGError(err, "update media metadata")
		}

		if len(metadataJSON) > 0 {
			_ = json.Unmarshal(metadataJSON, &updated.Metadata)
		}
		if sk.Valid {
			updated.StorageKey = &sk.String
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByProduct, m.ProductID)
	})
	return updated, err
}

func (r Repository) DeleteMediaMetadata(ctx context.Context, productID, mediaID string) error {
	if productID == "" || mediaID == "" {
		return ErrInvalidInput
	}

	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		res, err := tx.Exec(ctx, `DELETE FROM media_metadata WHERE id = $1 AND product_id = $2`, mediaID, productID)
		if err != nil {
			return translatePGError(err, "delete media metadata")
		}
		if res.RowsAffected() == 0 {
			return ErrNotFound
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByProduct, productID)
	})
}

func (r Repository) GetSellerListingPresentation(ctx context.Context, listingID string) (SellerListingPresentation, error) {
	if listingID == "" {
		return SellerListingPresentation{}, ErrInvalidInput
	}

	var pres SellerListingPresentation
	var sectionsJSON []byte
	err := r.pool.QueryRow(ctx, `
		SELECT seller_listing_id, schema_version, purchase_behavior, sections, created_at, updated_at
		FROM seller_listing_presentations
		WHERE seller_listing_id = $1
	`, listingID).Scan(&pres.SellerListingID, &pres.SchemaVersion, &pres.PurchaseBehavior, &sectionsJSON, &pres.CreatedAt, &pres.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return SellerListingPresentation{
				SellerListingID:  listingID,
				SchemaVersion:    1,
				PurchaseBehavior: "inherit",
				Sections:         []ProductPageSection{},
			}, nil
		}
		return SellerListingPresentation{}, fmt.Errorf("get seller listing presentation: %w", err)
	}

	if len(sectionsJSON) > 0 {
		_ = json.Unmarshal(sectionsJSON, &pres.Sections)
	}
	if pres.Sections == nil {
		pres.Sections = []ProductPageSection{}
	}
	return pres, nil
}

func (r Repository) UpsertSellerListingPresentation(ctx context.Context, pres SellerListingPresentation) (SellerListingPresentation, error) {
	if pres.SellerListingID == "" || pres.PurchaseBehavior == "" {
		return SellerListingPresentation{}, ErrInvalidInput
	}

	if pres.SchemaVersion <= 0 {
		pres.SchemaVersion = 1
	}

	sectionsJSON, err := json.Marshal(pres.Sections)
	if err != nil {
		return SellerListingPresentation{}, fmt.Errorf("marshal presentation sections: %w", err)
	}

	var updated SellerListingPresentation
	var outSections []byte
	err = r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO seller_listing_presentations (seller_listing_id, schema_version, purchase_behavior, sections)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (seller_listing_id)
			DO UPDATE SET schema_version = EXCLUDED.schema_version, purchase_behavior = EXCLUDED.purchase_behavior, sections = EXCLUDED.sections, updated_at = now()
			RETURNING seller_listing_id, schema_version, purchase_behavior, sections, created_at, updated_at
		`, pres.SellerListingID, pres.SchemaVersion, pres.PurchaseBehavior, sectionsJSON).Scan(&updated.SellerListingID, &updated.SchemaVersion, &updated.PurchaseBehavior, &outSections, &updated.CreatedAt, &updated.UpdatedAt); err != nil {
			return translatePGError(err, "upsert seller listing presentation")
		}

		if len(outSections) > 0 {
			_ = json.Unmarshal(outSections, &updated.Sections)
		}
		if updated.Sections == nil {
			updated.Sections = []ProductPageSection{}
		}

		return bumpStorefrontRevisions(ctx, tx, revisionStoresByListing, pres.SellerListingID)
	})

	return updated, err
}

func (r Repository) ListStoreFulfillmentLocations(ctx context.Context, storeID string) ([]FulfillmentLocation, error) {
	if storeID == "" {
		return nil, ErrInvalidInput
	}

	rows, err := r.pool.Query(ctx, `
		SELECT supplier_id, store_id, id, supplier_market_id, market_code, code, name, location_type, status, created_at, updated_at
		FROM fulfillment_locations
		WHERE store_id = $1 AND supplier_id IS NULL
		ORDER BY created_at ASC
	`, storeID)
	if err != nil {
		return nil, fmt.Errorf("query store fulfillment locations: %w", err)
	}
	defer rows.Close()

	var result []FulfillmentLocation
	for rows.Next() {
		var fl FulfillmentLocation
		var suppID, storeIDVal, suppMarketID sql.NullString
		if err := rows.Scan(&suppID, &storeIDVal, &fl.ID, &suppMarketID, &fl.MarketCode, &fl.Code, &fl.Name, &fl.LocationType, &fl.Status, &fl.CreatedAt, &fl.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan fulfillment location: %w", err)
		}
		if suppID.Valid {
			fl.SupplierID = suppID.String
		}
		if storeIDVal.Valid {
			fl.StoreID = storeIDVal.String
		}
		if suppMarketID.Valid {
			fl.SupplierMarketID = suppMarketID.String
		}
		result = append(result, fl)
	}
	return result, nil
}

func (r Repository) ListStoreInventorySnapshots(ctx context.Context, storeID string) ([]InventorySnapshot, error) {
	if storeID == "" {
		return nil, ErrInvalidInput
	}

	rows, err := r.pool.Query(ctx, `
		SELECT s.id, s.fulfillment_location_id, s.sku_id, s.on_hand_qty, s.reserved_qty, s.version, s.created_at, s.updated_at
		FROM inventory_snapshots s
		JOIN fulfillment_locations fl ON fl.id = s.fulfillment_location_id
		WHERE fl.store_id = $1 AND fl.supplier_id IS NULL
		ORDER BY s.created_at ASC
	`, storeID)
	if err != nil {
		return nil, fmt.Errorf("query store inventory snapshots: %w", err)
	}
	defer rows.Close()

	var result []InventorySnapshot
	for rows.Next() {
		var s InventorySnapshot
		if err := rows.Scan(&s.ID, &s.FulfillmentLocationID, &s.SKUID, &s.OnHandQty, &s.ReservedQty, &s.Version, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan inventory snapshot: %w", err)
		}
		result = append(result, s)
	}
	return result, nil
}

func (r Repository) ListStoreProducts(ctx context.Context, storeID, statusFilter, sourceFilter, queryFilter string, limit, offset int) ([]SellerProductListItem, int, error) {
	if storeID == "" {
		return nil, 0, ErrInvalidInput
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	sb := strings.Builder{}
	sb.WriteString(`
		SELECT
			p.id, p.slug, p.status, p.created_at, p.updated_at,
			sp.id, sp.seller_id, sp.product_id, sp.seller_code, sp.created_at, sp.updated_at,
			l.id, l.store_id, l.product_id, l.supplier_offer_id, l.market_code, l.status, l.created_at, l.updated_at,
			pr.id, pr.seller_listing_id, pr.amount_minor, pr.currency_code, pr.is_current, pr.created_at, pr.updated_at,
			COUNT(*) OVER() AS total_count
		FROM seller_listings l
		JOIN products p ON p.id = l.product_id
		LEFT JOIN seller_products sp ON sp.product_id = p.id
		LEFT JOIN seller_listing_prices pr ON pr.seller_listing_id = l.id AND pr.is_current = true
		WHERE l.store_id = $1
	`)

	args := []any{storeID}
	argCount := 1

	if statusFilter != "" {
		argCount++
		sb.WriteString(fmt.Sprintf(" AND l.status = $%d", argCount))
		args = append(args, statusFilter)
	}

	if sourceFilter == "seller_owned" {
		sb.WriteString(" AND l.supplier_offer_id IS NULL AND sp.id IS NOT NULL")
	} else if sourceFilter == "supplier_backed" {
		sb.WriteString(" AND l.supplier_offer_id IS NOT NULL")
	}

	if queryFilter != "" {
		argCount++
		sb.WriteString(fmt.Sprintf(" AND (p.slug ILIKE $%d OR sp.seller_code ILIKE $%d)", argCount, argCount))
		args = append(args, "%"+queryFilter+"%")
	}

	argCount++
	sb.WriteString(fmt.Sprintf(" ORDER BY l.created_at DESC, l.id DESC LIMIT $%d", argCount))
	args = append(args, limit)

	argCount++
	sb.WriteString(fmt.Sprintf(" OFFSET $%d", argCount))
	args = append(args, offset)

	rows, err := r.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query store products: %w", err)
	}
	defer rows.Close()

	var items []SellerProductListItem
	var totalCount int

	for rows.Next() {
		var item SellerProductListItem
		var spID, spSellerID, spProductID, spSellerCode sql.NullString
		var spCreatedAt, spUpdatedAt sql.NullTime

		var suppOfferID sql.NullString

		var prID, prListingID, prCurrency sql.NullString
		var prAmountMinor sql.NullInt64
		var prIsCurrent sql.NullBool
		var prCreatedAt, prUpdatedAt sql.NullTime

		if err := rows.Scan(
			&item.Product.ID, &item.Product.Slug, &item.Product.Status, &item.Product.CreatedAt, &item.Product.UpdatedAt,
			&spID, &spSellerID, &spProductID, &spSellerCode, &spCreatedAt, &spUpdatedAt,
			&item.Listing.ID, &item.Listing.StoreID, &item.Listing.ProductID, &suppOfferID, &item.Listing.MarketCode, &item.Listing.Status, &item.Listing.CreatedAt, &item.Listing.UpdatedAt,
			&prID, &prListingID, &prAmountMinor, &prCurrency, &prIsCurrent, &prCreatedAt, &prUpdatedAt,
			&totalCount,
		); err != nil {
			return nil, 0, fmt.Errorf("scan store product list item: %w", err)
		}

		if suppOfferID.Valid {
			item.Listing.SupplierOfferID = &suppOfferID.String
			item.Source = "supplier_backed"
		} else {
			item.Source = "seller_owned"
		}

		if spID.Valid {
			sp := SellerProduct{
				ID:        spID.String,
				SellerID:  spSellerID.String,
				ProductID: spProductID.String,
				CreatedAt: spCreatedAt.Time,
				UpdatedAt: spUpdatedAt.Time,
			}
			if spSellerCode.Valid {
				sp.SellerCode = &spSellerCode.String
			}
			item.SellerProduct = &sp
		}

		if prID.Valid {
			priceObj, _ := money.New(prAmountMinor.Int64, prCurrency.String)
			item.Price = &SellerListingPrice{
				ID:              prID.String,
				SellerListingID: prListingID.String,
				Price:           priceObj,
				IsCurrent:       prIsCurrent.Bool,
				CreatedAt:       prCreatedAt.Time,
				UpdatedAt:       prUpdatedAt.Time,
			}
		}

		items = append(items, item)
	}

	return items, totalCount, nil
}

func (r Repository) ListStoreOrders(ctx context.Context, storeID, statusFilter string, limit, offset int) ([]Order, int, error) {
	if storeID == "" {
		return nil, 0, ErrInvalidInput
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	if offset < 0 {
		offset = 0
	}

	sb := strings.Builder{}
	sb.WriteString(`
		SELECT o.id, o.store_id, o.order_number, o.status, o.total_minor, o.currency_code, o.created_at, o.updated_at, o.confirmation_deadline_at,
		       COUNT(*) OVER() AS total_count
		FROM orders o
		WHERE o.store_id = $1
	`)

	args := []any{storeID}
	argCount := 1

	if statusFilter != "" {
		argCount++
		sb.WriteString(fmt.Sprintf(" AND o.status = $%d", argCount))
		args = append(args, statusFilter)
	}

	argCount++
	sb.WriteString(fmt.Sprintf(" ORDER BY o.created_at DESC, o.id DESC LIMIT $%d", argCount))
	args = append(args, limit)

	argCount++
	sb.WriteString(fmt.Sprintf(" OFFSET $%d", argCount))
	args = append(args, offset)

	rows, err := r.pool.Query(ctx, sb.String(), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("query store orders: %w", err)
	}
	defer rows.Close()

	var orders []Order
	var totalCount int

	for rows.Next() {
		var o Order
		var deadline sql.NullTime

		if err := rows.Scan(&o.ID, &o.StoreID, &o.OrderNumber, &o.Status, &o.TotalMinor, &o.CurrencyCode, &o.CreatedAt, &o.UpdatedAt, &deadline, &totalCount); err != nil {
			return nil, 0, fmt.Errorf("scan store order: %w", err)
		}

		if deadline.Valid {
			o.ConfirmationDeadlineAt = deadline.Time
		}

		orders = append(orders, o)
	}

	return orders, totalCount, nil
}

func (r Repository) PublishSellerProduct(ctx context.Context, storeID, productID string) error {
	if storeID == "" || productID == "" {
		return ErrInvalidInput
	}

	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE products SET status = 'active', updated_at = now() WHERE id = $1
		`, productID); err != nil {
			return translatePGError(err, "activate product")
		}

		res, err := tx.Exec(ctx, `
			UPDATE seller_listings SET status = 'active', updated_at = now() WHERE store_id = $1 AND product_id = $2
		`, storeID, productID)
		if err != nil {
			return translatePGError(err, "activate listing")
		}
		if res.RowsAffected() == 0 {
			return ErrNotFound
		}

		return bumpStorefrontRevisions(ctx, tx, revisionStoreItself, storeID)
	})
}

func (r Repository) UnpublishSellerProduct(ctx context.Context, storeID, productID string) error {
	if storeID == "" || productID == "" {
		return ErrInvalidInput
	}

	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		res, err := tx.Exec(ctx, `
			UPDATE seller_listings SET status = 'inactive', updated_at = now() WHERE store_id = $1 AND product_id = $2
		`, storeID, productID)
		if err != nil {
			return translatePGError(err, "deactivate listing")
		}
		if res.RowsAffected() == 0 {
			return ErrNotFound
		}

		return bumpStorefrontRevisions(ctx, tx, revisionStoreItself, storeID)
	})
}

func (r Repository) CreateMediaUploadIntent(ctx context.Context, intent MediaUploadIntent) (MediaUploadIntent, error) {
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO media_upload_intents (seller_id, store_id, product_id, storage_key, content_type, max_bytes, token_digest, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id, created_at`,
			intent.SellerID, intent.StoreID, intent.ProductID,
			intent.StorageKey, intent.ContentType, intent.MaxBytes,
			intent.TokenDigest, intent.ExpiresAt,
		)
		return row.Scan(&intent.ID, &intent.CreatedAt)
	})
	if err != nil {
		return MediaUploadIntent{}, err
	}
	return intent, nil
}

// CompleteMediaUpload inserts the media metadata row and marks the upload
// intent completed as one transaction. A concurrent or retried completion of
// the same intent is rejected by the storage-key unique index on
// media_metadata and by the completed_at IS NULL guard on the intent update,
// so exactly one media record can ever be created per upload intent.
func (r Repository) CompleteMediaUpload(ctx context.Context, m MediaMetadata, intentID string) (MediaMetadata, error) {
	if m.ProductID == "" || m.MediaType == "" || m.URI == "" || intentID == "" {
		return MediaMetadata{}, ErrInvalidInput
	}

	var created MediaMetadata
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := uuid.NewString()
		metadataJSON, _ := json.Marshal(m.Metadata)
		if m.IsPrimary {
			if _, err := tx.Exec(ctx, `UPDATE media_metadata SET is_primary = false WHERE product_id = $1`, m.ProductID); err != nil {
				return translatePGError(err, "clear previous primary media")
			}
		}

		if err := tx.QueryRow(ctx, `
			INSERT INTO media_metadata (id, product_id, media_type, uri, alt_text, sort_order, metadata, storage_key, is_primary)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING created_at, updated_at
		`, id, m.ProductID, m.MediaType, m.URI, m.AltText, m.SortOrder, metadataJSON, m.StorageKey, m.IsPrimary).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create media metadata on upload completion")
		}

		res, err := tx.Exec(ctx, `
			UPDATE media_upload_intents SET completed_at = now()
			WHERE id = $1 AND completed_at IS NULL
		`, intentID)
		if err != nil {
			return translatePGError(err, "mark upload intent complete")
		}
		if res.RowsAffected() == 0 {
			return ErrConflict
		}

		created = MediaMetadata{
			ID:         id,
			ProductID:  m.ProductID,
			MediaType:  m.MediaType,
			URI:        m.URI,
			AltText:    m.AltText,
			SortOrder:  m.SortOrder,
			Metadata:   m.Metadata,
			StorageKey: m.StorageKey,
			IsPrimary:  m.IsPrimary,
			CreatedAt:  created.CreatedAt,
			UpdatedAt:  created.UpdatedAt,
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByProduct, m.ProductID)
	})

	return created, err
}

func (r Repository) GetMediaMetadataByStorageKey(ctx context.Context, productID, storageKey string) (MediaMetadata, error) {
	if productID == "" || storageKey == "" {
		return MediaMetadata{}, ErrInvalidInput
	}

	var m MediaMetadata
	var metadataJSON []byte
	err := r.pool.QueryRow(ctx, `
		SELECT id, product_id, media_type, uri, alt_text, sort_order, metadata, storage_key, is_primary, created_at, updated_at
		FROM media_metadata
		WHERE product_id = $1 AND storage_key = $2
	`, productID, storageKey).Scan(&m.ID, &m.ProductID, &m.MediaType, &m.URI, &m.AltText, &m.SortOrder, &metadataJSON, &m.StorageKey, &m.IsPrimary, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MediaMetadata{}, ErrNotFound
		}
		return MediaMetadata{}, fmt.Errorf("get media metadata by storage key: %w", err)
	}
	if len(metadataJSON) > 0 {
		_ = json.Unmarshal(metadataJSON, &m.Metadata)
	}
	return m, nil
}

// CreateSKUReplacingActive enforces the 1-active-SKU-per-variant invariant
// inside one transaction: creating an active SKU first deactivates the
// currently active SKU of the same variant. The partial unique index
// skus_variant_active_uidx is the concurrency backstop: two concurrent
// requests can never both end up active.
func (r Repository) CreateSKUReplacingActive(ctx context.Context, variantID, code, barcode, status string) (SKU, error) {
	if variantID == "" || code == "" || status == "" {
		return SKU{}, ErrInvalidInput
	}

	var created SKU
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if status == "active" {
			if _, err := tx.Exec(ctx, `
				UPDATE skus SET status = 'inactive', updated_at = now()
				WHERE variant_id = $1 AND status = 'active'
			`, variantID); err != nil {
				return translatePGError(err, "deactivate previous active sku")
			}
		}

		var barcodeParam *string
		if barcode != "" {
			barcodeParam = &barcode
		}
		id := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO skus (id, variant_id, code, barcode, status)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING created_at, updated_at
		`, id, variantID, code, barcodeParam, status).Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create sku")
		}

		created = SKU{
			ID:        id,
			VariantID: variantID,
			Code:      code,
			Status:    status,
			CreatedAt: created.CreatedAt,
			UpdatedAt: created.UpdatedAt,
		}
		if barcodeParam != nil {
			created.Barcode = barcode
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByVariant, variantID)
	})

	return created, err
}

// UpdateSKUReplacingActive updates a SKU and, when the target status is
// active, deactivates the other active SKUs of the same variant in the same
// transaction.
func (r Repository) UpdateSKUReplacingActive(ctx context.Context, skuID, variantID, code, barcode, status string) (SKU, error) {
	if skuID == "" || variantID == "" || code == "" || status == "" {
		return SKU{}, ErrInvalidInput
	}

	var updated SKU
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if status == "active" {
			if _, err := tx.Exec(ctx, `
				UPDATE skus SET status = 'inactive', updated_at = now()
				WHERE variant_id = $1 AND status = 'active' AND id != $2
			`, variantID, skuID); err != nil {
				return translatePGError(err, "deactivate other active skus")
			}
		}

		var barcodeParam *string
		if barcode != "" {
			barcodeParam = &barcode
		}
		if err := tx.QueryRow(ctx, `
			UPDATE skus
			SET code = $3, barcode = $4, status = $5, updated_at = now()
			WHERE id = $1 AND variant_id = $2
			RETURNING created_at, updated_at
		`, skuID, variantID, code, barcodeParam, status).Scan(&updated.CreatedAt, &updated.UpdatedAt); err != nil {
			return translatePGError(err, "update sku")
		}

		updated = SKU{
			ID:        skuID,
			VariantID: variantID,
			Code:      code,
			Status:    status,
			CreatedAt: updated.CreatedAt,
			UpdatedAt: updated.UpdatedAt,
		}
		if barcodeParam != nil {
			updated.Barcode = barcode
		}
		return bumpStorefrontRevisions(ctx, tx, revisionStoresByVariant, variantID)
	})

	return updated, err
}

// PublishSellerProductAtomically activates the Product and its canonical
// Listing only if the full sellable topology still holds, revalidated inside
// the publish transaction while the relevant rows are locked. Lock order is
// deterministic: product -> listing -> skus.
func (r Repository) PublishSellerProductAtomically(ctx context.Context, storeID, productID, marketCode, expectedCurrency string) error {
	if storeID == "" || productID == "" || marketCode == "" || expectedCurrency == "" {
		return ErrInvalidInput
	}

	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var storeStatus string
		if err := tx.QueryRow(ctx, `SELECT status FROM stores WHERE id = $1 FOR UPDATE`, storeID).Scan(&storeStatus); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return translatePGError(err, "lock store for publish")
		}
		if storeStatus != "active" {
			return fmt.Errorf("%w: store is not active", ErrInvalidInput)
		}

		if _, err := tx.Exec(ctx, `SELECT id FROM products WHERE id = $1 FOR UPDATE`, productID); err != nil {
			return translatePGError(err, "lock product for publish")
		}

		var listingID, listingMarket string
		err := tx.QueryRow(ctx, `
			SELECT id, market_code FROM seller_listings
			WHERE store_id = $1 AND product_id = $2
			FOR UPDATE
		`, storeID, productID).Scan(&listingID, &listingMarket)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return translatePGError(err, "lock listing for publish")
		}
		if listingMarket != marketCode {
			return fmt.Errorf("%w: listing market %s does not match store market %s", ErrInvalidInput, listingMarket, marketCode)
		}

		var reasons []string

		var hasName bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM product_translations
				WHERE product_id = $1 AND COALESCE(TRIM(name), '') <> ''
			)
		`, productID).Scan(&hasName); err != nil {
			return translatePGError(err, "check product translations for publish")
		}
		if !hasName {
			reasons = append(reasons, "Product title/translation is missing")
		}

		// Lock SKU rows of the product before evaluating the topology.
		if _, err := tx.Exec(ctx, `
			SELECT sk.id FROM skus sk
			JOIN variants v ON v.id = sk.variant_id
			WHERE v.product_id = $1
			FOR UPDATE OF sk
		`, productID); err != nil {
			return translatePGError(err, "lock skus for publish")
		}

		rows, err := tx.Query(ctx, `
			SELECT v.id, COUNT(sk.id)
			FROM variants v
			LEFT JOIN skus sk ON sk.variant_id = v.id AND sk.status = 'active'
			WHERE v.product_id = $1 AND v.status = 'active'
			GROUP BY v.id
		`, productID)
		if err != nil {
			return translatePGError(err, "check active variants for publish")
		}
		activeVariants := 0
		for rows.Next() {
			var variantID string
			var activeSKUCount int
			if err := rows.Scan(&variantID, &activeSKUCount); err != nil {
				rows.Close()
				return translatePGError(err, "scan active variant for publish")
			}
			activeVariants++
			if activeSKUCount != 1 {
				reasons = append(reasons, fmt.Sprintf("Variant %s must have exactly one active selectable SKU", variantID))
			}
		}
		rows.Close()
		if activeVariants == 0 {
			reasons = append(reasons, "At least one active Variant is required")
		}

		var hasPrice bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM seller_listing_prices
				WHERE seller_listing_id = $1 AND is_current = true AND currency_code = $2
			)
		`, listingID, expectedCurrency).Scan(&hasPrice); err != nil {
			return translatePGError(err, "check listing price for publish")
		}
		if !hasPrice {
			reasons = append(reasons, "Current retail price is missing or currency does not match store market")
		}

		var hasMedia bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM media_metadata WHERE product_id = $1)
		`, productID).Scan(&hasMedia); err != nil {
			return translatePGError(err, "check product media for publish")
		}
		if !hasMedia {
			reasons = append(reasons, "At least one product image is required")
		}

		var hasSellableInventory bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM variants v
				JOIN skus sk ON sk.variant_id = v.id AND sk.status = 'active'
				JOIN inventory_snapshots inv ON inv.sku_id = sk.id
				JOIN fulfillment_locations fl ON fl.id = inv.fulfillment_location_id
				WHERE v.product_id = $1
				  AND fl.store_id = $2
				  AND fl.supplier_id IS NULL
				  AND fl.status = 'active'
				  AND fl.market_code = $3
				  AND (inv.on_hand_qty - inv.reserved_qty) > 0
			)
		`, productID, storeID, marketCode).Scan(&hasSellableInventory); err != nil {
			return translatePGError(err, "check inventory topology for publish")
		}
		if !hasSellableInventory {
			reasons = append(reasons, "Sellable inventory at an active store location is required")
		}

		// Presentation must still be valid at publish time: revalidate the
		// persisted structured sections and purchase behavior inside this
		// same transaction, before any activation.
		presReasons, err := validatePresentationTx(ctx, tx, listingID, productID)
		if err != nil {
			return translatePGError(err, "revalidate presentation for publish")
		}
		reasons = append(reasons, presReasons...)

		if len(reasons) > 0 {
			return fmt.Errorf("%w: publish readiness failed: %s", ErrInvalidInput, strings.Join(reasons, "; "))
		}

		if _, err := tx.Exec(ctx, `
			UPDATE products SET status = 'active', updated_at = now() WHERE id = $1
		`, productID); err != nil {
			return translatePGError(err, "activate product")
		}

		if _, err := tx.Exec(ctx, `
			UPDATE seller_listings SET status = 'active', updated_at = now() WHERE id = $1
		`, listingID); err != nil {
			return translatePGError(err, "activate listing")
		}

		return bumpStorefrontRevisions(ctx, tx, revisionStoreItself, storeID)
	})
}

// validatePresentationTx revalidates the canonical listing's persisted
// presentation inside the caller's transaction: purchase behavior and the
// structured sections (against the product's current media set) must both be
// valid before the listing may go active.
func validatePresentationTx(ctx context.Context, tx pgx.Tx, listingID, productID string) ([]string, error) {
	var rawPb sql.NullString
	var rawSections []byte
	err := tx.QueryRow(ctx, `
		SELECT purchase_behavior, sections
		FROM seller_listing_presentations
		WHERE seller_listing_id = $1
	`, listingID).Scan(&rawPb, &rawSections)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	var reasons []string
	pb := "inherit"
	if rawPb.Valid && rawPb.String != "" {
		pb = rawPb.String
	}
	reasons = append(reasons, validatePurchaseBehavior(pb)...)

	mediaIDs := map[string]bool{}
	rows, err := tx.Query(ctx, `SELECT id FROM media_metadata WHERE product_id = $1`, productID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		mediaIDs[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(rawSections) > 0 {
		var sections []ProductPageSection
		if err := json.Unmarshal(rawSections, &sections); err != nil {
			return append(reasons, "Persisted presentation sections are not valid JSON"), nil
		}
		reasons = append(reasons, ValidateProductPageSections(sections, mediaIDs)...)
	}
	return reasons, nil
}

func (r Repository) GetOrderContactEmail(ctx context.Context, checkoutSessionID string) (string, error) {
	if checkoutSessionID == "" {
		return "", ErrInvalidInput
	}
	var email sql.NullString
	err := r.pool.QueryRow(ctx, `
		SELECT contact_email FROM checkout_sessions WHERE id = $1
	`, checkoutSessionID).Scan(&email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", fmt.Errorf("get order contact email: %w", err)
	}
	if !email.Valid {
		return "", nil
	}
	return email.String, nil
}

func (r Repository) ListOrderTimeline(ctx context.Context, orderID string) ([]OrderTimeline, error) {
	if orderID == "" {
		return nil, ErrInvalidInput
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, order_id, from_status, to_status, actor_type, reason, created_at
		FROM order_timeline
		WHERE order_id = $1
		ORDER BY created_at ASC, id ASC
	`, orderID)
	if err != nil {
		return nil, fmt.Errorf("query order timeline: %w", err)
	}
	defer rows.Close()

	var result []OrderTimeline
	for rows.Next() {
		var t OrderTimeline
		var fromStatus, reason sql.NullString
		if err := rows.Scan(&t.ID, &t.OrderID, &fromStatus, &t.ToStatus, &t.ActorType, &reason, &t.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan order timeline: %w", err)
		}
		if fromStatus.Valid {
			t.FromStatus = &fromStatus.String
		}
		if reason.Valid {
			t.Reason = &reason.String
		}
		result = append(result, t)
	}
	return result, nil
}

func (r Repository) GetMediaUploadIntentByStorageKey(ctx context.Context, storageKey string) (MediaUploadIntent, error) {
	var intent MediaUploadIntent
	err := r.pool.QueryRow(ctx, `
		SELECT id, seller_id, store_id, product_id, storage_key, content_type, max_bytes, token_digest,
		       expires_at, completed_at, created_at
		FROM media_upload_intents WHERE storage_key = $1`,
		storageKey,
	).Scan(
		&intent.ID, &intent.SellerID, &intent.StoreID, &intent.ProductID,
		&intent.StorageKey, &intent.ContentType, &intent.MaxBytes, &intent.TokenDigest,
		&intent.ExpiresAt, &intent.CompletedAt, &intent.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return MediaUploadIntent{}, ErrNotFound
		}
		return MediaUploadIntent{}, err
	}
	return intent, nil
}

func (r Repository) MarkMediaUploadIntentComplete(ctx context.Context, intentID string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE media_upload_intents SET completed_at = NOW() WHERE id = $1`,
		intentID,
	)
	return err
}

// ListSKUsByProductIDs returns the SKUs of the given products in one query.
func (r Repository) ListSKUsByProductIDs(ctx context.Context, productIDs []string) (map[string][]SKU, error) {
	out := map[string][]SKU{}
	if len(productIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT v.product_id, s.id, s.variant_id, s.code, s.barcode, s.status, s.created_at, s.updated_at
		FROM skus s
		JOIN variants v ON v.id = s.variant_id
		WHERE v.product_id = ANY($1)
		ORDER BY s.created_at ASC, s.code ASC
	`, productIDs)
	if err != nil {
		return nil, fmt.Errorf("query skus by product ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var productID string
		var s SKU
		var bc sql.NullString
		if err := rows.Scan(&productID, &s.ID, &s.VariantID, &s.Code, &bc, &s.Status, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan sku by product ids: %w", err)
		}
		if bc.Valid {
			s.Barcode = bc.String
		}
		out[productID] = append(out[productID], s)
	}
	return out, nil
}

// VariantActiveSKUCount is one active Variant of a product with the number of
// active SKUs it carries.
type VariantActiveSKUCount struct {
	ProductID    string
	VariantID    string
	ActiveSKUQty int
}

// ListActiveVariantSKUCountsByProductIDs reports, per active Variant of the
// given products, how many active SKUs it carries. Readiness requires that
// count to be exactly one.
func (r Repository) ListActiveVariantSKUCountsByProductIDs(ctx context.Context, productIDs []string) ([]VariantActiveSKUCount, error) {
	if len(productIDs) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT v.product_id, v.id, COUNT(sk.id)
		FROM variants v
		LEFT JOIN skus sk ON sk.variant_id = v.id AND sk.status = 'active'
		WHERE v.product_id = ANY($1) AND v.status = 'active'
		GROUP BY v.product_id, v.id
	`, productIDs)
	if err != nil {
		return nil, fmt.Errorf("query active variant sku counts: %w", err)
	}
	defer rows.Close()

	var result []VariantActiveSKUCount
	for rows.Next() {
		var c VariantActiveSKUCount
		if err := rows.Scan(&c.ProductID, &c.VariantID, &c.ActiveSKUQty); err != nil {
			return nil, fmt.Errorf("scan active variant sku count: %w", err)
		}
		result = append(result, c)
	}
	return result, nil
}

// ListProductNamesByProductIDs returns one display name per product, preferring
// the English translation, then Arabic, then any other locale.
func (r Repository) ListProductNamesByProductIDs(ctx context.Context, productIDs []string) (map[string]string, error) {
	out := map[string]string{}
	if len(productIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT DISTINCT ON (product_id) product_id, name
		FROM product_translations
		WHERE product_id = ANY($1)
		ORDER BY product_id,
			CASE locale WHEN 'en' THEN 0 WHEN 'ar' THEN 1 ELSE 2 END
	`, productIDs)
	if err != nil {
		return nil, fmt.Errorf("query product names: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var productID, name string
		if err := rows.Scan(&productID, &name); err != nil {
			return nil, fmt.Errorf("scan product name: %w", err)
		}
		out[productID] = name
	}
	return out, nil
}

// CountMediaByProductIDs returns the number of media records per product.
func (r Repository) CountMediaByProductIDs(ctx context.Context, productIDs []string) (map[string]int, error) {
	out := map[string]int{}
	if len(productIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `
		SELECT product_id, COUNT(*)
		FROM media_metadata
		WHERE product_id = ANY($1)
		GROUP BY product_id
	`, productIDs)
	if err != nil {
		return nil, fmt.Errorf("query media counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var productID string
		var count int
		if err := rows.Scan(&productID, &count); err != nil {
			return nil, fmt.Errorf("scan media count: %w", err)
		}
		out[productID] = count
	}
	return out, nil
}
