package settlement

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/matjeroapps/core/internal/balance"
	"github.com/matjeroapps/core/modules/commerce"
	"github.com/matjeroapps/core/packages/events"
	"github.com/matjeroapps/core/packages/outbox"
)

type BalanceReader interface {
	GetAccountBalance(ctx context.Context, accountID string) (*balance.AccountBalance, error)
	ListAccountBalances(ctx context.Context, page commerce.Page) ([]balance.AccountBalance, error)
}

type OutboxStore interface {
	Enqueue(ctx context.Context, tx pgx.Tx, event events.EventEnvelope) error
}

type Service interface {
	CalculatePeriodSettlements(ctx context.Context, params CalculateSettlementParams) ([]Settlement, error)
	FinalizePeriodSettlements(ctx context.Context, params FinalizeSettlementParams) ([]Settlement, error)
	GetSettlement(ctx context.Context, id string) (*Settlement, error)
	GetSettlementByPeriodAndAccount(ctx context.Context, periodID, accountID string) (*Settlement, error)
	ListAccountSettlements(ctx context.Context, accountID string, page commerce.Page) ([]Settlement, error)
	ListPeriodSettlements(ctx context.Context, periodID string, page commerce.Page) ([]Settlement, error)
}

type service struct {
	repo       Repository
	balance    BalanceReader
	calculator Calculator
	outbox     OutboxStore
}

func NewService(repo Repository, balanceReader BalanceReader) Service {
	return &service{
		repo:       repo,
		balance:    balanceReader,
		calculator: NewCalculator(),
		outbox:     outbox.NewStore(),
	}
}

func NewServiceWithDeps(repo Repository, balanceReader BalanceReader, calc Calculator, outboxStore OutboxStore) Service {
	return &service{
		repo:       repo,
		balance:    balanceReader,
		calculator: calc,
		outbox:     outboxStore,
	}
}

