CREATE TABLE IF NOT EXISTS account_balances (
    id UUID PRIMARY KEY,
    account_id UUID NOT NULL REFERENCES ledger_accounts(id) ON DELETE RESTRICT,
    currency CHAR(3) NOT NULL,
    debit_total_minor BIGINT NOT NULL DEFAULT 0 CHECK (debit_total_minor >= 0),
    credit_total_minor BIGINT NOT NULL DEFAULT 0 CHECK (credit_total_minor >= 0),
    balance_minor BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    CONSTRAINT account_balances_account_currency_uidx UNIQUE (account_id, currency)
);

CREATE INDEX IF NOT EXISTS account_balances_account_id_idx ON account_balances (account_id);
CREATE INDEX IF NOT EXISTS account_balances_updated_at_idx ON account_balances (updated_at);

CREATE TABLE IF NOT EXISTS settlement_periods (
    id UUID PRIMARY KEY,
    start_date TIMESTAMPTZ NOT NULL,
    end_date TIMESTAMPTZ NOT NULL,
    status TEXT NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'CLOSED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    closed_at TIMESTAMPTZ NULL,
    CONSTRAINT settlement_periods_dates_check CHECK (end_date > start_date)
);

CREATE INDEX IF NOT EXISTS settlement_periods_status_idx ON settlement_periods (status);
CREATE INDEX IF NOT EXISTS settlement_periods_dates_idx ON settlement_periods (start_date, end_date);
