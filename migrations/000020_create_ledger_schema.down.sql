DROP TRIGGER IF EXISTS trg_immutable_journal_lines ON journal_lines;
DROP TRIGGER IF EXISTS trg_immutable_journal_entries ON journal_entries;
DROP FUNCTION IF EXISTS prevent_ledger_modification();
DROP TABLE IF EXISTS journal_lines;
DROP TABLE IF EXISTS journal_entries;
DROP TABLE IF EXISTS ledger_accounts;
