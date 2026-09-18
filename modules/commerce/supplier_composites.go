package commerce

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"core/packages/money"
)

// Atomic supplier onboarding operations.
//
// Creating a supplier product or a supplier offer is a multi-row operation. When
// the rows are written by separate calls, a failure partway through leaves the
// earlier rows committed: a product with no supplier binding, or an offer with no
// price. Both are invalid states that no caller can produce deliberately and that
// nothing later repairs.
//
// The functions below write every row of one logical creation inside a single
// transaction, so the operation either fully applies or leaves no trace.

// ProductDraft is the full definition of a supplier's new product.
type ProductDraft struct {
	Slug         string
	Status       string
	SupplierCode string
	Translations []ProductTranslation
	CategoryIDs  []string
}

// CreateSupplierProductAtomically creates the global product, its translations,
// the supplier binding and the category assignments in one transaction.
func (r Repository) CreateSupplierProductAtomically(ctx context.Context, supplierID string, draft ProductDraft) (Product, SupplierProduct, error) {
	if supplierID == "" || draft.Slug == "" || draft.Status == "" || draft.SupplierCode == "" {
		return Product{}, SupplierProduct{}, ErrInvalidInput
	}
	for _, translation := range draft.Translations {
		if translation.Locale == "" || translation.Name == "" {
			return Product{}, SupplierProduct{}, ErrInvalidInput
		}
	}

	var (
		product         Product
		supplierProduct SupplierProduct
	)
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		productID := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO products (id, slug, status)
			VALUES ($1, $2, $3)
			RETURNING created_at, updated_at
		`, productID, draft.Slug, draft.Status).Scan(&product.CreatedAt, &product.UpdatedAt); err != nil {
			return translatePGError(err, "create product")
		}
		product.ID = productID
		product.Slug = draft.Slug
		product.Status = draft.Status

		for _, translation := range draft.Translations {
			if err := upsertProductTranslationTx(ctx, tx, productID, translation); err != nil {
				return err
			}
		}

		supplierProductID := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO supplier_products (id, supplier_id, product_id, supplier_code, status)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING created_at, updated_at
		`, supplierProductID, supplierID, productID, draft.SupplierCode, draft.Status).
			Scan(&supplierProduct.CreatedAt, &supplierProduct.UpdatedAt); err != nil {
			return translatePGError(err, "create supplier product")
		}
		supplierProduct.ID = supplierProductID
		supplierProduct.SupplierID = supplierID
		supplierProduct.ProductID = productID
		supplierProduct.SupplierCode = draft.SupplierCode
		supplierProduct.Status = draft.Status

		// No revision bump: a brand-new product is not listed by any store yet, so
		// no storefront output changes until a seller imports it.
		return setProductCategoriesTx(ctx, tx, productID, draft.CategoryIDs)
	})
	if err != nil {
		return Product{}, SupplierProduct{}, err
	}
	return product, supplierProduct, nil
}

// OfferDraft is the full definition of a supplier's new offer. Price and
// availability are optional; when present they are written in the same
// transaction as the offer row.
type OfferDraft struct {
	SupplierProductID string
	SupplierMarketID  string
	MarketCode        string
	Status            string
	Price             *money.Money
	IsAvailable       *bool
	AvailableQty      *int64
}

// CreateSupplierOfferAtomically creates the offer and, when supplied, its price
// and availability in one transaction. An offer is never left unpriced because a
// later step failed.
func (r Repository) CreateSupplierOfferAtomically(ctx context.Context, supplierID string, draft OfferDraft) (SupplierOffer, error) {
	if supplierID == "" || draft.SupplierProductID == "" || draft.SupplierMarketID == "" || draft.MarketCode == "" || draft.Status == "" {
		return SupplierOffer{}, ErrInvalidInput
	}
	// Validate before opening the transaction so an invalid price never causes a
	// rollback that looks like a storage failure.
	if draft.Price != nil {
		if err := draft.Price.Validate(); err != nil {
			return SupplierOffer{}, fmt.Errorf("%w: %s", ErrInvalidInput, err)
		}
	}
	if draft.AvailableQty != nil && *draft.AvailableQty < 0 {
		return SupplierOffer{}, ErrInvalidInput
	}

	var created SupplierOffer
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		offerID := uuid.NewString()
		if err := tx.QueryRow(ctx, `
			INSERT INTO supplier_offers (id, supplier_id, supplier_product_id, supplier_market_id, market_code, status)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING created_at, updated_at
		`, offerID, supplierID, draft.SupplierProductID, draft.SupplierMarketID, draft.MarketCode, draft.Status).
			Scan(&created.CreatedAt, &created.UpdatedAt); err != nil {
			return translatePGError(err, "create supplier offer")
		}
		created.ID = offerID
		created.SupplierID = supplierID
		created.SupplierProductID = draft.SupplierProductID
		created.SupplierMarketID = draft.SupplierMarketID
		created.MarketCode = draft.MarketCode
		created.Status = draft.Status

		if draft.Price != nil {
			if _, err := tx.Exec(ctx, `
				INSERT INTO supplier_offer_prices (id, supplier_offer_id, amount_minor, currency_code, is_current)
				VALUES ($1, $2, $3, $4, true)
				ON CONFLICT (supplier_offer_id)
				DO UPDATE SET amount_minor = EXCLUDED.amount_minor, currency_code = EXCLUDED.currency_code, is_current = true, updated_at = now()
			`, uuid.NewString(), offerID, draft.Price.AmountMinor, draft.Price.Currency); err != nil {
				return translatePGError(err, "set supplier offer price")
			}
		}

		if draft.IsAvailable != nil || draft.AvailableQty != nil {
			isAvailable := draft.IsAvailable != nil && *draft.IsAvailable
			if _, err := tx.Exec(ctx, `
				INSERT INTO supplier_offer_availability (id, supplier_offer_id, is_available, available_qty)
				VALUES ($1, $2, $3, $4)
				ON CONFLICT (supplier_offer_id)
				DO UPDATE SET is_available = EXCLUDED.is_available, available_qty = EXCLUDED.available_qty, updated_at = now()
			`, uuid.NewString(), offerID, isAvailable, draft.AvailableQty); err != nil {
				return translatePGError(err, "set supplier offer availability")
			}
		}

		// No revision bump: a brand-new offer has no seller listings yet, so no
		// storefront output changes until a seller imports it.
		return nil
	})
	if err != nil {
		return SupplierOffer{}, err
	}
	return created, nil
}

// upsertProductTranslationTx writes one localized product name/description on an
// open transaction.
func upsertProductTranslationTx(ctx context.Context, tx pgx.Tx, productID string, translation ProductTranslation) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO product_translations (product_id, locale, name, description)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (product_id, locale)
		DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description
	`, productID, translation.Locale, translation.Name, translation.Description)
	return translatePGError(err, "upsert product translation")
}

// setProductCategoriesTx replaces a product's category assignments on an open
// transaction.
func setProductCategoriesTx(ctx context.Context, tx pgx.Tx, productID string, categoryIDs []string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM product_categories WHERE product_id = $1`, productID); err != nil {
		return fmt.Errorf("clear product categories: %w", err)
	}
	for _, categoryID := range uniqueStrings(categoryIDs) {
		if categoryID == "" {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO product_categories (product_id, category_id)
			VALUES ($1, $2)
			ON CONFLICT (product_id, category_id) DO NOTHING
		`, productID, categoryID); err != nil {
			return translatePGError(err, "set product categories")
		}
	}
	return nil
}
