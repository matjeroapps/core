-- Migration 000045 Down: restore global SKU code uniqueness.
--
-- The up migration replaced the legacy global skus.code uniqueness with
-- store-scoped SKU uniqueness on (store_id, code). Under the clarified
-- store-scoped model the same code may legitimately exist in different stores,
-- so the legacy invariant only holds while codes are globally unique. Fail the
-- rollback before touching any schema object when the invariant would be
-- violated: nothing is then dropped or destructively restored, and operators
-- must resolve cross-store duplicate codes before re-running the rollback.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM skus
        GROUP BY code
        HAVING count(*) > 1
           AND (count(DISTINCT store_id) > 1 OR count(*) <> count(store_id))
    ) THEN
        RAISE EXCEPTION 'migration 000045 down preflight failed: SKU codes duplicated across stores violate the legacy global skus.code uniqueness; resolve the duplicate codes before rolling back';
    END IF;
END $$;

DROP INDEX IF EXISTS variant_attribute_values_product_signature_uidx;
DROP INDEX IF EXISTS skus_store_id_idx;
DROP INDEX IF EXISTS skus_store_code_uidx;

ALTER TABLE skus
    ADD CONSTRAINT skus_code_key UNIQUE (code);

ALTER TABLE skus
    DROP COLUMN IF EXISTS store_id;
