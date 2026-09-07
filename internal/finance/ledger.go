package finance

import (
	"time"
)

type AccountType string

const (
	AccountTypeAsset     AccountType = "ASSET"
	AccountTypeLiability AccountType = "LIABILITY"
	AccountTypeRevenue   AccountType = "REVENUE"
	AccountTypeExpense   AccountType = "EXPENSE"
	AccountTypeEquity    AccountType = "EQUITY"
)

func (t AccountType) IsValid() bool {
	switch t {
	case AccountTypeAsset, AccountTypeLiability, AccountTypeRevenue, AccountTypeExpense, AccountTypeEquity:
		return true
	default:
		return false
	}
}

type AccountStatus string

const (
	AccountStatusActive   AccountStatus = "ACTIVE"
	AccountStatusInactive AccountStatus = "INACTIVE"
	AccountStatusFrozen   AccountStatus = "FROZEN"
)

func (s AccountStatus) IsValid() bool {
	switch s {
	case AccountStatusActive, AccountStatusInactive, AccountStatusFrozen:
		return true
	default:
		return false
	}
}

type Account struct {
	ID          string        `json:"id"`
	AccountCode string        `json:"account_code"`
	Name        string        `json:"name"`
	AccountType AccountType   `json:"account_type"`
	Currency    string        `json:"currency"`
	Status      AccountStatus `json:"status"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type JournalEntry struct {
	ID            string        `json:"id"`
	ReferenceType string        `json:"reference_type"`
	ReferenceID   string        `json:"reference_id"`
	Description   string        `json:"description,omitempty"`
	Currency      string        `json:"currency"`
	PostedAt      time.Time     `json:"posted_at"`
	CreatedAt     time.Time     `json:"created_at"`
	Lines         []JournalLine `json:"lines"`
}

type JournalLine struct {
	ID                string    `json:"id"`
	JournalEntryID    string    `json:"journal_entry_id"`
	AccountID         string    `json:"account_id"`
	DebitAmountMinor  int64     `json:"debit_amount_minor"`
	CreditAmountMinor int64     `json:"credit_amount_minor"`
	CreatedAt         time.Time `json:"created_at"`
}

type CreateAccountParams struct {
	AccountCode string      `json:"account_code"`
	Name        string      `json:"name"`
	AccountType AccountType `json:"account_type"`
	Currency    string      `json:"currency"`
}

type CreateJournalLineParams struct {
	AccountID         string `json:"account_id"`
	DebitAmountMinor  int64  `json:"debit_amount_minor"`
	CreditAmountMinor int64  `json:"credit_amount_minor"`
}

type PostJournalEntryParams struct {
	ReferenceType string                    `json:"reference_type"`
	ReferenceID   string                    `json:"reference_id"`
	Description   string                    `json:"description,omitempty"`
	Currency      string                    `json:"currency"`
	Lines         []CreateJournalLineParams `json:"lines"`
	CorrelationID string                    `json:"-"`
	CausationID   string                    `json:"-"`
}
