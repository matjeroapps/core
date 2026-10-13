package commerce

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const maxProductRemovalBatch = 100

type StoreProductRemovalResult struct {
	ProductID          string `json:"product_id"`
	Outcome            string `json:"outcome"`
	Reason             string `json:"reason,omitempty"`
	ProductDataDeleted bool   `json:"product_data_deleted"`
}

type StoreProductRemovalResponse struct {
	Results []StoreProductRemovalResult `json:"results"`
}

type StoreProductsRemovalRequest struct {
	ProductIDs []string `json:"product_ids"`
}

// RemoveSellerProductsForSubject removes products from one store's catalog.
// Shared products are only physically deleted when no other store or protected
// operational record still depends on them.
func (s Service) RemoveSellerProductsForSubject(ctx context.Context, subject, storeID string, productIDs []string) (StoreProductRemovalResponse, error) {
	if subject == "" || storeID == "" || len(productIDs) == 0 || len(productIDs) > maxProductRemovalBatch {
		return StoreProductRemovalResponse{}, ErrInvalidInput
	}
	seen := make(map[string]struct{}, len(productIDs))
	for _, id := range productIDs {
		if _, err := uuid.Parse(id); err != nil {
			return StoreProductRemovalResponse{}, ErrInvalidInput
		}
		if _, exists := seen[id]; exists {
			return StoreProductRemovalResponse{}, ErrInvalidInput
		}
		seen[id] = struct{}{}
	}

	store, err := s.repo.GetStore(ctx, storeID)
	if err != nil {
		return StoreProductRemovalResponse{}, err
	}
	seller, err := s.RequireSellerManagerAccess(ctx, subject, store.SellerID)
	if err != nil {
		return StoreProductRemovalResponse{}, err
	}

	response := StoreProductRemovalResponse{Results: make([]StoreProductRemovalResult, 0, len(productIDs))}
	for _, productID := range productIDs {
		result, err := s.repo.RemoveSellerProductFromStore(ctx, seller.ID, storeID, productID)
		if err == nil {
			response.Results = append(response.Results, result)
			continue
		}
		outcome, reason := removalFailure(err)
		response.Results = append(response.Results, StoreProductRemovalResult{
			ProductID: productID,
			Outcome:   outcome,
			Reason:    reason,
		})
	}
	return response, nil
}

func removalFailure(err error) (string, string) {
	switch {
	case errors.Is(err, ErrNotFound):
		return "not_found", "not_found"
	case errors.Is(err, ErrResourceInUse):
		return "failed", "resource_in_use"
	case errors.Is(err, ErrInvalidInput):
		return "failed", "invalid_input"
	default:
		return "failed", "internal_error"
	}
}

