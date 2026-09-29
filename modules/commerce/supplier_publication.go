package commerce

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s Service) CreateSupplierVariantForSubject(ctx context.Context, subject, supplierID, productID, code, status string) (Variant, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return Variant{}, err
	}
	if _, err := s.repo.GetSupplierProductBySupplierAndProduct(ctx, supplierID, productID); err != nil {
		return Variant{}, err
	}
	return s.repo.CreateVariant(ctx, productID, code, status)
}

func (s Service) CreateSupplierSKUForSubject(ctx context.Context, subject, supplierID, productID, variantID, code, barcode, status string) (SKU, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return SKU{}, err
	}
	if _, err := s.repo.GetSupplierProductBySupplierAndProduct(ctx, supplierID, productID); err != nil {
		return SKU{}, err
	}
	variant, err := s.repo.GetVariantByID(ctx, variantID)
	if err != nil {
		return SKU{}, err
	}
	if variant.ProductID != productID {
		return SKU{}, ErrNotFound
	}
	return s.repo.CreateSKU(ctx, variantID, code, barcode, status)
}

func (s Service) CreateSupplierMediaForSubject(ctx context.Context, subject, supplierID, productID string, media MediaMetadata) (MediaMetadata, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return MediaMetadata{}, err
	}
	if _, err := s.repo.GetSupplierProductBySupplierAndProduct(ctx, supplierID, productID); err != nil {
		return MediaMetadata{}, err
	}
	media.ProductID = productID
	return s.repo.CreateSupplierMediaMetadata(ctx, media)
}

func (s Service) UpdateSupplierMediaForSubject(ctx context.Context, subject, supplierID, productID, mediaID string, altText string, sortOrder int, isPrimary bool) (MediaMetadata, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return MediaMetadata{}, err
	}
	if _, err := s.repo.GetSupplierProductBySupplierAndProduct(ctx, supplierID, productID); err != nil {
		return MediaMetadata{}, err
	}
	return s.repo.UpdateSupplierMediaMetadata(ctx, productID, mediaID, altText, sortOrder, isPrimary)
}

func (s Service) DeleteSupplierMediaForSubject(ctx context.Context, subject, supplierID, productID, mediaID string) error {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return err
	}
	if _, err := s.repo.GetSupplierProductBySupplierAndProduct(ctx, supplierID, productID); err != nil {
		return err
	}
	return s.repo.DeleteSupplierMediaMetadata(ctx, productID, mediaID)
}

func (s Service) GetSupplierPublicationReadinessForSubject(ctx context.Context, subject, supplierID, productID string) (SupplierPublication, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return SupplierPublication{}, err
	}
	sp, err := s.repo.GetSupplierProductBySupplierAndProduct(ctx, supplierID, productID)
	if err != nil {
		return SupplierPublication{}, err
	}
	readiness, offers, err := s.repo.EvaluateSupplierPublicationReadiness(ctx, supplierID, productID)
	if err != nil {
		return SupplierPublication{}, err
	}
	return SupplierPublication{ProductID: productID, SupplierProduct: sp, Readiness: readiness, Status: sp.Status, PublishedOffers: offers}, nil
}

func (s Service) PublishSupplierProductForSubject(ctx context.Context, subject, supplierID, productID string) (SupplierPublication, error) {
	if _, err := s.RequireSupplierAccess(ctx, subject, supplierID); err != nil {
		return SupplierPublication{}, err
	}
	if _, err := s.repo.GetSupplierProductBySupplierAndProduct(ctx, supplierID, productID); err != nil {
		return SupplierPublication{}, err
	}
	return s.repo.PublishSupplierProductAtomically(ctx, supplierID, productID)
}

