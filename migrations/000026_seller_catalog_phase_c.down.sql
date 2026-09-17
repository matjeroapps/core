-- Revert Phase C: Store-Scoped Media Library and Reusable Product References

DROP INDEX IF EXISTS idx_media_upload_intents_store_checksum;
DROP INDEX IF EXISTS idx_media_upload_intents_store_client_upload;

ALTER TABLE media_upload_intents
    DROP COLUMN IF EXISTS original_filename,
    DROP COLUMN IF EXISTS byte_size,
    DROP COLUMN IF EXISTS checksum_sha256,
    DROP COLUMN IF EXISTS request_fingerprint,
    DROP COLUMN IF EXISTS client_upload_id;

DROP TABLE IF EXISTS product_media_references;
DROP TABLE IF EXISTS store_media_assets;
