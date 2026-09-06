CREATE TABLE seller_products (
    id UUID PRIMARY KEY,
    seller_id UUID NOT NULL REFERENCES sellers(id) ON DELETE RESTRICT,
    product_id UUID NOT NULL UNIQUE REFERENCES products(id) ON DELETE RESTRICT,
    seller_code TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX seller_products_seller_id_idx ON seller_products(seller_id);
CREATE UNIQUE INDEX seller_products_seller_code_uidx ON seller_products(seller_id, seller_code) WHERE seller_code IS NOT NULL;

ALTER TABLE media_metadata
    ADD COLUMN storage_key TEXT,
    ADD COLUMN is_primary BOOLEAN NOT NULL DEFAULT false;

CREATE UNIQUE INDEX media_metadata_product_primary_uidx ON media_metadata(product_id) WHERE is_primary = true;

CREATE TABLE seller_listing_presentations (
    seller_listing_id UUID PRIMARY KEY
        REFERENCES seller_listings(id)
        ON DELETE CASCADE,

    schema_version INTEGER NOT NULL DEFAULT 1,

    purchase_behavior TEXT NOT NULL DEFAULT 'inherit',

    sections JSONB NOT NULL DEFAULT '[]'::jsonb,

    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),

    CHECK (
        purchase_behavior IN (
            'inherit',
            'add_to_cart',
            'buy_now'
        )
    )
);
