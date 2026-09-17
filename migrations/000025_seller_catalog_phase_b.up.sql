-- Phase B: Seller Catalog Operations Integration
-- 1. Product lifecycle: draft -> active -> archived
UPDATE products SET status = 'draft' WHERE status = 'inactive';

ALTER TABLE products
    ADD CONSTRAINT products_status_check CHECK (status IN ('draft', 'active', 'archived'));

-- 2. Listing lifecycle: draft <-> published <-> unpublished -> archived
UPDATE seller_listings SET status = 'published' WHERE status = 'active';
UPDATE seller_listings SET status = 'draft' WHERE status = 'inactive';

ALTER TABLE seller_listings
    ADD CONSTRAINT seller_listings_status_check CHECK (status IN ('draft', 'published', 'unpublished', 'archived'));

-- 3. Canonical listing & supplier offer import uniqueness per store
CREATE UNIQUE INDEX IF NOT EXISTS seller_listings_store_product_uidx
    ON seller_listings (store_id, product_id);

CREATE UNIQUE INDEX IF NOT EXISTS seller_listings_store_supplier_offer_uidx
    ON seller_listings (store_id, supplier_offer_id)
    WHERE supplier_offer_id IS NOT NULL;

-- 4. Inventory movement idempotency metadata & fingerprint
ALTER TABLE inventory_movements
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT,
    ADD COLUMN IF NOT EXISTS request_fingerprint TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_inventory_movements_snapshot_idempotency
    ON inventory_movements (inventory_snapshot_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND idempotency_key != '';
