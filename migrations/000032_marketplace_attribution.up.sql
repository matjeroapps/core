CREATE TABLE IF NOT EXISTS marketplace_order_attributions (
    id UUID PRIMARY KEY,
    order_id UUID NOT NULL UNIQUE REFERENCES orders(id) ON DELETE RESTRICT,
    checkout_session_id UUID NOT NULL UNIQUE REFERENCES checkout_sessions(id) ON DELETE RESTRICT,
    store_id UUID NOT NULL REFERENCES stores(id) ON DELETE RESTRICT,
    market_code CHAR(2) NOT NULL REFERENCES markets(code),
    seller_listing_id UUID NOT NULL REFERENCES seller_listings(id) ON DELETE RESTRICT,
    source_collection TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT marketplace_order_attributions_market_check CHECK (length(market_code) = 2)
);

CREATE INDEX IF NOT EXISTS idx_marketplace_attributions_listing
    ON marketplace_order_attributions (seller_listing_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_marketplace_attributions_store
    ON marketplace_order_attributions (store_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_marketplace_attributions_market_collection
    ON marketplace_order_attributions (market_code, source_collection)
    WHERE source_collection IS NOT NULL;
