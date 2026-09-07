package finance

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/matjeroapps/core/modules/commerce"
)

type DBExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type DBPool interface {
	DBExecutor
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Repository struct {
	pool DBPool
}

func NewRepository(pool DBPool) Repository {
	return Repository{pool: pool}
}

func (r Repository) getExec(exec DBExecutor) DBExecutor {
	if exec != nil {
		return exec
	}
	return r.pool
}

func (r Repository) WithTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
	if r.pool == nil {
		return errors.New("database pool is not initialized")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := fn(ctx, tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r Repository) CreateAccount(ctx context.Context, exec DBExecutor, account Account) error {
	db := r.getExec(exec)

	_, err := db.Exec(ctx, `
		INSERT INTO ledger_accounts (
			id, account_code, name, account_type, currency, status, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, account.ID, account.AccountCode, account.Name, string(account.AccountType), account.Currency, string(account.Status), account.CreatedAt, account.UpdatedAt)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique violation
			return ErrDuplicateAccountCode
		}
		return fmt.Errorf("insert ledger account: %w", err)
	}

	return nil
}

func (r Repository) GetAccountByID(ctx context.Context, exec DBExecutor, id string) (*Account, error) {
	db := r.getExec(exec)
	var account Account
	var typeStr, statusStr string

	err := db.QueryRow(ctx, `
		SELECT id, account_code, name, account_type, currency, status, created_at, updated_at
		FROM ledger_accounts
		WHERE id = $1
	`, id).Scan(
		&account.ID,
		&account.AccountCode,
		&account.Name,
		&typeStr,
		&account.Currency,
		&statusStr,
		&account.CreatedAt,
		&account.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("get ledger account by id: %w", err)
	}

	account.AccountType = AccountType(typeStr)
	account.Status = AccountStatus(statusStr)
	return &account, nil
}

func (r Repository) GetAccountsByIDs(ctx context.Context, exec DBExecutor, ids []string) (map[string]Account, error) {
	db := r.getExec(exec)
	rows, err := db.Query(ctx, `
		SELECT id, account_code, name, account_type, currency, status, created_at, updated_at
		FROM ledger_accounts
		WHERE id = ANY($1::uuid[])
	`, ids)
	if err != nil {
		return nil, fmt.Errorf("query accounts by ids: %w", err)
	}
	defer rows.Close()

	result := make(map[string]Account)
	for rows.Next() {
		var a Account
		var typeStr, statusStr string
		if err := rows.Scan(
			&a.ID,
			&a.AccountCode,
			&a.Name,
			&typeStr,
			&a.Currency,
			&statusStr,
			&a.CreatedAt,
			&a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ledger account: %w", err)
		}
		a.AccountType = AccountType(typeStr)
		a.Status = AccountStatus(statusStr)
		result[a.ID] = a
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error querying ledger accounts: %w", err)
	}

	return result, nil
}

func (r Repository) ListAccounts(ctx context.Context, exec DBExecutor, page commerce.Page) ([]Account, error) {
	db := r.getExec(exec)

	rows, err := db.Query(ctx, `
		SELECT id, account_code, name, account_type, currency, status, created_at, updated_at
		FROM ledger_accounts
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, page.Limit, page.Offset)
	if err != nil {
		return nil, fmt.Errorf("query ledger accounts: %w", err)
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var a Account
		var typeStr, statusStr string
		if err := rows.Scan(
			&a.ID,
			&a.AccountCode,
			&a.Name,
			&typeStr,
			&a.Currency,
			&statusStr,
			&a.CreatedAt,
			&a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan ledger account: %w", err)
		}
		a.AccountType = AccountType(typeStr)
		a.Status = AccountStatus(statusStr)
		accounts = append(accounts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error listing ledger accounts: %w", err)
	}
	return accounts, nil
}

func (r Repository) PostJournalEntryTx(ctx context.Context, tx pgx.Tx, entry JournalEntry) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO journal_entries (
			id, reference_type, reference_id, description, currency, posted_at, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`, entry.ID, entry.ReferenceType, entry.ReferenceID, entry.Description, entry.Currency, entry.PostedAt, entry.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique constraint violation on (reference_type, reference_id)
			return ErrDuplicatePosting
		}
		return fmt.Errorf("insert journal entry: %w", err)
	}

	for _, line := range entry.Lines {
		_, err := tx.Exec(ctx, `
			INSERT INTO journal_lines (
				id, journal_entry_id, account_id, debit_amount_minor, credit_amount_minor, created_at
			)
			VALUES ($1, $2, $3, $4, $5, $6)
		`, line.ID, entry.ID, line.AccountID, line.DebitAmountMinor, line.CreditAmountMinor, line.CreatedAt)
		if err != nil {
			return fmt.Errorf("insert journal line: %w", err)
		}
	}

	return nil
}

func (r Repository) GetJournalEntryByID(ctx context.Context, exec DBExecutor, id string) (*JournalEntry, error) {
	db := r.getExec(exec)
	var entry JournalEntry

	err := db.QueryRow(ctx, `
		SELECT id, reference_type, reference_id, description, currency, posted_at, created_at
		FROM journal_entries
		WHERE id = $1
	`, id).Scan(
		&entry.ID,
		&entry.ReferenceType,
		&entry.ReferenceID,
		&entry.Description,
		&entry.Currency,
		&entry.PostedAt,
		&entry.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrJournalEntryNotFound
		}
		return nil, fmt.Errorf("get journal entry by id: %w", err)
	}

	lines, err := r.ListJournalLinesByEntryID(ctx, db, id)
	if err != nil {
		return nil, err
	}
	entry.Lines = lines

	return &entry, nil
}

func (r Repository) GetJournalEntryByReference(ctx context.Context, exec DBExecutor, refType, refID string) (*JournalEntry, error) {
	db := r.getExec(exec)
	var entry JournalEntry

	err := db.QueryRow(ctx, `
		SELECT id, reference_type, reference_id, description, currency, posted_at, created_at
		FROM journal_entries
		WHERE reference_type = $1 AND reference_id = $2
	`, refType, refID).Scan(
		&entry.ID,
		&entry.ReferenceType,
		&entry.ReferenceID,
		&entry.Description,
		&entry.Currency,
		&entry.PostedAt,
		&entry.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrJournalEntryNotFound
		}
		return nil, fmt.Errorf("get journal entry by reference: %w", err)
	}

	lines, err := r.ListJournalLinesByEntryID(ctx, db, entry.ID)
	if err != nil {
		return nil, err
	}
	entry.Lines = lines

	return &entry, nil
}

func (r Repository) ListJournalLinesByEntryID(ctx context.Context, exec DBExecutor, entryID string) ([]JournalLine, error) {
	db := r.getExec(exec)

	rows, err := db.Query(ctx, `
		SELECT id, journal_entry_id, account_id, debit_amount_minor, credit_amount_minor, created_at
		FROM journal_lines
		WHERE journal_entry_id = $1
		ORDER BY created_at ASC
	`, entryID)
	if err != nil {
		return nil, fmt.Errorf("query journal lines: %w", err)
	}
	defer rows.Close()

	var lines []JournalLine
	for rows.Next() {
		var l JournalLine
		if err := rows.Scan(
			&l.ID,
			&l.JournalEntryID,
			&l.AccountID,
			&l.DebitAmountMinor,
			&l.CreditAmountMinor,
			&l.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan journal line: %w", err)
		}
		lines = append(lines, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error listing journal lines: %w", err)
	}

	return lines, nil
}
