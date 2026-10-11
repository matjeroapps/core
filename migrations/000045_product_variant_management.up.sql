-- Product variant management invariants.
-- SKU store ownership is derived by Core from the product/listing relationship.

ALTER TABLE skus
    ADD COLUMN IF NOT EXISTS store_id UUID REFERENCES stores(id) ON DELETE RESTRICT;

UPDATE skus sku
SET store_id = listing.store_id
FROM variants variant
JOIN seller_listings listing ON listing.product_id = variant.product_id
WHERE sku.variant_id = variant.id
  AND sku.store_id IS NULL;

ALTER TABLE skus
    DROP CONSTRAINT IF EXISTS skus_code_key;

CREATE UNIQUE INDEX IF NOT EXISTS skus_store_code_uidx
    ON skus (store_id, code)
    WHERE store_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS skus_store_id_idx
    ON skus (store_id)
    WHERE store_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS variant_attribute_values_product_signature_uidx
    ON variant_attribute_values (variant_id, attribute_id, attribute_value_id);
