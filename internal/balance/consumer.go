package balance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/matjeroapps/core/packages/events"
	"github.com/matjeroapps/core/packages/inbox"
)

const ConsumerName = "balance_projection_consumer"

type InboxStore interface {
	RecordProcessed(ctx context.Context, tx pgx.Tx, consumerName, eventID string) (bool, error)
}

type Consumer interface {
	HandleEvent(ctx context.Context, envelope events.EventEnvelope) error
}

type journalEntryPostedConsumer struct {
	pool  DBPool
	repo  Repository
	inbox InboxStore
}

func NewConsumer(pool DBPool, repo Repository) Consumer {
	return &journalEntryPostedConsumer{
		pool:  pool,
		repo:  repo,
		inbox: inbox.NewStore(),
	}
}

func NewConsumerWithDependencies(pool DBPool, repo Repository, inboxStore InboxStore) Consumer {
	return &journalEntryPostedConsumer{
		pool:  pool,
		repo:  repo,
		inbox: inboxStore,
	}
}

func (c *journalEntryPostedConsumer) HandleEvent(ctx context.Context, envelope events.EventEnvelope) error {
	if err := envelope.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEventPayload, err)
	}

	if envelope.Payload == nil {
		return ErrNilEventPayload
	}

	if envelope.EventType != events.EventTypeJournalEntryPosted {
		// Ignore events outside ledger.journal_entry.posted.v1
		return nil
	}

	if c.pool == nil {
		return errors.New("database pool is not initialized")
	}

	postedPayload, err := parseJournalEntryPostedPayload(envelope.Payload)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidEventPayload, err)
	}

	if postedPayload.JournalEntryID == "" || postedPayload.Currency == "" || len(postedPayload.Lines) == 0 {
		return ErrInvalidEventPayload
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

	// Update balance projection for each journal line
	for _, line := range postedPayload.Lines {
		if line.AccountID == "" {
			return fmt.Errorf("%w: line missing account_id", ErrInvalidEventPayload)
		}
		if line.DebitAmountMinor < 0 || line.CreditAmountMinor < 0 {
			return fmt.Errorf("%w: line negative amount", ErrInvalidEventPayload)
		}

		err := c.repo.UpsertAccountBalanceTx(
			ctx,
			tx,
			line.AccountID,
			postedPayload.Currency,
			line.DebitAmountMinor,
			line.CreditAmountMinor,
		)
		if err != nil {
			return fmt.Errorf("upsert balance for account %s: %w", line.AccountID, err)
		}
	}

	return tx.Commit(ctx)
}

func parseJournalEntryPostedPayload(payload map[string]any) (*events.JournalEntryPostedPayload, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal event payload map: %w", err)
	}
	var res events.JournalEntryPostedPayload
	if err := json.Unmarshal(data, &res); err != nil {
		return nil, fmt.Errorf("unmarshal journal entry posted payload: %w", err)
	}
	return &res, nil
}
