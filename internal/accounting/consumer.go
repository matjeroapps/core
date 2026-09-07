package accounting

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/packages/events"
	"github.com/matjeroapps/core/packages/inbox"
)

const ConsumerName = "accounting_payment_consumer"

type DBPool interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type InboxStore interface {
	RecordProcessed(ctx context.Context, tx pgx.Tx, consumerName, eventID string) (bool, error)
}

type Consumer interface {
	HandleEvent(ctx context.Context, envelope events.EventEnvelope) error
}

type paymentEventConsumer struct {
	pool       DBPool
	financeSvc finance.Service
	registry   AccountRegistry
	ruleEngine RuleEngine
	inbox      InboxStore
}

func NewConsumer(pool DBPool, financeSvc finance.Service) Consumer {
	return &paymentEventConsumer{
		pool:       pool,
		financeSvc: financeSvc,
		registry:   NewAccountRegistry(financeSvc),
		ruleEngine: NewRuleEngine(),
		inbox:      inbox.NewStore(),
	}
}

func NewConsumerWithDependencies(pool DBPool, financeSvc finance.Service, registry AccountRegistry, ruleEngine RuleEngine, inboxStore InboxStore) Consumer {
	return &paymentEventConsumer{
		pool:       pool,
		financeSvc: financeSvc,
		registry:   registry,
		ruleEngine: ruleEngine,
		inbox:      inboxStore,
	}
}

func (c *paymentEventConsumer) HandleEvent(ctx context.Context, envelope events.EventEnvelope) error {
	if err := envelope.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEventPayload, err)
	}

	if envelope.Payload == nil {
		return ErrNilEventPayload
	}

	rule, err := c.ruleEngine.ResolveRule(envelope.EventType, envelope.Payload)
	if err != nil {
		return err
	}

	if c.pool == nil {
		return errors.New("database pool is not initialized")
	}

	tx, err := c.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Record event in inbox store for consumer deduplication
	inserted, err := c.inbox.RecordProcessed(ctx, tx, ConsumerName, envelope.EventID)
	if err != nil {
		return fmt.Errorf("record processed event in inbox: %w", err)
	}
	if !inserted {
		// Event was already processed by this consumer - idempotent return
		return nil
	}

	if !rule.ShouldPost {
		// No financial entry required for this event (e.g. failed payment)
		return tx.Commit(ctx)
	}

	// Resolve debit and credit accounts
	debitAcc, err := c.registry.GetAccountByRole(ctx, rule.DebitAccountRole, rule.Currency)
	if err != nil {
		return fmt.Errorf("resolve debit account (%s): %w", rule.DebitAccountRole, err)
	}

	creditAcc, err := c.registry.GetAccountByRole(ctx, rule.CreditAccountRole, rule.Currency)
	if err != nil {
		return fmt.Errorf("resolve credit account (%s): %w", rule.CreditAccountRole, err)
	}

	params := finance.PostJournalEntryParams{
		ReferenceType: rule.ReferenceType,
		ReferenceID:   rule.ReferenceID,
		Description:   rule.Description,
		Currency:      rule.Currency,
		Lines: []finance.CreateJournalLineParams{
			{
				AccountID:         debitAcc.ID,
				DebitAmountMinor:  rule.AmountMinor,
				CreditAmountMinor: 0,
			},
			{
				AccountID:         creditAcc.ID,
				DebitAmountMinor:  0,
				CreditAmountMinor: rule.AmountMinor,
			},
		},
		CorrelationID: envelope.CorrelationID,
		CausationID:   envelope.EventID,
	}

	// Check if a journal entry already exists for this reference_type and reference_id (idempotency protection)
	existingEntry, err := c.financeSvc.GetJournalEntryByReferenceTx(ctx, tx, rule.ReferenceType, rule.ReferenceID)
	if err == nil && existingEntry != nil {
		// Entry already posted for this payment -> idempotent return
		return tx.Commit(ctx)
	} else if err != nil && !errors.Is(err, finance.ErrJournalEntryNotFound) {
		return fmt.Errorf("check existing journal entry by reference: %w", err)
	}

	_, err = c.financeSvc.PostJournalEntryTx(ctx, tx, params)
	if err != nil {
		if errors.Is(err, finance.ErrDuplicatePosting) {
			// Idempotent success: entry was already posted for this reference_type and reference_id
			return tx.Commit(ctx)
		}
		return fmt.Errorf("post journal entry: %w", err)
	}

	return tx.Commit(ctx)
}
