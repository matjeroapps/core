-- Phase 12: Marketplace Financial Rules Foundation Schema

CREATE TABLE IF NOT EXISTS financial_rules (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    rule_type TEXT NOT NULL CHECK (rule_type IN ('PERCENTAGE', 'FIXED_AMOUNT')),
    percentage NUMERIC(5, 2) NOT NULL DEFAULT 0.00 CHECK (percentage >= 0 AND percentage <= 100.00),
    fixed_amount_minor BIGINT NOT NULL DEFAULT 0 CHECK (fixed_amount_minor >= 0),
    currency CHAR(3) NOT NULL,
    allocation_type TEXT NOT NULL CHECK (allocation_type IN ('SELLER_SHARE', 'SUPPLIER_SHARE', 'PLATFORM_SHARE')),
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'INACTIVE')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX IF NOT EXISTS financial_rules_currency_idx ON financial_rules (currency);
CREATE INDEX IF NOT EXISTS financial_rules_status_idx ON financial_rules (status);

CREATE TABLE IF NOT EXISTS settlement_allocations (
    id UUID PRIMARY KEY,
    settlement_id UUID NOT NULL REFERENCES settlements(id) ON DELETE CASCADE,
    account_id UUID NOT NULL REFERENCES ledger_accounts(id) ON DELETE RESTRICT,
    allocation_type TEXT NOT NULL CHECK (allocation_type IN ('SELLER_SHARE', 'SUPPLIER_SHARE', 'PLATFORM_SHARE')),
    amount_minor BIGINT NOT NULL CHECK (amount_minor >= 0),
    currency CHAR(3) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT settlement_allocations_settlement_type_uidx UNIQUE (settlement_id, allocation_type)
);

CREATE INDEX IF NOT EXISTS settlement_allocations_settlement_id_idx ON settlement_allocations (settlement_id);
CREATE INDEX IF NOT EXISTS settlement_allocations_account_id_idx ON settlement_allocations (account_id);

CREATE OR REPLACE FUNCTION prevent_finalized_settlement_allocation_modification()
RETURNS TRIGGER AS $$
DECLARE
    settlement_status TEXT;
BEGIN
    SELECT status INTO settlement_status FROM settlements WHERE id = OLD.settlement_id;
    IF settlement_status = 'FINALIZED' THEN
        RAISE EXCEPTION 'Allocations for finalized settlements are immutable and cannot be updated or deleted';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_immutable_finalized_settlement_allocations
BEFORE UPDATE OR DELETE ON settlement_allocations
FOR EACH ROW EXECUTE FUNCTION prevent_finalized_settlement_allocation_modification();
