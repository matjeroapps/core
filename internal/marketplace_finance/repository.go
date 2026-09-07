package marketplace_finance

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/matjeroapps/core/internal/settlement"
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

func (r Repository) CreateFinancialRule(ctx context.Context, exec DBExecutor, rule FinancialRule) error {
	db := r.getExec(exec)

	_, err := db.Exec(ctx, `
		INSERT INTO financial_rules (
			id, name, rule_type, percentage, fixed_amount_minor,
			currency, allocation_type, status, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
		)
	`, rule.ID, rule.Name, string(rule.RuleType), rule.Percentage, rule.FixedAmountMinor,
		rule.Currency, string(rule.AllocationType), string(rule.Status), rule.CreatedAt, rule.UpdatedAt)

	if err != nil {
		return fmt.Errorf("create financial rule: %w", err)
	}
	return nil
}

func (r Repository) GetFinancialRuleByID(ctx context.Context, exec DBExecutor, id string) (*FinancialRule, error) {
	db := r.getExec(exec)

	var rule FinancialRule
	var ruleType, allocType, status string

	err := db.QueryRow(ctx, `
		SELECT id, name, rule_type, percentage, fixed_amount_minor,
		       currency, allocation_type, status, created_at, updated_at
		FROM financial_rules
		WHERE id = $1
	`, id).Scan(
		&rule.ID, &rule.Name, &ruleType, &rule.Percentage, &rule.FixedAmountMinor,
		&rule.Currency, &allocType, &status, &rule.CreatedAt, &rule.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrRuleNotFound
		}
		return nil, fmt.Errorf("get financial rule by id: %w", err)
	}

	rule.RuleType = RuleType(ruleType)
	rule.AllocationType = AllocationType(allocType)
	rule.Status = RuleStatus(status)
	return &rule, nil
}

func (r Repository) ListFinancialRules(ctx context.Context, exec DBExecutor, statusFilter string) ([]FinancialRule, error) {
	db := r.getExec(exec)
	statusFilter = strings.ToUpper(strings.TrimSpace(statusFilter))

	var query string
	var args []any

	if statusFilter != "" {
		query = `
			SELECT id, name, rule_type, percentage, fixed_amount_minor,
			       currency, allocation_type, status, created_at, updated_at
			FROM financial_rules
			WHERE status = $1
			ORDER BY created_at DESC
		`
		args = append(args, statusFilter)
	} else {
		query = `
			SELECT id, name, rule_type, percentage, fixed_amount_minor,
			       currency, allocation_type, status, created_at, updated_at
			FROM financial_rules
			ORDER BY created_at DESC
		`
	}

	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list financial rules query: %w", err)
	}
	defer rows.Close()

	var rules []FinancialRule
	for rows.Next() {
		var rule FinancialRule
		var ruleType, allocType, status string
		err := rows.Scan(
			&rule.ID, &rule.Name, &ruleType, &rule.Percentage, &rule.FixedAmountMinor,
			&rule.Currency, &allocType, &status, &rule.CreatedAt, &rule.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan financial rule: %w", err)
		}
		rule.RuleType = RuleType(ruleType)
		rule.AllocationType = AllocationType(allocType)
		rule.Status = RuleStatus(status)
		rules = append(rules, rule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate financial rules: %w", err)
	}

	return rules, nil
}

func (r Repository) GetActiveFinancialRulesByCurrency(ctx context.Context, exec DBExecutor, currency string) ([]FinancialRule, error) {
	db := r.getExec(exec)
	currency = strings.ToUpper(strings.TrimSpace(currency))

	rows, err := db.Query(ctx, `
		SELECT id, name, rule_type, percentage, fixed_amount_minor,
		       currency, allocation_type, status, created_at, updated_at
		FROM financial_rules
		WHERE status = 'ACTIVE' AND UPPER(currency) = $1
		ORDER BY created_at ASC
	`, currency)
	if err != nil {
		return nil, fmt.Errorf("query active financial rules: %w", err)
	}
	defer rows.Close()

	var rules []FinancialRule
	for rows.Next() {
		var rule FinancialRule
		var ruleType, allocType, status string
		err := rows.Scan(
			&rule.ID, &rule.Name, &ruleType, &rule.Percentage, &rule.FixedAmountMinor,
			&rule.Currency, &allocType, &status, &rule.CreatedAt, &rule.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan active financial rule: %w", err)
		}
		rule.RuleType = RuleType(ruleType)
		rule.AllocationType = AllocationType(allocType)
		rule.Status = RuleStatus(status)
		rules = append(rules, rule)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active financial rules: %w", err)
	}

	return rules, nil
}

func (r Repository) SaveSettlementAllocations(ctx context.Context, exec DBExecutor, allocations []SettlementAllocation) error {
	db := r.getExec(exec)

	for _, alloc := range allocations {
		_, err := db.Exec(ctx, `
			INSERT INTO settlement_allocations (
				id, settlement_id, account_id, allocation_type,
				amount_minor, currency, created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7
			)
			ON CONFLICT (settlement_id, allocation_type) DO UPDATE SET
				amount_minor = EXCLUDED.amount_minor,
				account_id = EXCLUDED.account_id,
				currency = EXCLUDED.currency
		`, alloc.ID, alloc.SettlementID, alloc.AccountID, string(alloc.AllocationType),
			alloc.AmountMinor, alloc.Currency, alloc.CreatedAt)

		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				if pgErr.Code == "P0001" || strings.Contains(pgErr.Message, "immutable") {
					return ErrSettlementFinalized
				}
				if pgErr.Code == "23505" {
					return ErrDuplicateAllocation
				}
			}
			return fmt.Errorf("save settlement allocation: %w", err)
		}
	}

	return nil
}

func (r Repository) ListSettlementAllocations(ctx context.Context, exec DBExecutor, settlementID string) ([]SettlementAllocation, error) {
	db := r.getExec(exec)

	rows, err := db.Query(ctx, `
		SELECT id, settlement_id, account_id, allocation_type, amount_minor, currency, created_at
		FROM settlement_allocations
		WHERE settlement_id = $1
		ORDER BY created_at ASC
	`, settlementID)
	if err != nil {
		return nil, fmt.Errorf("list settlement allocations query: %w", err)
	}
	defer rows.Close()

	var allocations []SettlementAllocation
	for rows.Next() {
		var alloc SettlementAllocation
		var allocType string
		err := rows.Scan(
			&alloc.ID, &alloc.SettlementID, &alloc.AccountID, &allocType,
			&alloc.AmountMinor, &alloc.Currency, &alloc.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan settlement allocation: %w", err)
		}
		alloc.AllocationType = AllocationType(allocType)
		allocations = append(allocations, alloc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate settlement allocations: %w", err)
	}

	return allocations, nil
}

func (r Repository) GetSettlementByID(ctx context.Context, exec DBExecutor, id string) (*settlement.Settlement, error) {
	db := r.getExec(exec)

	var s settlement.Settlement
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
