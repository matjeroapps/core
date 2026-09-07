package marketplace_finance

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/matjeroapps/core/packages/events"
	"github.com/matjeroapps/core/packages/outbox"
)

type OutboxStore interface {
	Enqueue(ctx context.Context, tx pgx.Tx, event events.EventEnvelope) error
}

type CreateRuleParams struct {
	Name             string         `json:"name"`
	RuleType         RuleType       `json:"rule_type"`
	Percentage       float64        `json:"percentage"`
	FixedAmountMinor int64          `json:"fixed_amount_minor"`
	Currency         string         `json:"currency"`
	AllocationType   AllocationType `json:"allocation_type"`
	Status           RuleStatus     `json:"status"`
}

type AllocateSettlementParams struct {
	SettlementID  string `json:"settlement_id"`
	CorrelationID string `json:"correlation_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`
}

type Service interface {
	CreateRule(ctx context.Context, params CreateRuleParams) (*FinancialRule, error)
	ListRules(ctx context.Context, statusFilter string) ([]FinancialRule, error)
	AllocateSettlement(ctx context.Context, params AllocateSettlementParams) ([]SettlementAllocation, error)
	ListAllocations(ctx context.Context, settlementID string) ([]SettlementAllocation, error)
}

type service struct {
	repo       Repository
	calculator *Calculator
	outbox     OutboxStore
}

func NewService(repo Repository) Service {
	return &service{
		repo:       repo,
		calculator: NewCalculator(),
		outbox:     outbox.NewStore(),
	}
}

func NewServiceWithDeps(repo Repository, calc *Calculator, outboxStore OutboxStore) Service {
	return &service{
		repo:       repo,
		calculator: calc,
		outbox:     outboxStore,
	}
}

func (s *service) CreateRule(ctx context.Context, params CreateRuleParams) (*FinancialRule, error) {
	status := params.Status
	if status == "" {
		status = RuleStatusActive
	}

	rule := FinancialRule{
		ID:               uuid.NewString(),
		Name:             strings.TrimSpace(params.Name),
		RuleType:         params.RuleType,
		Percentage:       params.Percentage,
		FixedAmountMinor: params.FixedAmountMinor,
		Currency:         strings.ToUpper(strings.TrimSpace(params.Currency)),
		AllocationType:   params.AllocationType,
		Status:           status,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
	}

	if err := rule.Validate(); err != nil {
		return nil, err
	}

	if err := s.repo.CreateFinancialRule(ctx, nil, rule); err != nil {
		return nil, fmt.Errorf("create financial rule: %w", err)
	}

	return &rule, nil
}

func (s *service) ListRules(ctx context.Context, statusFilter string) ([]FinancialRule, error) {
	return s.repo.ListFinancialRules(ctx, nil, statusFilter)
}

func (s *service) AllocateSettlement(ctx context.Context, params AllocateSettlementParams) ([]SettlementAllocation, error) {
	settlementID := strings.TrimSpace(params.SettlementID)
	if settlementID == "" {
		return nil, ErrSettlementNotFound
	}

	st, err := s.repo.GetSettlementByID(ctx, nil, settlementID)
	if err != nil {
		return nil, err
	}

	activeRules, err := s.repo.GetActiveFinancialRulesByCurrency(ctx, nil, st.Currency)
	if err != nil {
		return nil, fmt.Errorf("get active rules: %w", err)
	}

	allocations, err := s.calculator.CalculateAllocations(st, activeRules)
	if err != nil {
		return nil, err
	}

	err = s.repo.WithTx(ctx, func(ctx context.Context, tx pgx.Tx) error {
		if err := s.repo.SaveSettlementAllocations(ctx, tx, allocations); err != nil {
			return err
		}

		correlationID := params.CorrelationID
		if correlationID == "" {
			correlationID = uuid.NewString()
		}
		causationID := params.CausationID

		for _, alloc := range allocations {
			evtPayload := events.AllocationCalculatedPayload{
				SettlementID:   alloc.SettlementID,
				AllocationID:   alloc.ID,
				AccountID:      alloc.AccountID,
				AllocationType: string(alloc.AllocationType),
				AmountMinor:    alloc.AmountMinor,
				Currency:       alloc.Currency,
				CalculatedAt:   alloc.CreatedAt,
			}

			evt, err := events.NewAllocationCalculatedEvent(evtPayload, correlationID, causationID)
			if err != nil {
				return fmt.Errorf("create allocation calculated event: %w", err)
			}

			if err := s.outbox.Enqueue(ctx, tx, evt); err != nil {
				return fmt.Errorf("enqueue allocation calculated event: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return allocations, nil
}

func (s *service) ListAllocations(ctx context.Context, settlementID string) ([]SettlementAllocation, error) {
	settlementID = strings.TrimSpace(settlementID)
	if settlementID == "" {
		return nil, ErrSettlementNotFound
	}
	return s.repo.ListSettlementAllocations(ctx, nil, settlementID)
}
