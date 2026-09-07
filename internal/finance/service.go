package finance

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/packages/events"
	"github.com/matjeroapps/core/packages/outbox"
)

type Service interface {
	CreateAccount(ctx context.Context, params CreateAccountParams) (*Account, error)
	GetAccount(ctx context.Context, id string) (*Account, error)
	ListAccounts(ctx context.Context, page commerce.Page) ([]Account, error)
	PostJournalEntry(ctx context.Context, params PostJournalEntryParams) (*JournalEntry, error)
	PostJournalEntryTx(ctx context.Context, tx pgx.Tx, params PostJournalEntryParams) (*JournalEntry, error)
	GetJournalEntry(ctx context.Context, id string) (*JournalEntry, error)
	GetJournalEntryByReference(ctx context.Context, refType, refID string) (*JournalEntry, error)
	GetJournalEntryByReferenceTx(ctx context.Context, tx pgx.Tx, refType, refID string) (*JournalEntry, error)
}

type OutboxStore interface {
	Enqueue(ctx context.Context, tx pgx.Tx, event events.EventEnvelope) error
}

type service struct {
	repo   Repository
	outbox OutboxStore
}

func NewService(repo Repository) Service {
	return &service{
		repo:   repo,
		outbox: outbox.NewStore(),
	}
}

func NewServiceWithOutbox(repo Repository, outboxStore OutboxStore) Service {
	return &service{
		repo:   repo,
		outbox: outboxStore,
	}
}

func (s *service) CreateAccount(ctx context.Context, params CreateAccountParams) (*Account, error) {
	code := strings.TrimSpace(params.AccountCode)
	if code == "" {
		return nil, fmt.Errorf("account_code is required")
	}

	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}

	if !params.AccountType.IsValid() {
		return nil, ErrInvalidAccountType
	}

	currency := strings.ToUpper(strings.TrimSpace(params.Currency))
	if len(currency) != 3 {
		return nil, fmt.Errorf("invalid currency code: must be ISO 4217 3-character code")
	}

	now := time.Now().UTC()
	account := Account{
		ID:          uuid.NewString(),
		AccountCode: code,
		Name:        name,
		AccountType: params.AccountType,
		Currency:    currency,
		Status:      AccountStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if err := s.repo.CreateAccount(ctx, nil, account); err != nil {
		return nil, err
	}

	return &account, nil
}

func (s *service) GetAccount(ctx context.Context, id string) (*Account, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrAccountNotFound
	}
	return s.repo.GetAccountByID(ctx, nil, id)
}

func (s *service) ListAccounts(ctx context.Context, page commerce.Page) ([]Account, error) {
	return s.repo.ListAccounts(ctx, nil, page)
}

func (s *service) PostJournalEntry(ctx context.Context, params PostJournalEntryParams) (*JournalEntry, error) {
	var entry *JournalEntry
	err := s.repo.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		entry, err = s.PostJournalEntryTx(ctx, tx, params)
		return err
	})
	if err != nil {
		return nil, err
	}
	return entry, nil
}

