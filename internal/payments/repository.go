package payments

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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

func (r Repository) withTx(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
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

func (r Repository) CreatePaymentTx(ctx context.Context, tx pgx.Tx, payment Payment, attempt *PaymentAttempt) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO payments (
			id, order_id, amount_minor, currency, payment_method, status, created_at, updated_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, payment.ID, payment.OrderID, payment.AmountMinor, payment.Currency, payment.PaymentMethod, string(payment.Status), payment.CreatedAt, payment.UpdatedAt)
	if err != nil {
		return fmt.Errorf("insert payment: %w", err)
	}

	if attempt != nil {
		var providerRef *string
		if strings.TrimSpace(attempt.ProviderReference) != "" {
			ref := strings.TrimSpace(attempt.ProviderReference)
			providerRef = &ref
		}
		var errMsg *string
		if strings.TrimSpace(attempt.ErrorMessage) != "" {
			em := strings.TrimSpace(attempt.ErrorMessage)
			errMsg = &em
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO payment_attempts (
				id, payment_id, provider, provider_reference, status, error_message, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, attempt.ID, attempt.PaymentID, attempt.Provider, providerRef, attempt.Status, errMsg, attempt.CreatedAt, attempt.UpdatedAt)
		if err != nil {
			return fmt.Errorf("insert payment attempt: %w", err)
		}
	}

	return nil
}

func (r Repository) UpdatePaymentStatusTx(
	ctx context.Context,
	tx pgx.Tx,
	paymentID string,
	newStatus Status,
	attempt *PaymentAttempt,
	now time.Time,
) (*Payment, Status, error) {
	var (
		currentPayment Payment
		currentStatus  string
	)

	err := tx.QueryRow(ctx, `
		SELECT id, order_id, amount_minor, currency, payment_method, status, created_at, updated_at
		FROM payments
		WHERE id = $1
		FOR UPDATE
	`, paymentID).Scan(
		&currentPayment.ID,
		&currentPayment.OrderID,
		&currentPayment.AmountMinor,
		&currentPayment.Currency,
		&currentPayment.PaymentMethod,
		&currentStatus,
		&currentPayment.CreatedAt,
		&currentPayment.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrPaymentNotFound
		}
		return nil, "", fmt.Errorf("lock payment for update: %w", err)
	}

	fromStatus := Status(currentStatus)
	if err := ValidateTransition(fromStatus, newStatus); err != nil {
		return nil, fromStatus, err
	}

	_, err = tx.Exec(ctx, `
		UPDATE payments
		SET status = $1, updated_at = $2
		WHERE id = $3
	`, string(newStatus), now, paymentID)
	if err != nil {
		return nil, fromStatus, fmt.Errorf("update payment status: %w", err)
	}

	if attempt != nil {
		var providerRef *string
		if strings.TrimSpace(attempt.ProviderReference) != "" {
			ref := strings.TrimSpace(attempt.ProviderReference)
			providerRef = &ref
		}
		var errMsg *string
		if strings.TrimSpace(attempt.ErrorMessage) != "" {
			em := strings.TrimSpace(attempt.ErrorMessage)
			errMsg = &em
		}

		_, err := tx.Exec(ctx, `
			INSERT INTO payment_attempts (
				id, payment_id, provider, provider_reference, status, error_message, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`, attempt.ID, attempt.PaymentID, attempt.Provider, providerRef, attempt.Status, errMsg, attempt.CreatedAt, attempt.UpdatedAt)
		if err != nil {
			return nil, fromStatus, fmt.Errorf("insert payment attempt: %w", err)
		}
	}

	currentPayment.Status = newStatus
	currentPayment.UpdatedAt = now
	return &currentPayment, fromStatus, nil
}

func (r Repository) GetPaymentByID(ctx context.Context, exec DBExecutor, id string) (*Payment, error) {
	db := r.getExec(exec)
	var payment Payment
	var statusStr string

	err := db.QueryRow(ctx, `
		SELECT id, order_id, amount_minor, currency, payment_method, status, created_at, updated_at
		FROM payments
		WHERE id = $1
	`, id).Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.AmountMinor,
		&payment.Currency,
		&payment.PaymentMethod,
		&statusStr,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, fmt.Errorf("get payment by id: %w", err)
	}
	payment.Status = Status(statusStr)

	attempts, err := r.ListPaymentAttempts(ctx, db, id)
	if err != nil {
		return nil, err
	}
	payment.Attempts = attempts

	return &payment, nil
}

