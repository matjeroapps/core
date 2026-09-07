package balance

import (
	"time"
)

// Accounting Sign Convention:
//
// In this balance projection system:
// balance_minor = debit_total_minor - credit_total_minor
//
// For each posted journal line:
// 1. Debit Entry of amount D:
//    - Increases debit_total_minor by D
//    - Increases balance_minor by D
// 2. Credit Entry of amount C:
//    - Increases credit_total_minor by C
//    - Decreases balance_minor by C
//
// Positive balance_minor indicates a net debit position.
// Negative balance_minor indicates a net credit position.

type AccountBalance struct {
	ID               string    `json:"id"`
	AccountID        string    `json:"account_id"`
	Currency         string    `json:"currency"`
	DebitTotalMinor  int64     `json:"debit_total_minor"`
	CreditTotalMinor int64     `json:"credit_total_minor"`
	BalanceMinor     int64     `json:"balance_minor"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type SettlementPeriodStatus string

const (
	SettlementPeriodStatusOpen   SettlementPeriodStatus = "OPEN"
	SettlementPeriodStatusClosed SettlementPeriodStatus = "CLOSED"
)

func (s SettlementPeriodStatus) IsValid() bool {
	switch s {
	case SettlementPeriodStatusOpen, SettlementPeriodStatusClosed:
		return true
	default:
		return false
	}
}

type SettlementPeriod struct {
	ID        string                 `json:"id"`
	StartDate time.Time              `json:"start_date"`
	EndDate   time.Time              `json:"end_date"`
	Status    SettlementPeriodStatus `json:"status"`
	CreatedAt time.Time              `json:"created_at"`
	ClosedAt  *time.Time             `json:"closed_at,omitempty"`
}

type CreateSettlementPeriodParams struct {
	StartDate time.Time `json:"start_date"`
	EndDate   time.Time `json:"end_date"`
}
