-- Migration 000041: Variant options junction table and SKU physical specifications

CREATE TABLE IF NOT EXISTS variant_attribute_values (
    variant_id UUID NOT NULL REFERENCES variants(id) ON DELETE CASCADE,
    attribute_id UUID NOT NULL REFERENCES attributes(id) ON DELETE RESTRICT,
    attribute_value_id UUID NOT NULL REFERENCES attribute_values(id) ON DELETE RESTRICT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (variant_id, attribute_id),
    UNIQUE (variant_id, attribute_value_id)
);

CREATE INDEX IF NOT EXISTS idx_variant_attribute_values_value
    ON variant_attribute_values (attribute_value_id);

ALTER TABLE skus
    ADD COLUMN IF NOT EXISTS weight_grams INTEGER CHECK (weight_grams >= 0),
    ADD COLUMN IF NOT EXISTS length_mm INTEGER CHECK (length_mm >= 0),
    ADD COLUMN IF NOT EXISTS width_mm INTEGER CHECK (width_mm >= 0),
    ADD COLUMN IF NOT EXISTS height_mm INTEGER CHECK (height_mm >= 0),
    ADD COLUMN IF NOT EXISTS price_minor_units BIGINT CHECK (price_minor_units >= 0);

CREATE INDEX IF NOT EXISTS idx_skus_barcode ON skus (barcode) WHERE barcode IS NOT NULL;

ALTER TABLE product_translations
    ADD COLUMN IF NOT EXISTS meta_title TEXT,
    ADD COLUMN IF NOT EXISTS meta_description TEXT;
