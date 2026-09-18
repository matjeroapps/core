package markets

import (
	"context"

	"core/packages/i18n"
)

type Service struct {
	repository Repository
}

func NewService(repository Repository) Service {
	return Service{repository: repository}
}

func (s Service) List(ctx context.Context, locale i18n.Locale) ([]Market, error) {
	return s.repository.List(ctx, locale)
}

func (s Service) GetByCode(ctx context.Context, code string, locale i18n.Locale) (Market, error) {
	return s.repository.GetByCode(ctx, code, locale)
}
