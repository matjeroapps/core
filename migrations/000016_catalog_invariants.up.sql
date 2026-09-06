-- P5.8 catalog invariants, enforced at the database level:
-- 1. Exactly one active SKU per variant. The 1-active-SKU invariant was only
--    maintained in application code, which is race-prone; the partial unique
--    index makes two concurrent active SKUs impossible.
CREATE UNIQUE INDEX IF NOT EXISTS skus_variant_active_uidx
    ON skus (variant_id)
    WHERE status = 'active';

-- 2. One media metadata record per uploaded object. Upload completion retries
--    (same storage key) must return the existing media record, never duplicate
--    it.
CREATE UNIQUE INDEX IF NOT EXISTS media_metadata_storage_key_uidx
    ON media_metadata (storage_key)
    WHERE storage_key IS NOT NULL;