func (r Repository) CreateSupplierMediaMetadata(ctx context.Context, m MediaMetadata) (MediaMetadata, error) {
	if m.ProductID == "" || m.MediaType == "" || m.URI == "" {
		return MediaMetadata{}, ErrInvalidInput
	}
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		id := m.ID
		if id == "" {
			id = uuid.NewString()
		}
		if m.IsPrimary {
			if _, err := tx.Exec(ctx, `UPDATE media_metadata SET is_primary = false WHERE product_id = $1`, m.ProductID); err != nil {
				return translatePGError(err, "clear primary media")
			}
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO media_metadata (id, product_id, media_type, uri, alt_text, sort_order, metadata, storage_key, is_primary)
			VALUES ($1, $2, $3, $4, $5, $6, '{}'::jsonb, $7, $8)
			ON CONFLICT (storage_key) WHERE storage_key IS NOT NULL
			DO UPDATE SET alt_text = EXCLUDED.alt_text, sort_order = EXCLUDED.sort_order, is_primary = EXCLUDED.is_primary, updated_at = now()
			RETURNING id, created_at, updated_at
		`, id, m.ProductID, m.MediaType, m.URI, m.AltText, m.SortOrder, m.StorageKey, m.IsPrimary).Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return translatePGError(err, "create supplier media")
		}
		return nil
	})
	if err != nil {
		return MediaMetadata{}, err
	}
	return m, nil
}

func (r Repository) UpdateSupplierMediaMetadata(ctx context.Context, productID, mediaID string, altText string, sortOrder int, isPrimary bool) (MediaMetadata, error) {
	if productID == "" || mediaID == "" {
		return MediaMetadata{}, ErrInvalidInput
	}
	var m MediaMetadata
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if isPrimary {
			if _, err := tx.Exec(ctx, `UPDATE media_metadata SET is_primary = false WHERE product_id = $1`, productID); err != nil {
				return translatePGError(err, "clear primary media")
			}
		}
		var storageKey *string
		err := tx.QueryRow(ctx, `
			UPDATE media_metadata
			SET alt_text = $3, sort_order = $4, is_primary = $5, updated_at = now()
			WHERE product_id = $1 AND id = $2
			RETURNING id, product_id, media_type, uri, alt_text, sort_order, metadata, storage_key, is_primary, created_at, updated_at
		`, productID, mediaID, altText, sortOrder, isPrimary).Scan(&m.ID, &m.ProductID, &m.MediaType, &m.URI, &m.AltText, &m.SortOrder, &m.Metadata, &storageKey, &m.IsPrimary, &m.CreatedAt, &m.UpdatedAt)
		if err != nil {
			return translatePGError(err, "update supplier media")
		}
		m.StorageKey = storageKey
		return nil
	})
	if err != nil {
		return MediaMetadata{}, err
	}
	return m, nil
}

func (r Repository) DeleteSupplierMediaMetadata(ctx context.Context, productID, mediaID string) error {
	if productID == "" || mediaID == "" {
		return ErrInvalidInput
	}
	return r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `DELETE FROM media_metadata WHERE product_id = $1 AND id = $2`, productID, mediaID)
		if err != nil {
			return translatePGError(err, "delete supplier media")
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r Repository) EvaluateSupplierPublicationReadiness(ctx context.Context, supplierID, productID string) (PublishReadiness, []SupplierOffer, error) {
	reasons, offers, err := r.evaluateSupplierPublicationReadiness(ctx, supplierID, productID, nil)
	if err != nil {
		return PublishReadiness{}, nil, err
	}
	return PublishReadiness{IsReady: len(reasons) == 0, Reasons: reasons}, offers, nil
}

func (r Repository) PublishSupplierProductAtomically(ctx context.Context, supplierID, productID string) (SupplierPublication, error) {
	var out SupplierPublication
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT id FROM products WHERE id = $1 FOR UPDATE`, productID); err != nil {
			return translatePGError(err, "lock supplier product")
		}
		reasons, offers, err := r.evaluateSupplierPublicationReadiness(ctx, supplierID, productID, tx)
		if err != nil {
			return err
		}
		if len(reasons) > 0 {
			return fmt.Errorf("%w: supplier publication readiness failed: %s", ErrPublishNotReady, strings.Join(reasons, "; "))
		}
		if _, err := tx.Exec(ctx, `UPDATE products SET status = 'published', updated_at = now() WHERE id = $1`, productID); err != nil {
			return translatePGError(err, "publish product")
		}
		if _, err := tx.Exec(ctx, `UPDATE supplier_products SET status = 'active', updated_at = now() WHERE supplier_id = $1 AND product_id = $2`, supplierID, productID); err != nil {
			return translatePGError(err, "publish supplier product")
		}
		var sp SupplierProduct
		if err := tx.QueryRow(ctx, `
			SELECT id, supplier_id, product_id, supplier_code, status, created_at, updated_at
			FROM supplier_products
			WHERE supplier_id = $1 AND product_id = $2
		`, supplierID, productID).Scan(&sp.ID, &sp.SupplierID, &sp.ProductID, &sp.SupplierCode, &sp.Status, &sp.CreatedAt, &sp.UpdatedAt); err != nil {
			return translatePGError(err, "get published supplier product")
		}
		out = SupplierPublication{
			ProductID:       productID,
			SupplierProduct: sp,
			Readiness:       PublishReadiness{IsReady: true},
			Status:          sp.Status,
			PublishedOffers: offers,
		}
		return nil
	})
	return out, err
}

