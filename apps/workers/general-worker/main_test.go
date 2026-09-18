package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	amqp "github.com/rabbitmq/amqp091-go"

	"core/packages/config"
	"core/packages/events"
	"core/packages/messaging"
)

type emptyRows struct{}

func (emptyRows) Close()                                       {}
func (emptyRows) Err() error                                   { return nil }
func (emptyRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (emptyRows) Next() bool                                   { return false }
func (emptyRows) Scan(dest ...any) error                       { return pgx.ErrNoRows }
func (emptyRows) Values() ([]any, error)                       { return nil, nil }
func (emptyRows) RawValues() [][]byte                          { return nil }
func (emptyRows) Conn() *pgx.Conn                              { return nil }

type emptyRow struct{}

func (emptyRow) Scan(dest ...any) error { return pgx.ErrNoRows }

type dummyDBExecutor struct{}

func (dummyDBExecutor) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 0"), nil
}

func (dummyDBExecutor) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return emptyRows{}, nil
}

func (dummyDBExecutor) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return emptyRow{}
}

type fakeWorkerPublisher struct {
	failTransport bool
}

func (f *fakeWorkerPublisher) PublishEvent(ctx context.Context, exchange, routingKey string, event events.EventEnvelope) error {
	if f.failTransport {
		return messaging.ErrPublisherUnavailable
	}
	return nil
}

func (f *fakeWorkerPublisher) PublishMessage(ctx context.Context, exchange, routingKey string, message events.MessageEnvelope) error {
	if f.failTransport {
		return messaging.ErrPublisherUnavailable
	}
	return nil
}

func TestWorkerInitialRabbitSetupFailureRetries(t *testing.T) {
	cfg := config.Config{
		OutboxPollInterval: 10 * time.Millisecond,
		OutboxBatchSize:    10,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := dummyDBExecutor{}

	var setupAttempts atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	mockSetup := func(rabbitURL string) (*amqp.Connection, *amqp.Channel, messaging.Publisher, error) {
		attempt := setupAttempts.Add(1)
		if attempt == 1 {
			return nil, nil, nil, errors.New("rabbit connection failed")
		}
		return nil, nil, &fakeWorkerPublisher{}, nil
	}

	err := runWorkerLoop(ctx, cfg, db, logger, mockSetup, 5*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("expected nil error on context cancellation, got %v", err)
	}

	if setupAttempts.Load() < 2 {
		t.Errorf("expected setup function to be retried at least twice, got %d", setupAttempts.Load())
	}
}

type singleClaimRows struct {
	done bool
	id   string
}

func (r *singleClaimRows) Close()                                       {}
func (r *singleClaimRows) Err() error                                   { return nil }
func (r *singleClaimRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *singleClaimRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *singleClaimRows) Next() bool {
	if r.done {
		return false
	}
	r.done = true
	return true
}
func (r *singleClaimRows) Scan(dest ...any) error {
	if len(dest) > 0 {
		if ptr, ok := dest[0].(*string); ok {
			*ptr = r.id
		}
	}
	return nil
}
func (r *singleClaimRows) Values() ([]any, error) { return nil, nil }
func (r *singleClaimRows) RawValues() [][]byte    { return nil }
func (r *singleClaimRows) Conn() *pgx.Conn        { return nil }

type singleEnvelopeRow struct{}

func (singleEnvelopeRow) Scan(dest ...any) error {
	if len(dest) >= 9 {
		*dest[0].(*string) = "order"
		*dest[1].(*string) = "ord_1"
		*dest[2].(*int64) = 1
		*dest[3].(*string) = "commerce.order.created.v1"
		*dest[4].(*int) = 1
		*dest[5].(*[]byte) = []byte(`{}`)
		*dest[6].(**string) = nil
		*dest[7].(**string) = nil
		*dest[8].(*time.Time) = time.Now()
	}
	return nil
}

type mockClaimDBExecutor struct{}

func (mockClaimDBExecutor) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (mockClaimDBExecutor) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return &singleClaimRows{id: "44444444-4444-4444-4444-444444444444"}, nil
}

func (mockClaimDBExecutor) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return singleEnvelopeRow{}
}

func TestWorkerRuntimeTransportFailureReconnects(t *testing.T) {
	cfg := config.Config{
		OutboxPollInterval:            10 * time.Millisecond,
		OutboxBatchSize:               10,
		OutboxClaimLeaseDuration:      30 * time.Second,
		OutboxClaimRenewalMargin:      10 * time.Second,
		RabbitMQPublishConfirmTimeout: 5 * time.Second,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := mockClaimDBExecutor{}

	var setupAttempts atomic.Int32
	var requestedWaits []time.Duration
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mockSetup := func(rabbitURL string) (*amqp.Connection, *amqp.Channel, messaging.Publisher, error) {
		attempts := setupAttempts.Add(1)
		if attempts >= 3 {
			cancel()
		}
		return nil, nil, &fakeWorkerPublisher{failTransport: true}, nil
	}

	mockWait := func(c context.Context, d time.Duration) error {
		requestedWaits = append(requestedWaits, d)
		return c.Err()
	}

	err := runWorkerLoop(ctx, cfg, db, logger, mockSetup, 5*time.Millisecond, mockWait)
	if err != nil {
		t.Fatalf("expected clean exit on context cancel, got: %v", err)
	}

	if setupAttempts.Load() < 2 {
		t.Errorf("expected reconnect setup to be invoked at least twice after transport failure, got %d", setupAttempts.Load())
	}
	if len(requestedWaits) == 0 {
		t.Errorf("expected runtime reconnect to request non-zero delay")
	} else if requestedWaits[0] != 5*time.Millisecond {
		t.Errorf("expected first reconnect delay = 5ms, got %v", requestedWaits[0])
	}
}

func TestWorkerGracefulShutdownOnContextCancel(t *testing.T) {
	cfg := config.Config{
		OutboxPollInterval: 10 * time.Millisecond,
		OutboxBatchSize:    10,
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db := dummyDBExecutor{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	mockSetup := func(rabbitURL string) (*amqp.Connection, *amqp.Channel, messaging.Publisher, error) {
		return nil, nil, &fakeWorkerPublisher{}, nil
	}

	err := runWorkerLoop(ctx, cfg, db, logger, mockSetup, 5*time.Millisecond, nil)
	if err != nil {
		t.Fatalf("expected clean worker exit on immediate context cancel, got: %v", err)
	}
}
