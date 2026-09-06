-- Upload intents track presigned PUT requests so Complete can
-- verify ownership, expiry, and token authenticity.
CREATE TABLE IF NOT EXISTS media_upload_intents (
    id          TEXT        PRIMARY KEY DEFAULT gen_random_uuid()::text,
    seller_id   TEXT        NOT NULL,
    store_id    TEXT        NOT NULL,
    product_id  TEXT        NOT NULL,
    storage_key TEXT        NOT NULL UNIQUE,
    content_type TEXT       NOT NULL,
    max_bytes   BIGINT      NOT NULL,
    token_digest TEXT       NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mui_seller_product ON media_upload_intents(seller_id, product_id);
CREATE INDEX IF NOT EXISTS idx_mui_expires ON media_upload_intents(expires_at) WHERE completed_at IS NULL;
