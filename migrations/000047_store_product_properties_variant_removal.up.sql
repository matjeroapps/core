-- Store-owned custom product properties and history-preserving variant removal.
-- Existing global attributes remain NULL-owned system options.

ALTER TABLE attributes
    ADD COLUMN IF NOT EXISTS store_id UUID REFERENCES stores(id) ON DELETE CASCADE;

ALTER TABLE attributes
    DROP CONSTRAINT IF EXISTS attributes_code_key;

CREATE UNIQUE INDEX IF NOT EXISTS attributes_system_code_uidx
    ON attributes (code)
    WHERE store_id IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS attributes_store_code_uidx
    ON attributes (store_id, code)
    WHERE store_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS attributes_store_status_idx
    ON attributes (store_id, status);

ALTER TABLE variants
    ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS variants_product_current_idx
    ON variants (product_id, code)
    WHERE deleted_at IS NULL;

-- Existing non-active variants were intentionally removed from the merchant catalog.
-- Keep their variant, SKU, mapping, order, inventory, and reservation rows intact.
UPDATE variants
SET deleted_at = COALESCE(updated_at, created_at, now())
WHERE status <> 'active'
  AND deleted_at IS NULL;

-- A removed variant cannot retain a sellable SKU. SKU rows remain for history.
UPDATE skus sku
SET status = 'inactive', updated_at = now()
WHERE sku.status = 'active'
  AND EXISTS (
      SELECT 1
      FROM variants variant
      WHERE variant.id = sku.variant_id
        AND variant.deleted_at IS NOT NULL
  );
