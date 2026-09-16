ALTER TABLE stores
    ADD CONSTRAINT stores_status_check CHECK (status IN ('draft', 'active', 'inactive'));

CREATE INDEX IF NOT EXISTS idx_stores_seller_status ON stores(seller_id, status);