func (r Repository) RemoveSellerProductFromStore(ctx context.Context, sellerID, storeID, productID string) (StoreProductRemovalResult, error) {
	if sellerID == "" || storeID == "" || productID == "" {
		return StoreProductRemovalResult{}, ErrInvalidInput
	}
	result := StoreProductRemovalResult{ProductID: productID}
	err := r.withTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var listingID, listingStatus string
		var supplierOfferID *string
		var ownerID *string
		if err := tx.QueryRow(ctx, `
			SELECT l.id, l.status, l.supplier_offer_id, sp.seller_id
			FROM seller_listings l
			JOIN stores st ON st.id = l.store_id
			LEFT JOIN seller_products sp ON sp.product_id = l.product_id AND sp.seller_id = st.seller_id
			WHERE l.store_id = $1 AND l.product_id = $2
			FOR UPDATE OF l
		`, storeID, productID).Scan(&listingID, &listingStatus, &supplierOfferID, &ownerID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			return translatePGError(err, "lock seller listing for removal")
		}
		if supplierOfferID == nil && (ownerID == nil || *ownerID != sellerID) {
			return ErrNotFound
		}

		var hasStoreOrder bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1
				FROM order_items oi
				JOIN orders o ON o.id = oi.order_id
				WHERE o.store_id = $1 AND (
					oi.product_id = $2
					OR oi.variant_id IN (SELECT id FROM variants WHERE product_id = $2)
					OR oi.sku_id IN (SELECT sk.id FROM skus sk JOIN variants v ON v.id = sk.variant_id WHERE v.product_id = $2)
				)
			) OR EXISTS (
				SELECT 1 FROM marketplace_order_attributions moa
				WHERE moa.store_id = $1 AND moa.seller_listing_id = $3
			)
		`, storeID, productID, listingID).Scan(&hasStoreOrder); err != nil {
			return translatePGError(err, "check store orders before product removal")
		}

		if hasStoreOrder {
			if listingStatus != "archived" {
				if _, err := tx.Exec(ctx, `UPDATE seller_listings SET status = 'archived', updated_at = now() WHERE id = $1`, listingID); err != nil {
					return translatePGError(err, "archive store listing with order history")
				}
				if err := bumpStorefrontRevisions(ctx, tx, revisionStoreItself, storeID); err != nil {
					return err
				}
			}
			result.Outcome = "archived"
			return nil
		}

		var hasCartItems bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cart_items WHERE seller_listing_id = $1)`, listingID).Scan(&hasCartItems); err != nil {
			return translatePGError(err, "check carts before product removal")
		}
		if hasCartItems {
			return fmt.Errorf("%w: product listing is referenced by a cart", ErrResourceInUse)
		}
		var hasMarketplaceAttribution bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM marketplace_order_attributions WHERE seller_listing_id = $1)`, listingID).Scan(&hasMarketplaceAttribution); err != nil {
			return translatePGError(err, "check marketplace history before product removal")
		}
		if hasMarketplaceAttribution {
			return fmt.Errorf("%w: product listing is referenced by marketplace history", ErrResourceInUse)
		}

		// This relationship belongs to the store, so removing it must not delete
		// the media asset or references belonging to another store.
		if _, err := tx.Exec(ctx, `DELETE FROM product_media_references WHERE store_id = $1 AND product_id = $2`, storeID, productID); err != nil {
			return translatePGError(err, "detach current-store product media references")
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM store_product_categories spc
			USING store_categories sc
			WHERE spc.product_id = $1 AND spc.store_category_id = sc.id AND sc.store_id = $2
		`, productID, storeID); err != nil {
			return translatePGError(err, "detach current-store product categories")
		}
		if _, err := tx.Exec(ctx, `DELETE FROM seller_listings WHERE id = $1`, listingID); err != nil {
			return translatePGError(err, "remove product listing from store")
		}
		if err := bumpStorefrontRevisions(ctx, tx, revisionStoreItself, storeID); err != nil {
			return err
		}

		// Supplier catalog rows are never deleted through a seller action.
		if supplierOfferID != nil {
			result.Outcome = "removed"
			return nil
		}

		var protected bool
		if err := tx.QueryRow(ctx, `
			SELECT
				EXISTS (SELECT 1 FROM seller_listings WHERE product_id = $1)
				OR EXISTS (SELECT 1 FROM store_product_categories WHERE product_id = $1)
				OR EXISTS (
					SELECT 1 FROM order_items oi JOIN orders o ON o.id = oi.order_id
					WHERE oi.product_id = $1
						OR oi.variant_id IN (SELECT id FROM variants WHERE product_id = $1)
						OR oi.sku_id IN (SELECT sk.id FROM skus sk JOIN variants v ON v.id = sk.variant_id WHERE v.product_id = $1)
				)
				OR EXISTS (SELECT 1 FROM supplier_products WHERE product_id = $1)
				OR EXISTS (SELECT 1 FROM product_media_references WHERE product_id = $1)
				OR EXISTS (SELECT 1 FROM ai_authoring_image_candidates WHERE product_id = $1)
				OR EXISTS (SELECT 1 FROM inventory_snapshots sn JOIN skus sk ON sk.id = sn.sku_id JOIN variants v ON v.id = sk.variant_id WHERE v.product_id = $1)
				OR EXISTS (SELECT 1 FROM cart_items ci JOIN skus sk ON sk.id = ci.sku_id JOIN variants v ON v.id = sk.variant_id WHERE v.product_id = $1)
		`, productID).Scan(&protected); err != nil {
			return translatePGError(err, "check shared product references")
		}
		if protected {
			result.Outcome = "removed"
			return nil
		}

		if _, err := tx.Exec(ctx, `DELETE FROM seller_products WHERE seller_id = $1 AND product_id = $2`, sellerID, productID); err != nil {
			return translatePGError(err, "remove seller product ownership")
		}
		res, err := tx.Exec(ctx, `DELETE FROM products WHERE id = $1`, productID)
		if err != nil {
			return translatePGError(err, "delete unreferenced seller product")
		}
		result.Outcome = "removed"
		result.ProductDataDeleted = res.RowsAffected() > 0
		return nil
	})
	return result, err
}
