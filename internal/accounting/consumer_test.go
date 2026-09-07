package accounting_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/matjeroapps/core/internal/accounting"
	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/packages/events"
)

type mockInboxStore struct {
	processed map[string]bool
}

func newMockInboxStore() *mockInboxStore {
	return &mockInboxStore{processed: make(map[string]bool)}
}

func (m *mockInboxStore) RecordProcessed(ctx context.Context, tx pgx.Tx, consumerName, eventID string) (bool, error) {
	key := consumerName + ":" + eventID
	if m.processed[key] {
		return false, nil
	}
	m.processed[key] = true
	return true, nil
}

type mockDBPool struct{}

func (m *mockDBPool) Begin(ctx context.Context) (pgx.Tx, error) {
	return &mockTx{}, nil
}

type mockTx struct {
	pgx.Tx
}

func (m *mockTx) Commit(ctx context.Context) error {
	return nil
}

func (m *mockTx) Rollback(ctx context.Context) error {
	return nil
}

type consumerMockFinanceSvc struct {
	*mockFinanceService
	postedEntries []*finance.PostJournalEntryParams
	entriesByRef  map[string]*finance.JournalEntry
	failPosting   error
}

func newConsumerMockFinanceSvc() *consumerMockFinanceSvc {
	return &consumerMockFinanceSvc{
		mockFinanceService: newMockFinanceService(),
		entriesByRef:       make(map[string]*finance.JournalEntry),
	}
}

func (m *consumerMockFinanceSvc) PostJournalEntryTx(ctx context.Context, tx pgx.Tx, params finance.PostJournalEntryParams) (*finance.JournalEntry, error) {
	if m.failPosting != nil {
		return nil, m.failPosting
	}
	m.postedEntries = append(m.postedEntries, &params)
	entry := &finance.JournalEntry{
		ID:            uuid.NewString(),
		ReferenceType: params.ReferenceType,
		ReferenceID:   params.ReferenceID,
		Description:   params.Description,
		Currency:      params.Currency,
	}
	m.entriesByRef[params.ReferenceType+":"+params.ReferenceID] = entry
	return entry, nil
}

func (m *consumerMockFinanceSvc) GetJournalEntryByReference(ctx context.Context, refType, refID string) (*finance.JournalEntry, error) {
	key := refType + ":" + refID
	if entry, ok := m.entriesByRef[key]; ok {
		return entry, nil
	}
	return nil, finance.ErrJournalEntryNotFound
}

func (m *consumerMockFinanceSvc) GetJournalEntryByReferenceTx(ctx context.Context, tx pgx.Tx, refType, refID string) (*finance.JournalEntry, error) {
	key := refType + ":" + refID
	if entry, ok := m.entriesByRef[key]; ok {
		return entry, nil
	}
	return nil, finance.ErrJournalEntryNotFound
}

