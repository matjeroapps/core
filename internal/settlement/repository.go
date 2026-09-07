package settlement

import (
	"context"
	"errors"
	"fmt"
	"time"

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
		return fmt.Errorf("database pool is nil")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := fn(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

func (r Repository) SaveSettlement(ctx context.Context, exec DBExecutor, s Settlement) error {
	db := r.getExec(exec)

	_, err := db.Exec(ctx, `
		INSERT INTO settlements (
			id, settlement_period_id, account_id, currency,
			gross_amount_minor, adjustment_amount_minor, net_amount_minor,
			status, created_at, calculated_at, finalized_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
		)
		ON CONFLICT (settlement_period_id, account_id) DO UPDATE SET
			gross_amount_minor = EXCLUDED.gross_amount_minor,
			adjustment_amount_minor = EXCLUDED.adjustment_amount_minor,
			net_amount_minor = EXCLUDED.net_amount_minor,
			status = EXCLUDED.status,
			calculated_at = EXCLUDED.calculated_at,
			finalized_at = EXCLUDED.finalized_at
		WHERE settlements.status != 'FINALIZED'
	`, s.ID, s.SettlementPeriodID, s.AccountID, s.Currency,
		s.GrossAmountMinor, s.AdjustmentAmountMinor, s.NetAmountMinor,
		s.Status, s.CreatedAt, s.CalculatedAt, s.FinalizedAt)

	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) {
			if pgErr.Code == "P0001" || pgErr.Message == "Finalized settlement records are immutable and cannot be updated or deleted" {
				return ErrFinalizedSettlementImmutable
			}
		}
		return fmt.Errorf("save settlement: %w", err)
	}

	return nil
}

func (r Repository) FinalizeSettlementsForPeriod(ctx context.Context, exec DBExecutor, periodID string) ([]Settlement, error) {
	db := r.getExec(exec)
	now := time.Now().UTC()

	rows, err := db.Query(ctx, `
		UPDATE settlements
		SET status = 'FINALIZED', finalized_at = $1
		WHERE settlement_period_id = $2 AND status = 'CALCULATED'
		RETURNING id, settlement_period_id, account_id, currency, gross_amount_minor, adjustment_amount_minor, net_amount_minor, status, created_at, calculated_at, finalized_at
	`, now, periodID)
	if err != nil {
		return nil, fmt.Errorf("finalize settlements query: %w", err)
	}
	defer rows.Close()

	var finalized []Settlement
	for rows.Next() {
		var s Settlement
		err := rows.Scan(
			&s.ID, &s.SettlementPeriodID, &s.AccountID, &s.Currency,
			&s.GrossAmountMinor, &s.AdjustmentAmountMinor, &s.NetAmountMinor,
			&s.Status, &s.CreatedAt, &s.CalculatedAt, &s.FinalizedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan finalized settlement: %w", err)
		}
		finalized = append(finalized, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate finalized settlements: %w", err)
	}

	return finalized, nil
}

func (r Repository) GetSettlementByID(ctx context.Context, exec DBExecutor, id string) (*Settlement, error) {
	db := r.getExec(exec)

	var s Settlement
	err := db.QueryRow(ctx, `
		SELECT id, settlement_period_id, account_id, currency,
		       gross_amount_minor, adjustment_amount_minor, net_amount_minor,
		       status, created_at, calculated_at, finalized_at
		FROM settlements
		WHERE id = $1
	`, id).Scan(
		&s.ID, &s.SettlementPeriodID, &s.AccountID, &s.Currency,
		&s.GrossAmountMinor, &s.AdjustmentAmountMinor, &s.NetAmountMinor,
		&s.Status, &s.CreatedAt, &s.CalculatedAt, &s.FinalizedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSettlementNotFound
		}
		return nil, fmt.Errorf("get settlement by id: %w", err)
	}

	return &s, nil
}

