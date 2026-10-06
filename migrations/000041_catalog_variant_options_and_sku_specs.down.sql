-- Migration 000041 Down: Drop variant options junction and SKU physical specifications

ALTER TABLE product_translations
    DROP COLUMN IF EXISTS meta_description,
    DROP COLUMN IF EXISTS meta_title;

DROP INDEX IF EXISTS idx_skus_barcode;

ALTER TABLE skus
    DROP COLUMN IF EXISTS price_minor_units,
    DROP COLUMN IF EXISTS height_mm,
    DROP COLUMN IF EXISTS width_mm,
    DROP COLUMN IF EXISTS length_mm,
    DROP COLUMN IF EXISTS weight_grams;

DROP TABLE IF EXISTS variant_attribute_values;