func (s *service) CalculatePeriodSettlements(ctx context.Context, params CalculateSettlementParams) ([]Settlement, error) {
	periodID := strings.TrimSpace(params.PeriodID)
	if periodID == "" {
		return nil, ErrInvalidSettlementPeriodID
	}

	period, err := s.repo.GetSettlementPeriodByID(ctx, nil, periodID)
	if err != nil {
		return nil, err
	}

	if period.Status == PeriodStatusFinalized || period.Status == PeriodStatusClosed {
		return nil, ErrSettlementAlreadyFinalized
	}

	accountID := strings.TrimSpace(params.AccountID)
	var balancesToCalculate []balance.AccountBalance

	if accountID != "" {
		bal, err := s.balance.GetAccountBalance(ctx, accountID)
		if err != nil {
			return nil, fmt.Errorf("get account balance: %w", err)
		}
		balancesToCalculate = append(balancesToCalculate, *bal)
	} else {
		// List balances for all accounts
		bals, err := s.balance.ListAccountBalances(ctx, commerce.Page{Limit: 1000})
		if err != nil {
			return nil, fmt.Errorf("list account balances: %w", err)
		}
		if len(bals) == 0 {
			return nil, ErrNoAccountBalances
		}
		balancesToCalculate = bals
	}

	var calculatedSettlements []Settlement
	var calculatedEvents []events.EventEnvelope

	for _, bal := range balancesToCalculate {
		st, err := s.calculator.Calculate(ctx, periodID, bal)
		if err != nil {
			return nil, fmt.Errorf("calculate settlement for account %s: %w", bal.AccountID, err)
		}

		evtPayload := events.SettlementCalculatedPayload{
			SettlementID:          st.ID,
			PeriodID:              st.SettlementPeriodID,
			AccountID:             st.AccountID,
			Currency:              st.Currency,
			GrossAmountMinor:      st.GrossAmountMinor,
			AdjustmentAmountMinor: st.AdjustmentAmountMinor,
			NetAmountMinor:        st.NetAmountMinor,
			Status:                string(st.Status),
			CalculatedAt:          *st.CalculatedAt,
		}

		evt, err := events.NewSettlementCalculatedEvent(evtPayload, params.CorrelationID, params.CausationID)
		if err != nil {
			return nil, fmt.Errorf("create settlement calculated event: %w", err)
		}

		calculatedSettlements = append(calculatedSettlements, *st)
		calculatedEvents = append(calculatedEvents, evt)
	}

	// Persist inside transaction
	err = s.repo.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		for _, st := range calculatedSettlements {
			if err := s.repo.SaveSettlement(ctx, tx, st); err != nil {
				return err
			}
		}

		if _, err := s.repo.UpdateSettlementPeriodStatus(ctx, tx, periodID, PeriodStatusCalculated); err != nil {
			return fmt.Errorf("update period status: %w", err)
		}

		for _, evt := range calculatedEvents {
			if err := s.outbox.Enqueue(ctx, tx, evt); err != nil {
				return fmt.Errorf("enqueue settlement calculated event: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return calculatedSettlements, nil
}

func (s *service) FinalizePeriodSettlements(ctx context.Context, params FinalizeSettlementParams) ([]Settlement, error) {
	periodID := strings.TrimSpace(params.PeriodID)
	if periodID == "" {
		return nil, ErrInvalidSettlementPeriodID
	}

	period, err := s.repo.GetSettlementPeriodByID(ctx, nil, periodID)
	if err != nil {
		return nil, err
	}

	if period.Status == PeriodStatusFinalized || period.Status == PeriodStatusClosed {
		return nil, ErrSettlementAlreadyFinalized
	}

	var finalizedSettlements []Settlement

	err = s.repo.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		finalizedSettlements, err = s.repo.FinalizeSettlementsForPeriod(ctx, tx, periodID)
		if err != nil {
			return fmt.Errorf("finalize settlements for period: %w", err)
		}

		if _, err := s.repo.UpdateSettlementPeriodStatus(ctx, tx, periodID, PeriodStatusFinalized); err != nil {
			return fmt.Errorf("update period status to finalized: %w", err)
		}

		for _, st := range finalizedSettlements {
			evtPayload := events.SettlementFinalizedPayload{
				SettlementID:   st.ID,
				PeriodID:       st.SettlementPeriodID,
				AccountID:      st.AccountID,
				Currency:       st.Currency,
				NetAmountMinor: st.NetAmountMinor,
				Status:         string(st.Status),
				FinalizedAt:    *st.FinalizedAt,
			}

			evt, err := events.NewSettlementFinalizedEvent(evtPayload, params.CorrelationID, params.CausationID)
			if err != nil {
				return fmt.Errorf("create settlement finalized event: %w", err)
			}

			if err := s.outbox.Enqueue(ctx, tx, evt); err != nil {
				return fmt.Errorf("enqueue settlement finalized event: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return finalizedSettlements, nil
}

func (s *service) GetSettlement(ctx context.Context, id string) (*Settlement, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrSettlementNotFound
	}
	return s.repo.GetSettlementByID(ctx, nil, id)
}

func (s *service) GetSettlementByPeriodAndAccount(ctx context.Context, periodID, accountID string) (*Settlement, error) {
	periodID = strings.TrimSpace(periodID)
	accountID = strings.TrimSpace(accountID)
	if periodID == "" || accountID == "" {
		return nil, ErrSettlementNotFound
	}
	return s.repo.GetSettlementByPeriodAndAccount(ctx, nil, periodID, accountID)
}

func (s *service) ListAccountSettlements(ctx context.Context, accountID string, page commerce.Page) ([]Settlement, error) {
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return nil, ErrInvalidAccountID
	}
	return s.repo.ListSettlementsByAccount(ctx, nil, accountID, page)
}

func (s *service) ListPeriodSettlements(ctx context.Context, periodID string, page commerce.Page) ([]Settlement, error) {
	periodID = strings.TrimSpace(periodID)
	if periodID == "" {
		return nil, ErrInvalidSettlementPeriodID
	}
	return s.repo.ListSettlementsByPeriod(ctx, nil, periodID, page)
}