func TestConsumerHandleEvent(t *testing.T) {
	ctx := context.Background()

	t.Run("Handle Payment Captured Event Success", func(t *testing.T) {
		finSvc := newConsumerMockFinanceSvc()
		inbox := newMockInboxStore()
		pool := &mockDBPool{}

		consumer := accounting.NewConsumerWithDependencies(pool, finSvc, accounting.NewAccountRegistry(finSvc), accounting.NewRuleEngine(), inbox)

		payload := events.PaymentCapturedPayload{
			PaymentID:     "pay_100",
			OrderID:       "ord_100",
			AmountMinor:   25000,
			Currency:      "SAR",
			PaymentMethod: "CREDIT_CARD",
			Provider:      "CHECKOUT",
			CapturedAt:    time.Now().UTC(),
		}

		envelope, err := events.NewPaymentCapturedEvent(payload, "corr_100", "caus_100")
		if err != nil {
			t.Fatalf("create payment captured event: %v", err)
		}

		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("unexpected error handling payment captured event: %v", err)
		}

		if len(finSvc.postedEntries) != 1 {
			t.Fatalf("expected 1 posted journal entry, got %d", len(finSvc.postedEntries))
		}

		posted := finSvc.postedEntries[0]
		if posted.ReferenceType != "PAYMENT_CAPTURED" || posted.ReferenceID != "pay_100" {
			t.Errorf("unexpected reference: %s / %s", posted.ReferenceType, posted.ReferenceID)
		}
		if posted.Currency != "SAR" {
			t.Errorf("expected currency SAR, got %s", posted.Currency)
		}
		if len(posted.Lines) != 2 {
			t.Fatalf("expected 2 lines, got %d", len(posted.Lines))
		}
		if posted.Lines[0].DebitAmountMinor != 25000 || posted.Lines[1].CreditAmountMinor != 25000 {
			t.Errorf("unexpected line amounts: debit %d, credit %d", posted.Lines[0].DebitAmountMinor, posted.Lines[1].CreditAmountMinor)
		}
		if posted.CausationID != envelope.EventID {
			t.Errorf("expected CausationID to be event ID %s, got %s", envelope.EventID, posted.CausationID)
		}
	})

	t.Run("Duplicate Inbox Event Skipped Idempotently", func(t *testing.T) {
		finSvc := newConsumerMockFinanceSvc()
		inbox := newMockInboxStore()
		pool := &mockDBPool{}

		consumer := accounting.NewConsumerWithDependencies(pool, finSvc, accounting.NewAccountRegistry(finSvc), accounting.NewRuleEngine(), inbox)

		payload := events.PaymentCapturedPayload{
			PaymentID:     "pay_100",
			OrderID:       "ord_100",
			AmountMinor:   25000,
			Currency:      "SAR",
			PaymentMethod: "CREDIT_CARD",
			CapturedAt:    time.Now().UTC(),
		}

		envelope, err := events.NewPaymentCapturedEvent(payload, "corr_100", "caus_100")
		if err != nil {
			t.Fatalf("create payment captured event: %v", err)
		}

		// First execution
		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("first handle event failed: %v", err)
		}
		if len(finSvc.postedEntries) != 1 {
			t.Fatalf("expected 1 posted entry on first call, got %d", len(finSvc.postedEntries))
		}

		// Second execution with exact same event envelope
		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("second handle event failed: %v", err)
		}
		if len(finSvc.postedEntries) != 1 {
			t.Fatalf("expected still 1 posted entry after duplicate event, got %d", len(finSvc.postedEntries))
		}
	})

	t.Run("Duplicate Ledger Posting Error Treated as Idempotent Success", func(t *testing.T) {
		finSvc := newConsumerMockFinanceSvc()
		finSvc.failPosting = finance.ErrDuplicatePosting
		inbox := newMockInboxStore()
		pool := &mockDBPool{}

		consumer := accounting.NewConsumerWithDependencies(pool, finSvc, accounting.NewAccountRegistry(finSvc), accounting.NewRuleEngine(), inbox)

		payload := events.PaymentCapturedPayload{
			PaymentID:     "pay_200",
			OrderID:       "ord_200",
			AmountMinor:   10000,
			Currency:      "SAR",
			PaymentMethod: "MADA",
			CapturedAt:    time.Now().UTC(),
		}

		envelope, err := events.NewPaymentCapturedEvent(payload, "corr_200", "caus_200")
		if err != nil {
			t.Fatalf("create payment captured event: %v", err)
		}

		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("expected nil error for duplicate posting error (idempotent success), got: %v", err)
		}
	})

	t.Run("Handle Payment Failed Event No Posting", func(t *testing.T) {
		finSvc := newConsumerMockFinanceSvc()
		inbox := newMockInboxStore()
		pool := &mockDBPool{}

		consumer := accounting.NewConsumerWithDependencies(pool, finSvc, accounting.NewAccountRegistry(finSvc), accounting.NewRuleEngine(), inbox)

		payload := events.PaymentFailedPayload{
			PaymentID:     "pay_300",
			OrderID:       "ord_300",
			AmountMinor:   10000,
			Currency:      "SAR",
			PaymentMethod: "CREDIT_CARD",
			ErrorMessage:  "Declined by bank",
			FailedAt:      time.Now().UTC(),
		}

		envelope, err := events.NewPaymentFailedEvent(payload, "corr_300", "caus_300")
		if err != nil {
			t.Fatalf("create payment failed event: %v", err)
		}

		err = consumer.HandleEvent(ctx, envelope)
		if err != nil {
			t.Fatalf("unexpected error handling payment failed event: %v", err)
		}

		if len(finSvc.postedEntries) != 0 {
			t.Errorf("expected 0 posted journal entries for failed payment, got %d", len(finSvc.postedEntries))
		}
	})

	t.Run("Invalid Event Envelope Fails Validation", func(t *testing.T) {
		finSvc := newConsumerMockFinanceSvc()
		inbox := newMockInboxStore()
		pool := &mockDBPool{}

		consumer := accounting.NewConsumerWithDependencies(pool, finSvc, accounting.NewAccountRegistry(finSvc), accounting.NewRuleEngine(), inbox)

		invalidEnv := events.EventEnvelope{} // empty event envelope
		err := consumer.HandleEvent(ctx, invalidEnv)
		if err == nil {
			t.Fatalf("expected error for invalid event envelope, got nil")
		}
		if !errors.Is(err, accounting.ErrInvalidEventPayload) {
			t.Errorf("expected ErrInvalidEventPayload, got %v", err)
		}
	})
}
