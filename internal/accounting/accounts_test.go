package accounting_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/matjeroapps/core/internal/accounting"
	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/modules/commerce"
)

type mockFinanceService struct {
	accounts map[string]*finance.Account
}

func newMockFinanceService() *mockFinanceService {
	return &mockFinanceService{
		accounts: make(map[string]*finance.Account),
	}
}

func (m *mockFinanceService) CreateAccount(ctx context.Context, params finance.CreateAccountParams) (*finance.Account, error) {
	if acc, exists := m.accounts[params.AccountCode]; exists {
		return acc, finance.ErrDuplicateAccountCode
	}

	acc := &finance.Account{
		ID:          "acc_" + params.AccountCode,
		AccountCode: params.AccountCode,
		Name:        params.Name,
		AccountType: params.AccountType,
		Currency:    params.Currency,
		Status:      finance.AccountStatusActive,
	}
	m.accounts[params.AccountCode] = acc
	return acc, nil
}

func (m *mockFinanceService) GetAccount(ctx context.Context, id string) (*finance.Account, error) {
	for _, acc := range m.accounts {
		if acc.ID == id {
			return acc, nil
		}
	}
	return nil, finance.ErrAccountNotFound
}

func (m *mockFinanceService) ListAccounts(ctx context.Context, page commerce.Page) ([]finance.Account, error) {
	var list []finance.Account
	for _, acc := range m.accounts {
		list = append(list, *acc)
	}
	return list, nil
}

func (m *mockFinanceService) PostJournalEntry(ctx context.Context, params finance.PostJournalEntryParams) (*finance.JournalEntry, error) {
	return nil, nil
}

func (m *mockFinanceService) PostJournalEntryTx(ctx context.Context, tx pgx.Tx, params finance.PostJournalEntryParams) (*finance.JournalEntry, error) {
	return nil, nil
}

func (m *mockFinanceService) GetJournalEntry(ctx context.Context, id string) (*finance.JournalEntry, error) {
	return nil, nil
}

func (m *mockFinanceService) GetJournalEntryByReference(ctx context.Context, refType, refID string) (*finance.JournalEntry, error) {
	return nil, finance.ErrJournalEntryNotFound
}

func (m *mockFinanceService) GetJournalEntryByReferenceTx(ctx context.Context, tx pgx.Tx, refType, refID string) (*finance.JournalEntry, error) {
	return nil, finance.ErrJournalEntryNotFound
}

func TestAccountRegistry(t *testing.T) {
	financeSvc := newMockFinanceService()
	registry := accounting.NewAccountRegistry(financeSvc)
	ctx := context.Background()

	t.Run("Ensure System Accounts Idempotency", func(t *testing.T) {
		accounts1, err := registry.EnsureSystemAccounts(ctx, "SAR")
		if err != nil {
			t.Fatalf("first EnsureSystemAccounts failed: %v", err)
		}

		if len(accounts1) != 3 {
			t.Fatalf("expected 3 system accounts, got %d", len(accounts1))
		}

		clearing1 := accounts1[accounting.RolePaymentClearing]
		if clearing1 == nil || clearing1.AccountCode != "1010-PAYMENT-CLEARING-SAR" {
			t.Errorf("unexpected clearing account: %v", clearing1)
		}

		// Second call should return existing accounts without error
		accounts2, err := registry.EnsureSystemAccounts(ctx, "SAR")
		if err != nil {
			t.Fatalf("second EnsureSystemAccounts failed: %v", err)
		}

		if len(accounts2) != 3 {
			t.Fatalf("expected 3 system accounts on second call, got %d", len(accounts2))
		}

		if accounts2[accounting.RolePaymentClearing].ID != clearing1.ID {
			t.Errorf("expected same account ID on second call, got %s vs %s", accounts2[accounting.RolePaymentClearing].ID, clearing1.ID)
		}
	})

	t.Run("Get Account By Role", func(t *testing.T) {
		acc, err := registry.GetAccountByRole(ctx, accounting.RoleCustomerFunds, "SAR")
		if err != nil {
			t.Fatalf("unexpected error getting account by role: %v", err)
		}

		if acc.AccountCode != "2010-CUSTOMER-FUNDS-SAR" {
			t.Errorf("expected account code 2010-CUSTOMER-FUNDS-SAR, got %s", acc.AccountCode)
		}
	})

	t.Run("Invalid Currency Rejection", func(t *testing.T) {
		_, err := registry.EnsureSystemAccounts(ctx, "INVALID")
		if err == nil {
			t.Fatalf("expected error for invalid currency, got nil")
		}
	})
}
