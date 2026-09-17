-- Phase C: Store-Scoped Media Library and Reusable Product References

-- 1. Immutable store-scoped media assets
CREATE TABLE IF NOT EXISTS store_media_assets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id UUID NOT NULL REFERENCES stores(id) ON DELETE RESTRICT,
    checksum_sha256 TEXT NOT NULL,
    storage_key TEXT NOT NULL UNIQUE,
    content_type TEXT NOT NULL,
    byte_size BIGINT NOT NULL,
    original_filename TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL CHECK (status IN ('ready', 'deleting', 'deleted')),
    created_by_subject TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (id, store_id)
);

-- Partial unique index for per-store SHA-256 deduplication
CREATE UNIQUE INDEX IF NOT EXISTS idx_store_media_assets_store_checksum
    ON store_media_assets (store_id, checksum_sha256)
    WHERE status IN ('ready', 'deleting');

CREATE INDEX IF NOT EXISTS idx_store_media_assets_store_status
    ON store_media_assets (store_id, status);

-- 2. Reusable product media references
CREATE TABLE IF NOT EXISTS product_media_references (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_id UUID NOT NULL REFERENCES stores(id) ON DELETE RESTRICT,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    asset_id UUID NOT NULL REFERENCES store_media_assets(id) ON DELETE RESTRICT,
    alt_text TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    is_primary BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (asset_id, store_id) REFERENCES store_media_assets(id, store_id) ON DELETE RESTRICT
);

-- One reference for (store_id, product_id, asset_id)
CREATE UNIQUE INDEX IF NOT EXISTS idx_product_media_references_store_product_asset
    ON product_media_references (store_id, product_id, asset_id);

-- At most one primary reference per (store_id, product_id)
CREATE UNIQUE INDEX IF NOT EXISTS idx_product_media_references_store_product_primary
    ON product_media_references (store_id, product_id)
    WHERE is_primary = true;

CREATE INDEX IF NOT EXISTS idx_product_media_references_product
    ON product_media_references (product_id);

-- 3. Evolve media_upload_intents table
ALTER TABLE media_upload_intents
    ADD COLUMN IF NOT EXISTS client_upload_id TEXT,
    ADD COLUMN IF NOT EXISTS request_fingerprint TEXT,
    ADD COLUMN IF NOT EXISTS checksum_sha256 TEXT,
    ADD COLUMN IF NOT EXISTS byte_size BIGINT,
    ADD COLUMN IF NOT EXISTS original_filename TEXT;

ALTER TABLE media_upload_intents
    ALTER COLUMN product_id DROP NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_media_upload_intents_store_client_upload
    ON media_upload_intents (store_id, client_upload_id)
    WHERE client_upload_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_media_upload_intents_store_checksum
    ON media_upload_intents (store_id, checksum_sha256)
    WHERE completed_at IS NULL;

-- 4. Backfill existing legacy media_metadata into store_media_assets and product_media_references
DO $$
DECLARE
    rec RECORD;
    v_store_id UUID;
    v_asset_id UUID;
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'media_metadata') THEN
        FOR rec IN SELECT * FROM media_metadata LOOP
            SELECT store_id INTO v_store_id
            FROM seller_listings
            WHERE product_id = rec.product_id
            LIMIT 1;

            IF v_store_id IS NOT NULL THEN
                v_asset_id := gen_random_uuid();

                INSERT INTO store_media_assets (
                    id, store_id, checksum_sha256, storage_key, content_type, byte_size, original_filename, status
                ) VALUES (
                    v_asset_id,
                    v_store_id,
                    lpad(replace(rec.id::text, '-', ''), 64, '0'),
                    COALESCE(rec.storage_key, 'legacy/' || rec.id::text),
                    COALESCE(rec.media_type, 'image/jpeg'),
                    1,
                    'legacy_image',
                    'ready'
                ) ON CONFLICT (storage_key) DO UPDATE SET updated_at = now();

                INSERT INTO product_media_references (
                    id, store_id, product_id, asset_id, alt_text, sort_order, is_primary, created_at, updated_at
                ) VALUES (
                    rec.id,
                    v_store_id,
                    rec.product_id,
                    v_asset_id,
                    COALESCE(rec.alt_text, ''),
                    COALESCE(rec.sort_order, 0),
                    COALESCE(rec.is_primary, false),
                    rec.created_at,
                    rec.updated_at
                ) ON CONFLICT (store_id, product_id, asset_id) DO NOTHING;
            END IF;
        END LOOP;
    END IF;
END $$;
