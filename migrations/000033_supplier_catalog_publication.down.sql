ALTER TABLE supplier_offers
    DROP CONSTRAINT IF EXISTS supplier_offers_minimum_order_quantity_positive;

ALTER TABLE supplier_offers
    DROP COLUMN IF EXISTS minimum_order_quantity;
