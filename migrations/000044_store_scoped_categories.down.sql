-- Migration 000044 Down: Drop store-scoped category tables

DROP TABLE IF EXISTS store_product_categories;
DROP TABLE IF EXISTS store_category_translations;
DROP TABLE IF EXISTS store_categories;
