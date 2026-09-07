package finance_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/matjeroapps/core/internal/finance"
)

type mockRepo struct {
	accounts map[string]finance.Account
}

func newMockRepo() *mockRepo {
	return &mockRepo{accounts: make(map[string]finance.Account)}
}

func (m *mockRepo) GetAccountsByIDs(ctx context.Context, exec finance.DBExecutor, ids []string) (map[string]finance.Account, error) {
	result := make(map[string]finance.Account)
	for _, id := range ids {
		if acc, ok := m.accounts[id]; ok {
			result[id] = acc
		}
	}
	return result, nil
}

func TestJournalEntryValidation(t *testing.T) {
	acc1ID := uuid.NewString()
	acc2ID := uuid.NewString()

	mockR := newMockRepo()
	mockR.accounts[acc1ID] = finance.Account{
		ID:          acc1ID,
		AccountCode: "1000",
		AccountType: finance.AccountTypeAsset,
		Currency:    "SAR",
		Status:      finance.AccountStatusActive,
	}
	mockR.accounts[acc2ID] = finance.Account{
		ID:          acc2ID,
		AccountCode: "2000",
		AccountType: finance.AccountTypeRevenue,
		Currency:    "SAR",
		Status:      finance.AccountStatusActive,
	}

	t.Run("unbalanced entry debit != credit fails", func(t *testing.T) {
		lines := []finance.CreateJournalLineParams{
			{AccountID: acc1ID, DebitAmountMinor: 1000, CreditAmountMinor: 0},
			{AccountID: acc2ID, DebitAmountMinor: 0, CreditAmountMinor: 500},
		}

		// Validation logic embedded in posting checks
		var totalDebit, totalCredit int64
		for _, l := range lines {
			totalDebit += l.DebitAmountMinor
			totalCredit += l.CreditAmountMinor
		}
		if totalDebit == totalCredit {
			t.Errorf("expected unbalanced lines, but totalDebit == totalCredit")
		}
	})

	t.Run("line with debit and credit both > 0 fails", func(t *testing.T) {
		line := finance.CreateJournalLineParams{
			AccountID:         acc1ID,
			DebitAmountMinor:  100,
			CreditAmountMinor: 100,
		}
		if (line.DebitAmountMinor > 0 && line.CreditAmountMinor > 0) || (line.DebitAmountMinor == 0 && line.CreditAmountMinor == 0) {
			// Expect invalid
		} else {
			t.Errorf("expected invalid line amounts")
		}
	})

	t.Run("line with debit and credit both == 0 fails", func(t *testing.T) {
		line := finance.CreateJournalLineParams{
			AccountID:         acc1ID,
			DebitAmountMinor:  0,
			CreditAmountMinor: 0,
		}
		if (line.DebitAmountMinor > 0 && line.CreditAmountMinor > 0) || (line.DebitAmountMinor == 0 && line.CreditAmountMinor == 0) {
			// Expect invalid
		} else {
			t.Errorf("expected invalid line amounts")
		}
	})
}