func (s *service) PostJournalEntryTx(ctx context.Context, tx pgx.Tx, params PostJournalEntryParams) (*JournalEntry, error) {
	refType := strings.TrimSpace(params.ReferenceType)
	refID := strings.TrimSpace(params.ReferenceID)
	if refType == "" || refID == "" {
		return nil, ErrInvalidReference
	}

	currency := strings.ToUpper(strings.TrimSpace(params.Currency))
	if len(currency) != 3 {
		return nil, fmt.Errorf("invalid currency code: must be ISO 4217 3-character code")
	}

	if len(params.Lines) < 2 {
		return nil, ErrMissingLines
	}

	var totalDebit int64
	var totalCredit int64
	accountIDSet := make(map[string]bool)
	accountIDs := make([]string, 0, len(params.Lines))

	for i, line := range params.Lines {
		accID := strings.TrimSpace(line.AccountID)
		if accID == "" {
			return nil, fmt.Errorf("line %d: account_id is required", i)
		}
		if !accountIDSet[accID] {
			accountIDSet[accID] = true
			accountIDs = append(accountIDs, accID)
		}

		if line.DebitAmountMinor < 0 || line.CreditAmountMinor < 0 {
			return nil, ErrInvalidLineAmounts
		}

		// A line cannot have debit > 0 AND credit > 0, or debit == 0 AND credit == 0
		if (line.DebitAmountMinor > 0 && line.CreditAmountMinor > 0) || (line.DebitAmountMinor == 0 && line.CreditAmountMinor == 0) {
			return nil, ErrInvalidLineAmounts
		}

		totalDebit += line.DebitAmountMinor
		totalCredit += line.CreditAmountMinor
	}

	if totalDebit != totalCredit || totalDebit <= 0 {
		return nil, ErrUnbalancedEntry
	}

	// Validate accounts exist, are active, and match currency
	accountsMap, err := s.repo.GetAccountsByIDs(ctx, tx, accountIDs)
	if err != nil {
		return nil, fmt.Errorf("fetch accounts for validation: %w", err)
	}

	for _, accID := range accountIDs {
		acc, ok := accountsMap[accID]
		if !ok {
			return nil, fmt.Errorf("%w: account_id %s", ErrAccountNotFound, accID)
		}
		if acc.Status != AccountStatusActive {
			return nil, fmt.Errorf("%w: account %s is %s", ErrInactiveAccount, acc.AccountCode, acc.Status)
		}
		if acc.Currency != currency {
			return nil, fmt.Errorf("%w: account %s currency %s does not match entry currency %s", ErrCurrencyMismatch, acc.AccountCode, acc.Currency, currency)
		}
	}

	now := time.Now().UTC()
	entryID := uuid.NewString()

	lines := make([]JournalLine, 0, len(params.Lines))
	eventLinesPayload := make([]events.JournalEntryPostedLinePayload, 0, len(params.Lines))

	for _, l := range params.Lines {
		lineID := uuid.NewString()
		accID := strings.TrimSpace(l.AccountID)

		lines = append(lines, JournalLine{
			ID:                lineID,
			JournalEntryID:    entryID,
			AccountID:         accID,
			DebitAmountMinor:  l.DebitAmountMinor,
			CreditAmountMinor: l.CreditAmountMinor,
			CreatedAt:         now,
		})

		eventLinesPayload = append(eventLinesPayload, events.JournalEntryPostedLinePayload{
			AccountID:         accID,
			DebitAmountMinor:  l.DebitAmountMinor,
			CreditAmountMinor: l.CreditAmountMinor,
		})
	}

	entry := JournalEntry{
		ID:            entryID,
		ReferenceType: refType,
		ReferenceID:   refID,
		Description:   strings.TrimSpace(params.Description),
		Currency:      currency,
		PostedAt:      now,
		CreatedAt:     now,
		Lines:         lines,
	}

	eventPayload := events.JournalEntryPostedPayload{
		JournalEntryID: entry.ID,
		ReferenceType:  entry.ReferenceType,
		ReferenceID:    entry.ReferenceID,
		Description:    entry.Description,
		Currency:       entry.Currency,
		PostedAt:       entry.PostedAt,
		Lines:          eventLinesPayload,
	}

	event, err := events.NewJournalEntryPostedEvent(eventPayload, params.CorrelationID, params.CausationID)
	if err != nil {
		return nil, fmt.Errorf("create journal entry posted event: %w", err)
	}

	if err := s.repo.PostJournalEntryTx(ctx, tx, entry); err != nil {
		return nil, err
	}
	if err := s.outbox.Enqueue(ctx, tx, event); err != nil {
		return nil, fmt.Errorf("enqueue journal entry posted event: %w", err)
	}

	return &entry, nil
}

func (s *service) GetJournalEntry(ctx context.Context, id string) (*JournalEntry, error) {
	if strings.TrimSpace(id) == "" {
		return nil, ErrJournalEntryNotFound
	}
	return s.repo.GetJournalEntryByID(ctx, nil, id)
}

func (s *service) GetJournalEntryByReference(ctx context.Context, refType, refID string) (*JournalEntry, error) {
	refType = strings.TrimSpace(refType)
	refID = strings.TrimSpace(refID)
	if refType == "" || refID == "" {
		return nil, ErrInvalidReference
	}
	return s.repo.GetJournalEntryByReference(ctx, nil, refType, refID)
}

func (s *service) GetJournalEntryByReferenceTx(ctx context.Context, tx pgx.Tx, refType, refID string) (*JournalEntry, error) {
	refType = strings.TrimSpace(refType)
	refID = strings.TrimSpace(refID)
	if refType == "" || refID == "" {
		return nil, ErrInvalidReference
	}
	return s.repo.GetJournalEntryByReference(ctx, tx, refType, refID)
}
