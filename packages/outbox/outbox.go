package outbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/matjeroapps/core/packages/events"
)

var ErrClaimLost = errors.New("outbox claim lost or expired")
var ErrInvalidEnvelope = errors.New("invalid outbox event envelope")

type DBExecutor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Store struct{}

type PendingEvent struct {
	EventID       string
	EventType     string
	SchemaVersion int
	Payload       []byte
	CorrelationID string
	CausationID   string
	OccurredAt    time.Time
}

func NewStore() Store {
	return Store{}
}

func (Store) Enqueue(ctx context.Context, tx pgx.Tx, event events.EventEnvelope) error {
	if err := event.Validate(); err != nil {
		return err
	}

	payload, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("marshal outbox payload: %w", err)
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO outbox_events (
			event_id, aggregate_type, aggregate_id, aggregate_version, event_type,
			schema_version, payload, correlation_id, causation_id, occurred_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, event.EventID, event.AggregateType, event.AggregateID, event.AggregateVersion, event.EventType,
		event.SchemaVersion, payload, event.CorrelationID, event.CausationID, event.OccurredAt)
	if err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}

	return nil
}

func (Store) ClaimBatch(ctx context.Context, db DBExecutor, claimID string, batchSize int, leaseDuration time.Duration) ([]string, error) {
	if claimID == "" {
		return nil, fmt.Errorf("claim id is required")
	}
	if batchSize <= 0 {
		return nil, fmt.Errorf("batch size must be greater than zero")
	}
	if leaseDuration <= 0 {
		return nil, fmt.Errorf("lease duration must be greater than zero")
	}

	leaseInterval := fmt.Sprintf("%d microseconds", leaseDuration.Microseconds())

	rows, err := db.Query(ctx, `
		WITH eligible AS (
			SELECT event_id
			FROM outbox_events
			WHERE published_at IS NULL
			  AND next_attempt_at <= clock_timestamp()
			  AND (
				publish_claim_id IS NULL
				OR publish_claimed_at < (clock_timestamp() - $2::interval)
			  )
			ORDER BY next_attempt_at ASC, created_at ASC, event_id ASC
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		UPDATE outbox_events
		SET publish_claim_id = $1::uuid,
		    publish_claimed_at = clock_timestamp()
		FROM eligible
		WHERE outbox_events.event_id = eligible.event_id
		RETURNING outbox_events.event_id::text;
	`, claimID, leaseInterval, batchSize)
	if err != nil {
		return nil, fmt.Errorf("claim batch outbox events: %w", err)
	}
	defer rows.Close()

	var claimedIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan claimed event id: %w", err)
		}
		claimedIDs = append(claimedIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error claiming outbox events: %w", err)
	}

	return claimedIDs, nil
}

func (Store) RenewAndLoadEvent(ctx context.Context, db DBExecutor, eventID string, claimID string, leaseDuration time.Duration) (*events.EventEnvelope, error) {
	if eventID == "" {
		return nil, fmt.Errorf("event id is required")
	}
	if claimID == "" {
		return nil, fmt.Errorf("claim id is required")
	}
	if leaseDuration <= 0 {
		return nil, fmt.Errorf("lease duration must be greater than zero")
	}

	leaseInterval := fmt.Sprintf("%d microseconds", leaseDuration.Microseconds())

	var (
		aggregateType    string
		aggregateID      string
		aggregateVersion int64
		eventType        string
		schemaVersion    int
		payloadBytes     []byte
		correlationID    *string
		causationID      *string
		occurredAt       time.Time
	)

	err := db.QueryRow(ctx, `
		UPDATE outbox_events
		SET publish_claimed_at = clock_timestamp()
		WHERE event_id = $1::uuid
		  AND publish_claim_id = $2::uuid
		  AND published_at IS NULL
		  AND publish_claimed_at >= (clock_timestamp() - $3::interval)
		RETURNING aggregate_type, aggregate_id, aggregate_version, event_type, schema_version, payload, correlation_id, causation_id, occurred_at;
	`, eventID, claimID, leaseInterval).Scan(
		&aggregateType,
		&aggregateID,
		&aggregateVersion,
		&eventType,
		&schemaVersion,
		&payloadBytes,
		&correlationID,
		&causationID,
		&occurredAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrClaimLost
		}
		return nil, fmt.Errorf("renew and load outbox event: %w", err)
	}

	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: unmarshal payload: %v", ErrInvalidEnvelope, err)
	}

	env := &events.EventEnvelope{
		EventID:          eventID,
		EventType:        eventType,
		SchemaVersion:    schemaVersion,
		AggregateType:    aggregateType,
		AggregateID:      aggregateID,
		AggregateVersion: aggregateVersion,
		OccurredAt:       occurredAt,
		Payload:          payload,
	}
	if correlationID != nil {
		env.CorrelationID = *correlationID
	}
	if causationID != nil {
		env.CausationID = *causationID
	}

	if err := env.Validate(); err != nil {
		return nil, fmt.Errorf("%w: validate envelope: %v", ErrInvalidEnvelope, err)
	}

	return env, nil
}

