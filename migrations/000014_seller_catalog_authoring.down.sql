DROP TABLE IF EXISTS seller_listing_presentations;

DROP INDEX IF EXISTS media_metadata_product_primary_uidx;

ALTER TABLE media_metadata
    DROP COLUMN IF EXISTS is_primary,
    DROP COLUMN IF EXISTS storage_key;

DROP INDEX IF EXISTS seller_products_seller_code_uidx;
DROP INDEX IF EXISTS seller_products_seller_id_idx;
DROP TABLE IF EXISTS seller_products;
