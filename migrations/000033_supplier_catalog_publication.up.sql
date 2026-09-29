ALTER TABLE supplier_offers
    ADD COLUMN IF NOT EXISTS minimum_order_quantity BIGINT NOT NULL DEFAULT 1;

ALTER TABLE supplier_offers
    ADD CONSTRAINT supplier_offers_minimum_order_quantity_positive
    CHECK (minimum_order_quantity > 0) NOT VALID;

ALTER TABLE supplier_offers
    VALIDATE CONSTRAINT supplier_offers_minimum_order_quantity_positive;
