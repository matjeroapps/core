DROP INDEX IF EXISTS idx_inventory_movements_snapshot_idempotency;

ALTER TABLE inventory_movements
    DROP COLUMN IF EXISTS request_fingerprint,
    DROP COLUMN IF EXISTS idempotency_key;

DROP INDEX IF EXISTS seller_listings_store_supplier_offer_uidx;
DROP INDEX IF EXISTS seller_listings_store_product_uidx;

ALTER TABLE seller_listings DROP CONSTRAINT IF EXISTS seller_listings_status_check;
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_status_check;
