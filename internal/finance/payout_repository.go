package finance

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type StorePayout struct {
	ID               string     `json:"id"`
	StoreID          string     `json:"store_id"`
	AccountID        string     `json:"account_id"`
	AmountMinorUnits int64      `json:"amount_minor_units"`
	Currency         string     `json:"currency"`
	Status           string     `json:"status"`
	DestinationBank  string     `json:"destination_bank"`
	ReferenceNumber  *string    `json:"reference_number,omitempty"`
	FailureReason    *string    `json:"failure_reason,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	ProcessedAt      *time.Time `json:"processed_at,omitempty"`
}

func (r Repository) ListStorePayouts(ctx context.Context, storeID string, page, pageSize int) ([]StorePayout, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	var totalCount int
	countQuery := `SELECT COUNT(*) FROM store_payouts WHERE store_id = $1`
	if err := r.pool.QueryRow(ctx, countQuery, storeID).Scan(&totalCount); err != nil {
		return nil, 0, fmt.Errorf("count store payouts: %w", err)
	}

	query := `
		SELECT 
			id, store_id, account_id, amount_minor_units, currency,
			status, destination_bank, reference_number, failure_reason,
			created_at, processed_at
		FROM store_payouts
		WHERE store_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`
	rows, err := r.pool.Query(ctx, query, storeID, pageSize, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("query store payouts: %w", err)
	}
	defer rows.Close()

	var payouts []StorePayout
	for rows.Next() {
		var p StorePayout
		var ref, failure sql.NullString
		var processed sql.NullTime
		if err := rows.Scan(
			&p.ID, &p.StoreID, &p.AccountID, &p.AmountMinorUnits, &p.Currency,
			&p.Status, &p.DestinationBank, &ref, &failure,
			&p.CreatedAt, &processed,
		); err != nil {
			return nil, 0, fmt.Errorf("scan store payout: %w", err)
		}
		if ref.Valid {
			p.ReferenceNumber = &ref.String
		}
		if failure.Valid {
			p.FailureReason = &failure.String
		}
		if processed.Valid {
			p.ProcessedAt = &processed.Time
		}
		payouts = append(payouts, p)
	}

	if payouts == nil {
		payouts = []StorePayout{}
	}
	return payouts, totalCount, nil
}

func (r Repository) CreateStorePayout(ctx context.Context, p StorePayout) (*StorePayout, error) {
	if p.ID == "" {
		p.ID = uuid.NewString()
	}
	if p.Status == "" {
		p.Status = "REQUESTED"
	}
	query := `
		INSERT INTO store_payouts (
			id, store_id, account_id, amount_minor_units, currency,
			status, destination_bank, reference_number, failure_reason,
			created_at, processed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, now(), $10)
		RETURNING created_at
	`
	err := r.pool.QueryRow(
		ctx, query,
		p.ID, p.StoreID, p.AccountID, p.AmountMinorUnits, p.Currency,
		p.Status, p.DestinationBank, p.ReferenceNumber, p.FailureReason, p.ProcessedAt,
	).Scan(&p.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert store payout: %w", err)
	}
	return &p, nil
}

func (r Repository) GetStorePayout(ctx context.Context, storeID, payoutID string) (*StorePayout, error) {
	query := `
		SELECT 
			id, store_id, account_id, amount_minor_units, currency,
			status, destination_bank, reference_number, failure_reason,
			created_at, processed_at
		FROM store_payouts
		WHERE store_id = $1 AND id = $2
	`
	var p StorePayout
	var ref, failure sql.NullString
	var processed sql.NullTime
	err := r.pool.QueryRow(ctx, query, storeID, payoutID).Scan(
		&p.ID, &p.StoreID, &p.AccountID, &p.AmountMinorUnits, &p.Currency,
		&p.Status, &p.DestinationBank, &ref, &failure,
		&p.CreatedAt, &processed,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("payout not found")
		}
		return nil, fmt.Errorf("get store payout: %w", err)
	}
	if ref.Valid {
		p.ReferenceNumber = &ref.String
	}
	if failure.Valid {
		p.FailureReason = &failure.String
	}
	if processed.Valid {
		p.ProcessedAt = &processed.Time
	}
	return &p, nil
}
