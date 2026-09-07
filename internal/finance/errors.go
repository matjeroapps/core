package finance

import "errors"

var (
	ErrAccountNotFound      = errors.New("ledger account not found")
	ErrDuplicateAccountCode = errors.New("account code already exists")
	ErrInactiveAccount      = errors.New("ledger account is inactive or frozen")
	ErrInvalidAccountType   = errors.New("invalid account type")
	ErrInvalidAccountStatus = errors.New("invalid account status")
	ErrUnbalancedEntry      = errors.New("unbalanced journal entry: total debits must equal total credits")
	ErrInvalidLineAmounts   = errors.New("invalid line amounts: line must have either debit > 0 or credit > 0, but not both or neither")
	ErrCurrencyMismatch     = errors.New("currency mismatch between journal entry and ledger account")
	ErrDuplicatePosting     = errors.New("duplicate journal entry posting for reference")
	ErrMissingLines         = errors.New("journal entry must contain at least two lines")
	ErrJournalEntryNotFound = errors.New("journal entry not found")
	ErrInvalidReference     = errors.New("invalid reference type or id")
)
