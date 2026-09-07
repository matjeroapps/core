package settlement

import (
	"fmt"
	"time"
)

type SettlementStatus string

const (
	StatusPending    SettlementStatus = "PENDING"
	StatusCalculated SettlementStatus = "CALCULATED"
	StatusFinalized  SettlementStatus = "FINALIZED"
)

func (s SettlementStatus) IsValid() bool {
	switch s {
	case StatusPending, StatusCalculated, StatusFinalized:
		return true
	default:
		return false
	}
}

func (s SettlementStatus) CanTransitionTo(target SettlementStatus) bool {
	switch s {
	case StatusPending:
		return target == StatusCalculated || target == StatusFinalized
	case StatusCalculated:
		return target == StatusFinalized
	case StatusFinalized:
		return false // Finalized settlements are immutable
	default:
		return false
	}
}

type PeriodStatus string

const (
	PeriodStatusOpen       PeriodStatus = "OPEN"
	PeriodStatusCalculated PeriodStatus = "CALCULATED"
	PeriodStatusFinalized  PeriodStatus = "FINALIZED"
	PeriodStatusClosed     PeriodStatus = "CLOSED"
)

func (p PeriodStatus) IsValid() bool {
	switch p {
	case PeriodStatusOpen, PeriodStatusCalculated, PeriodStatusFinalized, PeriodStatusClosed:
		return true
	default:
		return false
	}
}

func (p PeriodStatus) CanTransitionTo(target PeriodStatus) bool {
	switch p {
	case PeriodStatusOpen:
		return target == PeriodStatusCalculated || target == PeriodStatusClosed
	case PeriodStatusCalculated:
		return target == PeriodStatusFinalized || target == PeriodStatusClosed
	case PeriodStatusFinalized, PeriodStatusClosed:
		return false
	default:
		return false
	}
}

type Settlement struct {
	ID                    string           `json:"id"`
	SettlementPeriodID    string           `json:"settlement_period_id"`
	AccountID             string           `json:"account_id"`
	Currency              string           `json:"currency"`
	GrossAmountMinor      int64            `json:"gross_amount_minor"`
	AdjustmentAmountMinor int64            `json:"adjustment_amount_minor"`
	NetAmountMinor        int64            `json:"net_amount_minor"`
	Status                SettlementStatus `json:"status"`
	CreatedAt             time.Time        `json:"created_at"`
	CalculatedAt          *time.Time       `json:"calculated_at,omitempty"`
	FinalizedAt           *time.Time       `json:"finalized_at,omitempty"`
}

func (s Settlement) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("settlement id is required")
	}
	if s.SettlementPeriodID == "" {
		return fmt.Errorf("settlement_period_id is required")
	}
	if s.AccountID == "" {
		return fmt.Errorf("account_id is required")
	}
	if len(s.Currency) != 3 {
		return fmt.Errorf("invalid currency code")
	}
	if !s.Status.IsValid() {
		return fmt.Errorf("invalid status")
	}
	if s.NetAmountMinor != s.GrossAmountMinor+s.AdjustmentAmountMinor {
		return fmt.Errorf("net amount (%d) must equal gross amount (%d) + adjustment amount (%d)",
			s.NetAmountMinor, s.GrossAmountMinor, s.AdjustmentAmountMinor)
	}
	return nil
}

type SettlementPeriod struct {
	ID        string       `json:"id"`
	StartDate time.Time    `json:"start_date"`
	EndDate   time.Time    `json:"end_date"`
	Status    PeriodStatus `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
	ClosedAt  *time.Time   `json:"closed_at,omitempty"`
}

type CalculateSettlementParams struct {
	PeriodID      string `json:"period_id"`
	AccountID     string `json:"account_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`
}

type FinalizeSettlementParams struct {
	PeriodID      string `json:"period_id"`
	CorrelationID string `json:"correlation_id,omitempty"`
	CausationID   string `json:"causation_id,omitempty"`
}
