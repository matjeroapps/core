-- Enforce strict 1:1 Supplier <-> Seller Retail Capability Affiliation with ON DELETE RESTRICT.

CREATE TABLE IF NOT EXISTS supplier_seller_affiliations (
    supplier_id UUID PRIMARY KEY REFERENCES suppliers(id) ON DELETE RESTRICT,
    seller_id   UUID UNIQUE NOT NULL REFERENCES sellers(id) ON DELETE RESTRICT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE supplier_seller_affiliations
    DROP CONSTRAINT IF EXISTS supplier_seller_affiliations_supplier_id_fkey,
    DROP CONSTRAINT IF EXISTS supplier_seller_affiliations_seller_id_fkey;

ALTER TABLE supplier_seller_affiliations
    ADD CONSTRAINT supplier_seller_affiliations_supplier_id_fkey
        FOREIGN KEY (supplier_id) REFERENCES suppliers(id) ON DELETE RESTRICT,
    ADD CONSTRAINT supplier_seller_affiliations_seller_id_fkey
        FOREIGN KEY (seller_id) REFERENCES sellers(id) ON DELETE RESTRICT;
