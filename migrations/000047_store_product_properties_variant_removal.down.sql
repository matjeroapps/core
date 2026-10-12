-- Refuse rollback if merchant-created properties exist; globalizing them would
-- expose tenant data and may collide with existing system property codes.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM attributes WHERE store_id IS NOT NULL) THEN
        RAISE EXCEPTION 'migration 000047 down preflight failed: store-owned properties exist; preserve them or migrate them explicitly before rollback';
    END IF;
END $$;

-- Preserve user removals in the legacy lifecycle as archived variants. Existing
-- inactive/archived rows retain their original statuses.
UPDATE variants
SET status = 'archived', updated_at = now()
WHERE deleted_at IS NOT NULL
  AND status = 'active';

UPDATE skus sku
SET status = 'inactive', updated_at = now()
WHERE sku.status = 'active'
  AND EXISTS (
      SELECT 1 FROM variants variant
      WHERE variant.id = sku.variant_id
        AND variant.deleted_at IS NOT NULL
  );

DROP INDEX IF EXISTS variants_product_current_idx;
ALTER TABLE variants DROP COLUMN IF EXISTS deleted_at;
DROP INDEX IF EXISTS attributes_store_status_idx;
DROP INDEX IF EXISTS attributes_store_code_uidx;
DROP INDEX IF EXISTS attributes_system_code_uidx;
ALTER TABLE attributes DROP COLUMN IF EXISTS store_id;
ALTER TABLE attributes ADD CONSTRAINT attributes_code_key UNIQUE (code);
