package balance

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
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

func (r Repository) UpsertAccountBalanceTx(ctx context.Context, tx pgx.Tx, accountID, currency string, debitDelta, creditDelta int64) error {
	netBalanceDelta := debitDelta - creditDelta

	_, err := tx.Exec(ctx, `
		INSERT INTO account_balances (
			id, account_id, currency, debit_total_minor, credit_total_minor, balance_minor, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp())
		ON CONFLICT (account_id, currency) DO UPDATE SET
			debit_total_minor = account_balances.debit_total_minor + EXCLUDED.debit_total_minor,
			credit_total_minor = account_balances.credit_total_minor + EXCLUDED.credit_total_minor,
			balance_minor = account_balances.balance_minor + EXCLUDED.balance_minor,
			updated_at = clock_timestamp()
	`, uuid.NewString(), accountID, currency, debitDelta, creditDelta, netBalanceDelta)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign key violation
			return ErrAccountNotFound
		}
		return fmt.Errorf("upsert account balance: %w", err)
	}

	return nil
}

func (r Repository) GetAccountBalance(ctx context.Context, exec DBExecutor, accountID string) (*AccountBalance, error) {
	db := r.getExec(exec)

	var bal AccountBalance
	err := db.QueryRow(ctx, `
		SELECT id, account_id, currency, debit_total_minor, credit_total_minor, balance_minor, updated_at
		FROM account_balances
		WHERE account_id = $1
	`, accountID).Scan(
		&bal.ID,
		&bal.AccountID,
		&bal.Currency,
		&bal.DebitTotalMinor,
		&bal.CreditTotalMinor,
		&bal.BalanceMinor,
		&bal.UpdatedAt,
	)

	if err == nil {
		return &bal, nil
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("query account balance by account_id: %w", err)
	}

	// Balance record does not exist yet. Verify if account exists in ledger_accounts.
	var currency string
	var createdAt pgx.Row
	err = db.QueryRow(ctx, `
		SELECT currency FROM ledger_accounts WHERE id = $1
	`, accountID).Scan(&currency)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("check ledger account existence: %w", err)
	}

	_ = createdAt
	// Account exists but has 0 balance record projected so far
	return &AccountBalance{
		ID:               "",
		AccountID:        accountID,
		Currency:         currency,
		DebitTotalMinor:  0,
		CreditTotalMinor: 0,
		BalanceMinor:     0,
	}, nil
}

func (r Repository) ListAccountBalances(ctx context.Context, exec DBExecutor, page commerce.Page) ([]AccountBalance, error) {
	db := r.getExec(exec)

	limit := page.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := page.Offset
	if offset < 0 {
		offset = 0
	}

	rows, err := db.Query(ctx, `
		SELECT id, account_id, currency, debit_total_minor, credit_total_minor, balance_minor, updated_at
		FROM account_balances
		ORDER BY updated_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query account balances: %w", err)
	}
	defer rows.Close()

	var balances []AccountBalance
	for rows.Next() {
		var b AccountBalance
		if err := rows.Scan(
			&b.ID,
			&b.AccountID,
			&b.Currency,
			&b.DebitTotalMinor,
			&b.CreditTotalMinor,
			&b.BalanceMinor,
			&b.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan account balance: %w", err)
		}
		balances = append(balances, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error listing account balances: %w", err)
	}

	if balances == nil {
		balances = []AccountBalance{}
	}

	return balances, nil
}

func (r Repository) CreateSettlementPeriod(ctx context.Context, exec DBExecutor, period SettlementPeriod) error {
	db := r.getExec(exec)

	_, err := db.Exec(ctx, `
		INSERT INTO settlement_periods (
			id, start_date, end_date, status, created_at, closed_at
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, period.ID, period.StartDate, period.EndDate, string(period.Status), period.CreatedAt, period.ClosedAt)

	if err != nil {
		return fmt.Errorf("insert settlement period: %w", err)
	}

	return nil
}

func (r Repository) GetSettlementPeriodByID(ctx context.Context, exec DBExecutor, id string) (*SettlementPeriod, error) {
	db := r.getExec(exec)

	var p SettlementPeriod
	var statusStr string
	err := db.QueryRow(ctx, `
		SELECT id, start_date, end_date, status, created_at, closed_at
		FROM settlement_periods
		WHERE id = $1
	`, id).Scan(
		&p.ID,
		&p.StartDate,
		&p.EndDate,
		&statusStr,
		&p.CreatedAt,
		&p.ClosedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSettlementPeriodNotFound
		}
		return nil, fmt.Errorf("get settlement period by id: %w", err)
	}

	p.Status = SettlementPeriodStatus(statusStr)
	return &p, nil
}

func (r Repository) CloseSettlementPeriod(ctx context.Context, exec DBExecutor, id string) (*SettlementPeriod, error) {
	db := r.getExec(exec)

	p, err := r.GetSettlementPeriodByID(ctx, db, id)
	if err != nil {
		return nil, err
	}

	if p.Status == SettlementPeriodStatusClosed {
		return nil, ErrSettlementPeriodAlreadyClosed
	}

	tag, err := db.Exec(ctx, `
		UPDATE settlement_periods
		SET status = 'CLOSED', closed_at = clock_timestamp()
		WHERE id = $1 AND status = 'OPEN'
	`, id)
	if err != nil {
		return nil, fmt.Errorf("update settlement period status: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return nil, ErrSettlementPeriodAlreadyClosed
	}

	return r.GetSettlementPeriodByID(ctx, db, id)
}

func (r Repository) ListSettlementPeriods(ctx context.Context, exec DBExecutor, page commerce.Page) ([]SettlementPeriod, error) {
	db := r.getExec(exec)

	limit := page.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := page.Offset
	if offset < 0 {
		offset = 0
	}

	rows, err := db.Query(ctx, `
		SELECT id, start_date, end_date, status, created_at, closed_at
		FROM settlement_periods
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("query settlement periods: %w", err)
	}
	defer rows.Close()

	var periods []SettlementPeriod
	for rows.Next() {
		var p SettlementPeriod
		var statusStr string
		if err := rows.Scan(
			&p.ID,
			&p.StartDate,
			&p.EndDate,
			&statusStr,
			&p.CreatedAt,
			&p.ClosedAt,
		); err != nil {
			return nil, fmt.Errorf("scan settlement period: %w", err)
		}
		p.Status = SettlementPeriodStatus(statusStr)
		periods = append(periods, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error listing settlement periods: %w", err)
	}

	if periods == nil {
		periods = []SettlementPeriod{}
	}

	return periods, nil
}