func (r Repository) GetPaymentByOrderID(ctx context.Context, exec DBExecutor, orderID string) (*Payment, error) {
	db := r.getExec(exec)
	var payment Payment
	var statusStr string

	err := db.QueryRow(ctx, `
		SELECT id, order_id, amount_minor, currency, payment_method, status, created_at, updated_at
		FROM payments
		WHERE order_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`, orderID).Scan(
		&payment.ID,
		&payment.OrderID,
		&payment.AmountMinor,
		&payment.Currency,
		&payment.PaymentMethod,
		&statusStr,
		&payment.CreatedAt,
		&payment.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, fmt.Errorf("get payment by order id: %w", err)
	}
	payment.Status = Status(statusStr)

	attempts, err := r.ListPaymentAttempts(ctx, db, payment.ID)
	if err != nil {
		return nil, err
	}
	payment.Attempts = attempts

	return &payment, nil
}

func (r Repository) ListPaymentAttempts(ctx context.Context, exec DBExecutor, paymentID string) ([]PaymentAttempt, error) {
	db := r.getExec(exec)
	rows, err := db.Query(ctx, `
		SELECT id, payment_id, provider, provider_reference, status, error_message, created_at, updated_at
		FROM payment_attempts
		WHERE payment_id = $1
		ORDER BY created_at ASC
	`, paymentID)
	if err != nil {
		return nil, fmt.Errorf("query payment attempts: %w", err)
	}
	defer rows.Close()

	var attempts []PaymentAttempt
	for rows.Next() {
		var a PaymentAttempt
		var providerRef, errMsg sql.NullString
		if err := rows.Scan(
			&a.ID,
			&a.PaymentID,
			&a.Provider,
			&providerRef,
			&a.Status,
			&errMsg,
			&a.CreatedAt,
			&a.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan payment attempt: %w", err)
		}
		if providerRef.Valid {
			a.ProviderReference = providerRef.String
		}
		if errMsg.Valid {
			a.ErrorMessage = errMsg.String
		}
		attempts = append(attempts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error listing payment attempts: %w", err)
	}
	return attempts, nil
}

func (r Repository) PersistWebhookInbox(ctx context.Context, exec DBExecutor, inbox WebhookInbox) (*WebhookInbox, bool, error) {
	db := r.getExec(exec)

	var connID *string
	if strings.TrimSpace(inbox.ConnectionID) != "" {
		c := strings.TrimSpace(inbox.ConnectionID)
		connID = &c
	}

	tag, err := db.Exec(ctx, `
		INSERT INTO webhook_inbox (
			id, provider, connection_id, provider_event_id, event_type, payload_json,
			signature_verified, status, attempt_count, received_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (provider, provider_event_id) DO NOTHING
	`, inbox.ID, inbox.Provider, connID, inbox.ProviderEventID, inbox.EventType, inbox.PayloadJSON,
		inbox.SignatureVerified, inbox.Status, inbox.AttemptCount, inbox.ReceivedAt)
	if err != nil {
		return nil, false, fmt.Errorf("insert webhook inbox: %w", err)
	}

	if tag.RowsAffected() == 0 {
		// Duplicate event! Fetch existing record from webhook_inbox
		var existing WebhookInbox
		var cid sql.NullString
		var procAt sql.NullTime

		err := db.QueryRow(ctx, `
			SELECT id, provider, connection_id, provider_event_id, event_type, payload_json,
			       signature_verified, status, attempt_count, received_at, processed_at
			FROM webhook_inbox
			WHERE provider = $1 AND provider_event_id = $2
		`, inbox.Provider, inbox.ProviderEventID).Scan(
			&existing.ID,
			&existing.Provider,
			&cid,
			&existing.ProviderEventID,
			&existing.EventType,
			&existing.PayloadJSON,
			&existing.SignatureVerified,
			&existing.Status,
			&existing.AttemptCount,
			&existing.ReceivedAt,
			&procAt,
		)
		if err != nil {
			return nil, false, fmt.Errorf("fetch duplicate webhook inbox entry: %w", err)
		}
		if cid.Valid {
			existing.ConnectionID = cid.String
		}
		if procAt.Valid {
			existing.ProcessedAt = &procAt.Time
		}
		return &existing, true, nil
	}

	return &inbox, false, nil
}
