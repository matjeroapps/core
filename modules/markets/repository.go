package markets

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"core/packages/i18n"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) Repository {
	return Repository{pool: pool}
}

func (r Repository) List(ctx context.Context, locale i18n.Locale) ([]Market, error) {
	rows, err := r.pool.Query(ctx, marketQuery, locale, fallbackLocale(locale))
	if err != nil {
		return nil, fmt.Errorf("list markets: %w", err)
	}
	defer rows.Close()

	markets := make([]Market, 0, 8)
	for rows.Next() {
		market, err := scanMarket(rows.Scan)
		if err != nil {
			return nil, err
		}
		markets = append(markets, market)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("iterate markets: %w", rows.Err())
	}

	return markets, nil
}

func (r Repository) GetByCode(ctx context.Context, code string, locale i18n.Locale) (Market, error) {
	row := r.pool.QueryRow(ctx, marketQueryByCode, code, locale, fallbackLocale(locale))
	market, err := scanOneMarket(row.Scan)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Market{}, ErrNotFound
		}
		return Market{}, err
	}
	return market, nil
}

func fallbackLocale(locale i18n.Locale) string {
	if locale == i18n.LocaleArabic {
		return string(i18n.LocaleEnglish)
	}
	return string(i18n.LocaleArabic)
}

const marketQuery = `
SELECT
    m.code,
    c.code,
    COALESCE(ct.name, cte.name, c.code) AS country_name,
    c.timezone,
    c.status,
    cur.code,
    cur.symbol,
    cur.minor_unit,
    cur.status,
    m.default_locale,
    m.timezone,
    m.status,
    m.configuration,
    COALESCE(array_agg(ml.locale ORDER BY ml.is_default DESC, ml.sort_order, ml.locale) FILTER (WHERE ml.locale IS NOT NULL), ARRAY[]::text[])
FROM markets m
JOIN countries c ON c.code = m.country_code
JOIN currencies cur ON cur.code = m.currency_code
LEFT JOIN country_translations ct ON ct.country_code = c.code AND ct.locale = $1
LEFT JOIN country_translations cte ON cte.country_code = c.code AND cte.locale = $2
LEFT JOIN market_locales ml ON ml.market_code = m.code AND ml.is_enabled = true
GROUP BY m.code, c.code, c.timezone, c.status, cur.code, cur.symbol, cur.minor_unit, cur.status, m.default_locale, m.timezone, m.status, m.configuration, ct.name, cte.name
ORDER BY m.code
`

const marketQueryByCode = `
SELECT
    m.code,
    c.code,
    COALESCE(ct.name, cte.name, c.code) AS country_name,
    c.timezone,
    c.status,
    cur.code,
    cur.symbol,
    cur.minor_unit,
    cur.status,
    m.default_locale,
    m.timezone,
    m.status,
    m.configuration,
    COALESCE(array_agg(ml.locale ORDER BY ml.is_default DESC, ml.sort_order, ml.locale) FILTER (WHERE ml.locale IS NOT NULL), ARRAY[]::text[])
FROM markets m
JOIN countries c ON c.code = m.country_code
JOIN currencies cur ON cur.code = m.currency_code
LEFT JOIN country_translations ct ON ct.country_code = c.code AND ct.locale = $2
LEFT JOIN country_translations cte ON cte.country_code = c.code AND cte.locale = $3
LEFT JOIN market_locales ml ON ml.market_code = m.code AND ml.is_enabled = true
WHERE m.code = $1
GROUP BY m.code, c.code, c.timezone, c.status, cur.code, cur.symbol, cur.minor_unit, cur.status, m.default_locale, m.timezone, m.status, m.configuration, ct.name, cte.name
`

func scanOneMarket(scan func(dest ...any) error) (Market, error) {
	var r row
	if err := scan(
		&r.Code,
		&r.CountryCode,
		&r.CountryName,
		&r.CountryTimezone,
		&r.CountryStatus,
		&r.CurrencyCode,
		&r.CurrencySymbol,
		&r.CurrencyMinorUnit,
		&r.CurrencyStatus,
		&r.DefaultLocale,
		&r.Timezone,
		&r.Status,
		&r.Configuration,
		&r.Locales,
	); err != nil {
		return Market{}, fmt.Errorf("scan market: %w", err)
	}
	return r.toMarket()
}

func scanMarket(scan func(dest ...any) error) (Market, error) {
	return scanOneMarket(scan)
}