func (r Repository) GetSettlementByPeriodAndAccount(ctx context.Context, exec DBExecutor, periodID, accountID string) (*Settlement, error) {
	db := r.getExec(exec)

	var s Settlement
	err := db.QueryRow(ctx, `
		SELECT id, settlement_period_id, account_id, currency,
		       gross_amount_minor, adjustment_amount_minor, net_amount_minor,
		       status, created_at, calculated_at, finalized_at
		FROM settlements
		WHERE settlement_period_id = $1 AND account_id = $2
	`, periodID, accountID).Scan(
		&s.ID, &s.SettlementPeriodID, &s.AccountID, &s.Currency,
		&s.GrossAmountMinor, &s.AdjustmentAmountMinor, &s.NetAmountMinor,
		&s.Status, &s.CreatedAt, &s.CalculatedAt, &s.FinalizedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSettlementNotFound
		}
		return nil, fmt.Errorf("get settlement by period and account: %w", err)
	}

	return &s, nil
}

func (r Repository) ListSettlementsByAccount(ctx context.Context, exec DBExecutor, accountID string, page commerce.Page) ([]Settlement, error) {
	db := r.getExec(exec)
	limit := page.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := page.Offset

	rows, err := db.Query(ctx, `
		SELECT id, settlement_period_id, account_id, currency,
		       gross_amount_minor, adjustment_amount_minor, net_amount_minor,
		       status, created_at, calculated_at, finalized_at
		FROM settlements
		WHERE account_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, accountID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list settlements by account: %w", err)
	}
	defer rows.Close()

	var settlements []Settlement
	for rows.Next() {
		var s Settlement
		err := rows.Scan(
			&s.ID, &s.SettlementPeriodID, &s.AccountID, &s.Currency,
			&s.GrossAmountMinor, &s.AdjustmentAmountMinor, &s.NetAmountMinor,
			&s.Status, &s.CreatedAt, &s.CalculatedAt, &s.FinalizedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan settlement by account: %w", err)
		}
		settlements = append(settlements, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settlements by account: %w", err)
	}

	return settlements, nil
}

func (r Repository) ListSettlementsByPeriod(ctx context.Context, exec DBExecutor, periodID string, page commerce.Page) ([]Settlement, error) {
	db := r.getExec(exec)
	limit := page.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := page.Offset

	rows, err := db.Query(ctx, `
		SELECT id, settlement_period_id, account_id, currency,
		       gross_amount_minor, adjustment_amount_minor, net_amount_minor,
		       status, created_at, calculated_at, finalized_at
		FROM settlements
		WHERE settlement_period_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, periodID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("list settlements by period: %w", err)
	}
	defer rows.Close()

	var settlements []Settlement
	for rows.Next() {
		var s Settlement
		err := rows.Scan(
			&s.ID, &s.SettlementPeriodID, &s.AccountID, &s.Currency,
			&s.GrossAmountMinor, &s.AdjustmentAmountMinor, &s.NetAmountMinor,
			&s.Status, &s.CreatedAt, &s.CalculatedAt, &s.FinalizedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan settlement by period: %w", err)
		}
		settlements = append(settlements, s)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settlements by period: %w", err)
	}

	return settlements, nil
}

func (r Repository) GetSettlementPeriodByID(ctx context.Context, exec DBExecutor, id string) (*SettlementPeriod, error) {
	db := r.getExec(exec)

	var p SettlementPeriod
	err := db.QueryRow(ctx, `
		SELECT id, start_date, end_date, status, created_at, closed_at
		FROM settlement_periods
		WHERE id = $1
	`, id).Scan(&p.ID, &p.StartDate, &p.EndDate, &p.Status, &p.CreatedAt, &p.ClosedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSettlementPeriodNotFound
		}
		return nil, fmt.Errorf("get settlement period: %w", err)
	}

	return &p, nil
}

func (r Repository) UpdateSettlementPeriodStatus(ctx context.Context, exec DBExecutor, id string, status PeriodStatus) (*SettlementPeriod, error) {
	db := r.getExec(exec)
	now := time.Now().UTC()

	var closedAt *time.Time
	if status == PeriodStatusClosed || status == PeriodStatusFinalized {
		closedAt = &now
	}

	var p SettlementPeriod
	err := db.QueryRow(ctx, `
		UPDATE settlement_periods
		SET status = $1, closed_at = COALESCE($2, closed_at)
		WHERE id = $3
		RETURNING id, start_date, end_date, status, created_at, closed_at
	`, status, closedAt, id).Scan(&p.ID, &p.StartDate, &p.EndDate, &p.Status, &p.CreatedAt, &p.ClosedAt)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrSettlementPeriodNotFound
		}
		return nil, fmt.Errorf("update settlement period status: %w", err)
	}

	return &p, nil
}
