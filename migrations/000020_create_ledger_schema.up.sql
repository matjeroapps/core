CREATE TABLE IF NOT EXISTS ledger_accounts (
    id UUID PRIMARY KEY,
    account_code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    account_type TEXT NOT NULL CHECK (account_type IN ('ASSET', 'LIABILITY', 'REVENUE', 'EXPENSE', 'EQUITY')),
    currency CHAR(3) NOT NULL,
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'INACTIVE', 'FROZEN')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

CREATE INDEX IF NOT EXISTS ledger_accounts_account_code_idx ON ledger_accounts (account_code);
CREATE INDEX IF NOT EXISTS ledger_accounts_status_idx ON ledger_accounts (status);

CREATE TABLE IF NOT EXISTS journal_entries (
    id UUID PRIMARY KEY,
    reference_type TEXT NOT NULL,
    reference_id TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    currency CHAR(3) NOT NULL,
    posted_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT journal_entries_reference_uidx UNIQUE (reference_type, reference_id)
);

CREATE INDEX IF NOT EXISTS journal_entries_reference_idx ON journal_entries (reference_type, reference_id);
CREATE INDEX IF NOT EXISTS journal_entries_posted_at_idx ON journal_entries (posted_at);

CREATE TABLE IF NOT EXISTS journal_lines (
    id UUID PRIMARY KEY,
    journal_entry_id UUID NOT NULL REFERENCES journal_entries(id) ON DELETE RESTRICT,
    account_id UUID NOT NULL REFERENCES ledger_accounts(id) ON DELETE RESTRICT,
    debit_amount_minor BIGINT NOT NULL DEFAULT 0 CHECK (debit_amount_minor >= 0),
    credit_amount_minor BIGINT NOT NULL DEFAULT 0 CHECK (credit_amount_minor >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT journal_lines_amounts_check CHECK (
        (debit_amount_minor > 0 AND credit_amount_minor = 0) OR
        (credit_amount_minor > 0 AND debit_amount_minor = 0)
    )
);

CREATE INDEX IF NOT EXISTS journal_lines_entry_id_idx ON journal_lines (journal_entry_id);
CREATE INDEX IF NOT EXISTS journal_lines_account_id_idx ON journal_lines (account_id);

CREATE OR REPLACE FUNCTION prevent_ledger_modification()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'Journal entries and lines are immutable and cannot be updated or deleted';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_immutable_journal_entries
BEFORE UPDATE OR DELETE ON journal_entries
FOR EACH ROW EXECUTE FUNCTION prevent_ledger_modification();

CREATE TRIGGER trg_immutable_journal_lines
BEFORE UPDATE OR DELETE ON journal_lines
FOR EACH ROW EXECUTE FUNCTION prevent_ledger_modification();