func (Store) RenewBatchNearExpiry(ctx context.Context, db DBExecutor, eventIDs []string, claimID string, leaseDuration time.Duration, renewalMargin time.Duration) error {
	if len(eventIDs) == 0 {
		return nil
	}
	if claimID == "" {
		return fmt.Errorf("claim id is required")
	}
	if leaseDuration <= 0 {
		return fmt.Errorf("lease duration must be greater than zero")
	}
	if renewalMargin <= 0 {
		return fmt.Errorf("renewal margin must be greater than zero")
	}

	leaseInterval := fmt.Sprintf("%d microseconds", leaseDuration.Microseconds())
	marginInterval := fmt.Sprintf("%d microseconds", renewalMargin.Microseconds())

	_, err := db.Exec(ctx, `
		UPDATE outbox_events
		SET publish_claimed_at = clock_timestamp()
		WHERE event_id = ANY($1::uuid[])
		  AND publish_claim_id = $2::uuid
		  AND published_at IS NULL
		  AND publish_claimed_at >= (clock_timestamp() - $3::interval)
		  AND (publish_claimed_at + $3::interval - clock_timestamp()) <= $4::interval;
	`, eventIDs, claimID, leaseInterval, marginInterval)
	if err != nil {
		return fmt.Errorf("renew batch outbox claims near expiry: %w", err)
	}
	return nil
}

func (Store) MarkPublished(ctx context.Context, db DBExecutor, eventID string, claimID string) (bool, error) {
	if eventID == "" {
		return false, fmt.Errorf("event id is required")
	}
	if claimID == "" {
		return false, fmt.Errorf("claim id is required")
	}

	tag, err := db.Exec(ctx, `
		UPDATE outbox_events
		SET published_at = clock_timestamp(),
		    publish_claim_id = NULL,
		    publish_claimed_at = NULL
		WHERE event_id = $1::uuid
		  AND publish_claim_id = $2::uuid
		  AND published_at IS NULL;
	`, eventID, claimID)
	if err != nil {
		return false, fmt.Errorf("mark outbox event published: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func (Store) ReleaseWithBackoff(ctx context.Context, db DBExecutor, eventID string, claimID string) (bool, error) {
	if eventID == "" {
		return false, fmt.Errorf("event id is required")
	}
	if claimID == "" {
		return false, fmt.Errorf("claim id is required")
	}

	tag, err := db.Exec(ctx, `
		UPDATE outbox_events
		SET publish_attempts = publish_attempts + 1,
		    publish_claim_id = NULL,
		    publish_claimed_at = NULL,
		    next_attempt_at = clock_timestamp() + (interval '1 second' * power(2, LEAST(publish_attempts, 6)))
		WHERE event_id = $1::uuid
		  AND publish_claim_id = $2::uuid
		  AND published_at IS NULL;
	`, eventID, claimID)
	if err != nil {
		return false, fmt.Errorf("release outbox event with backoff: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

func ResolveRoutingKey(eventType string) (exchange string, routingKey string, err error) {
	switch eventType {
	case "commerce.order.created.v1":
		return "commerce.events", "order.created", nil
	case "commerce.order.status_changed.v1":
		return "commerce.events", "order.status_changed", nil
	case "shipping.shipment.created.v1":
		return "shipping.events", "shipment.created", nil
	case "shipping.shipment.status_changed.v1":
		return "shipping.events", "shipment.status_changed", nil
	case "payments.payment.captured.v1":
		return "payments.events", "payment.captured", nil
	case "payments.payment.failed.v1":
		return "payments.events", "payment.failed", nil
	case "ledger.journal_entry.posted.v1":
		return "ledger.events", "journal_entry.posted", nil
	default:
		return "", "", fmt.Errorf("unknown routing for event_type: %s", eventType)
	}
}
