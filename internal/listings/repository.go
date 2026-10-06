package listings

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) GetListingWithWholesalePrice(ctx context.Context, storeID, listingID string) (*ListingWholesaleInfo, error) {
	query := `
		SELECT
			sl.id,
			sl.store_id,
			sl.product_id,
			sl.supplier_offer_id,
			sl.market_code,
			COALESCE(sop.amount_minor, 0) AS wholesale_price_minor_units,
			COALESCE(sop.currency_code, '') AS wholesale_currency
		FROM seller_listings sl
		LEFT JOIN supplier_offer_prices sop
			ON sop.supplier_offer_id = sl.supplier_offer_id AND sop.is_current = true
		WHERE sl.store_id = $1 AND sl.id = $2
	`

	var info ListingWholesaleInfo
	var suppOfferID sql.NullString
	err := r.pool.QueryRow(ctx, query, storeID, listingID).Scan(
		&info.ListingID,
		&info.StoreID,
		&info.ProductID,
		&suppOfferID,
		&info.MarketCode,
		&info.WholesalePriceMinorUnits,
		&info.WholesaleCurrency,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	if suppOfferID.Valid {
		info.SupplierOfferID = &suppOfferID.String
	}

	return &info, nil
}

func (r *PostgresRepository) SaveListingPrice(ctx context.Context, listingID string, amountMinor int64, currency string) (time.Time, error) {
	newID := uuid.NewString()
	query := `
		INSERT INTO seller_listing_prices (id, seller_listing_id, amount_minor, currency_code, is_current, updated_at)
		VALUES ($1, $2, $3, $4, true, now())
		ON CONFLICT (seller_listing_id)
		DO UPDATE SET
			amount_minor = EXCLUDED.amount_minor,
			currency_code = EXCLUDED.currency_code,
			is_current = true,
			updated_at = now()
		RETURNING updated_at
	`

	var updatedAt time.Time
	err := r.pool.QueryRow(ctx, query, newID, listingID, amountMinor, currency).Scan(&updatedAt)
	if err != nil {
		return time.Time{}, err
	}
	return updatedAt, nil
}