func (r Repository) evaluateSupplierPublicationReadiness(ctx context.Context, supplierID, productID string, tx pgx.Tx) ([]string, []SupplierOffer, error) {
	q := r.pool.QueryRow
	query := func(sql string, args ...any) pgx.Row {
		if tx != nil {
			return tx.QueryRow(ctx, sql, args...)
		}
		return q(ctx, sql, args...)
	}
	var exists bool
	if err := query(`SELECT EXISTS (SELECT 1 FROM supplier_products WHERE supplier_id = $1 AND product_id = $2)`, supplierID, productID).Scan(&exists); err != nil {
		return nil, nil, translatePGError(err, "check supplier product")
	}
	if !exists {
		return nil, nil, ErrNotFound
	}
	reasons := []string{}
	checks := []struct {
		reason string
		sql    string
	}{
		{"English product name is required", `SELECT EXISTS (SELECT 1 FROM product_translations WHERE product_id = $1 AND locale = 'en' AND name <> '')`},
		{"Arabic product name is required", `SELECT EXISTS (SELECT 1 FROM product_translations WHERE product_id = $1 AND locale = 'ar' AND name <> '')`},
		{"At least one category is required", `SELECT EXISTS (SELECT 1 FROM product_categories WHERE product_id = $1)`},
		{"At least one active SKU is required", `SELECT EXISTS (SELECT 1 FROM variants v JOIN skus s ON s.variant_id = v.id WHERE v.product_id = $1 AND v.status = 'active' AND s.status = 'active')`},
		{"A completed primary media asset is required", `SELECT EXISTS (SELECT 1 FROM media_metadata WHERE product_id = $1 AND is_primary = true)`},
	}
	for _, check := range checks {
		var ok bool
		if err := query(check.sql, productID).Scan(&ok); err != nil {
			return nil, nil, translatePGError(err, "check supplier readiness")
		}
		if !ok {
			reasons = append(reasons, check.reason)
		}
	}
	var offers []SupplierOffer
	queryRows := func(sql string, args ...any) (pgx.Rows, error) {
		if tx != nil {
			return tx.Query(ctx, sql, args...)
		}
		return r.pool.Query(ctx, sql, args...)
	}
	rows, err := queryRows(`
		SELECT so.id, so.supplier_id, so.supplier_product_id, so.supplier_market_id, so.market_code, so.status, so.minimum_order_quantity, so.created_at, so.updated_at
		FROM supplier_offers so
		JOIN supplier_products sp ON sp.id = so.supplier_product_id
		JOIN supplier_markets sm ON sm.id = so.supplier_market_id
		JOIN supplier_offer_prices sop ON sop.supplier_offer_id = so.id AND sop.is_current = true
		JOIN supplier_offer_availability av ON av.supplier_offer_id = so.id
		WHERE so.supplier_id = $1 AND sp.product_id = $2 AND so.status = 'active'
		  AND so.minimum_order_quantity > 0
		  AND sop.amount_minor > 0
		  AND sm.status = 'active'
		  AND COALESCE(av.is_available, false) = true
		  AND COALESCE(av.available_qty, 0) > 0
	`, supplierID, productID)
	if err != nil {
		return nil, nil, translatePGError(err, "check supplier offers")
	}
	defer rows.Close()
	for rows.Next() {
		var offer SupplierOffer
		if err := rows.Scan(&offer.ID, &offer.SupplierID, &offer.SupplierProductID, &offer.SupplierMarketID, &offer.MarketCode, &offer.Status, &offer.MinimumOrderQty, &offer.CreatedAt, &offer.UpdatedAt); err != nil {
			return nil, nil, translatePGError(err, "scan ready supplier offer")
		}
		offers = append(offers, offer)
	}
	if rows.Err() != nil {
		return nil, nil, rows.Err()
	}
	if len(offers) == 0 {
		reasons = append(reasons, "At least one active offer with price, MOQ, market and available quantity is required")
	}
	var inventoryOK bool
	if err := query(`
		SELECT EXISTS (
			SELECT 1
			FROM inventory_snapshots inv
			JOIN fulfillment_locations fl ON fl.id = inv.fulfillment_location_id
			JOIN variants v ON v.product_id = $2
			JOIN skus s ON s.variant_id = v.id AND s.id = inv.sku_id
			WHERE fl.supplier_id = $1 AND fl.status = 'active' AND (inv.on_hand_qty - inv.reserved_qty) > 0
		)
	`, supplierID, productID).Scan(&inventoryOK); err != nil {
		return nil, nil, translatePGError(err, "check supplier inventory")
	}
	if !inventoryOK {
		reasons = append(reasons, "At least one authorized location must have positive available inventory")
	}
	return reasons, offers, nil
}
