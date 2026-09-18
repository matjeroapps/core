package markets

import (
	"encoding/json"

	"core/packages/i18n"
)

type Country struct {
	Code     string `json:"code"`
	Name     string `json:"name"`
	Timezone string `json:"timezone"`
	Status   string `json:"status"`
}

type Currency struct {
	Code      string `json:"code"`
	Symbol    string `json:"symbol"`
	MinorUnit int    `json:"minor_unit"`
	Status    string `json:"status"`
}

type Market struct {
	Code             string         `json:"code"`
	Country          Country        `json:"country"`
	Currency         Currency       `json:"currency"`
	DefaultLocale    i18n.Locale    `json:"default_locale"`
	SupportedLocales []i18n.Locale  `json:"supported_locales"`
	Timezone         string         `json:"timezone"`
	Status           string         `json:"status"`
	Configuration    map[string]any `json:"configuration"`
}

type row struct {
	Code              string
	CountryCode       string
	CountryName       string
	CountryTimezone   string
	CountryStatus     string
	CurrencyCode      string
	CurrencySymbol    string
	CurrencyMinorUnit int
	CurrencyStatus    string
	DefaultLocale     string
	Timezone          string
	Status            string
	Configuration     []byte
	Locales           []string
}

func (r row) toMarket() (Market, error) {
	var configuration map[string]any
	if len(r.Configuration) > 0 {
		if err := json.Unmarshal(r.Configuration, &configuration); err != nil {
			return Market{}, err
		}
	} else {
		configuration = map[string]any{}
	}

	supportedLocales := make([]i18n.Locale, 0, len(r.Locales))
	for _, locale := range r.Locales {
		supportedLocales = append(supportedLocales, i18n.Locale(locale))
	}

	return Market{
		Code: r.Code,
		Country: Country{
			Code:     r.CountryCode,
			Name:     r.CountryName,
			Timezone: r.CountryTimezone,
			Status:   r.CountryStatus,
		},
		Currency: Currency{
			Code:      r.CurrencyCode,
			Symbol:    r.CurrencySymbol,
			MinorUnit: r.CurrencyMinorUnit,
			Status:    r.CurrencyStatus,
		},
		DefaultLocale:    i18n.Locale(r.DefaultLocale),
		SupportedLocales: supportedLocales,
		Timezone:         r.Timezone,
		Status:           r.Status,
		Configuration:    configuration,
	}, nil
}
