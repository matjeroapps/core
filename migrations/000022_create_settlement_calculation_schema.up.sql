-- Phase 11: Settlement Calculation Foundation Schema

ALTER TABLE settlement_periods DROP CONSTRAINT IF EXISTS settlement_periods_status_check;
ALTER TABLE settlement_periods ADD CONSTRAINT settlement_periods_status_check CHECK (status IN ('OPEN', 'CALCULATED', 'FINALIZED', 'CLOSED'));

CREATE TABLE IF NOT EXISTS settlements (
    id UUID PRIMARY KEY,
    settlement_period_id UUID NOT NULL REFERENCES settlement_periods(id) ON DELETE RESTRICT,
    account_id UUID NOT NULL REFERENCES ledger_accounts(id) ON DELETE RESTRICT,
    currency CHAR(3) NOT NULL,
    gross_amount_minor BIGINT NOT NULL DEFAULT 0,
    adjustment_amount_minor BIGINT NOT NULL DEFAULT 0,
    net_amount_minor BIGINT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'CALCULATED', 'FINALIZED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    calculated_at TIMESTAMPTZ NULL,
    finalized_at TIMESTAMPTZ NULL,
    CONSTRAINT settlements_net_amount_check CHECK (net_amount_minor = gross_amount_minor + adjustment_amount_minor),
    CONSTRAINT settlements_period_account_uidx UNIQUE (settlement_period_id, account_id)
);

CREATE INDEX IF NOT EXISTS settlements_period_id_idx ON settlements (settlement_period_id);
CREATE INDEX IF NOT EXISTS settlements_account_id_idx ON settlements (account_id);
CREATE INDEX IF NOT EXISTS settlements_status_idx ON settlements (status);

CREATE OR REPLACE FUNCTION prevent_finalized_settlement_modification()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.status = 'FINALIZED' THEN
        RAISE EXCEPTION 'Finalized settlement records are immutable and cannot be updated or deleted';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_immutable_finalized_settlements
BEFORE UPDATE OR DELETE ON settlements
FOR EACH ROW EXECUTE FUNCTION prevent_finalized_settlement_modification();
