package balance

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/matjeroapps/core/modules/commerce"
)

type Service interface {
	GetAccountBalance(ctx context.Context, accountID string) (*AccountBalance, error)
	ListAccountBalances(ctx context.Context, page commerce.Page) ([]AccountBalance, error)
	CreateSettlementPeriod(ctx context.Context, params CreateSettlementPeriodParams) (*SettlementPeriod, error)
	GetSettlementPeriod(ctx context.Context, id string) (*SettlementPeriod, error)
	CloseSettlementPeriod(ctx context.Context, id string) (*SettlementPeriod, error)
	ListSettlementPeriods(ctx context.Context, page commerce.Page) ([]SettlementPeriod, error)
}

type service struct {
	repo Repository
}

func NewService(repo Repository) Service {
	return &service{repo: repo}
}

func (s *service) GetAccountBalance(ctx context.Context, accountID string) (*AccountBalance, error) {
	if accountID == "" {
		return nil, ErrInvalidAccountID
	}
	return s.repo.GetAccountBalance(ctx, nil, accountID)
}

func (s *service) ListAccountBalances(ctx context.Context, page commerce.Page) ([]AccountBalance, error) {
	return s.repo.ListAccountBalances(ctx, nil, page)
}

func (s *service) CreateSettlementPeriod(ctx context.Context, params CreateSettlementPeriodParams) (*SettlementPeriod, error) {
	if params.StartDate.IsZero() || params.EndDate.IsZero() {
		return nil, ErrInvalidSettlementPeriodDates
	}
	if !params.EndDate.After(params.StartDate) {
		return nil, ErrInvalidSettlementPeriodDates
	}

	period := SettlementPeriod{
		ID:        uuid.NewString(),
		StartDate: params.StartDate,
		EndDate:   params.EndDate,
		Status:    SettlementPeriodStatusOpen,
		CreatedAt: time.Now().UTC(),
	}

	if err := s.repo.CreateSettlementPeriod(ctx, nil, period); err != nil {
		return nil, fmt.Errorf("create settlement period: %w", err)
	}

	return &period, nil
}

func (s *service) GetSettlementPeriod(ctx context.Context, id string) (*SettlementPeriod, error) {
	if id == "" {
		return nil, ErrSettlementPeriodNotFound
	}
	return s.repo.GetSettlementPeriodByID(ctx, nil, id)
}

func (s *service) CloseSettlementPeriod(ctx context.Context, id string) (*SettlementPeriod, error) {
	if id == "" {
		return nil, ErrSettlementPeriodNotFound
	}
	return s.repo.CloseSettlementPeriod(ctx, nil, id)
}

func (s *service) ListSettlementPeriods(ctx context.Context, page commerce.Page) ([]SettlementPeriod, error) {
	return s.repo.ListSettlementPeriods(ctx, nil, page)
}
