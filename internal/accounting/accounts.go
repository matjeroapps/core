package accounting

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/matjeroapps/core/internal/finance"
	"github.com/matjeroapps/core/modules/commerce"
)

type AccountRole string

const (
	RolePaymentClearing AccountRole = "PAYMENT_CLEARING"
	RoleCustomerFunds   AccountRole = "CUSTOMER_FUNDS"
	RolePlatformRevenue AccountRole = "PLATFORM_REVENUE"
)

// Default System Account Codes
const (
	DefaultCodePaymentClearing = "1010-PAYMENT-CLEARING"
	DefaultCodeCustomerFunds   = "2010-CUSTOMER-FUNDS"
	DefaultCodePlatformRevenue = "4010-PLATFORM-REVENUE"
)

type SystemAccountDef struct {
	Role        AccountRole
	AccountCode string
	Name        string
	AccountType finance.AccountType
}

var DefaultSystemAccountDefs = []SystemAccountDef{
	{
		Role:        RolePaymentClearing,
		AccountCode: DefaultCodePaymentClearing,
		Name:        "Payment Clearing Asset",
		AccountType: finance.AccountTypeAsset,
	},
	{
		Role:        RoleCustomerFunds,
		AccountCode: DefaultCodeCustomerFunds,
		Name:        "Customer Funds Liability",
		AccountType: finance.AccountTypeLiability,
	},
	{
		Role:        RolePlatformRevenue,
		AccountCode: DefaultCodePlatformRevenue,
		Name:        "Platform Revenue",
		AccountType: finance.AccountTypeRevenue,
	},
}

type AccountRegistry interface {
	EnsureSystemAccounts(ctx context.Context, currency string) (map[AccountRole]*finance.Account, error)
	GetAccountByRole(ctx context.Context, role AccountRole, currency string) (*finance.Account, error)
}

type accountRegistry struct {
	financeSvc finance.Service
}

func NewAccountRegistry(financeSvc finance.Service) AccountRegistry {
	return &accountRegistry{
		financeSvc: financeSvc,
	}
}

func (r *accountRegistry) EnsureSystemAccounts(ctx context.Context, currency string) (map[AccountRole]*finance.Account, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return nil, fmt.Errorf("invalid currency code: %s", currency)
	}

	result := make(map[AccountRole]*finance.Account)

	for _, def := range DefaultSystemAccountDefs {
		accountCode := fmt.Sprintf("%s-%s", def.AccountCode, currency)
		acc, err := r.financeSvc.CreateAccount(ctx, finance.CreateAccountParams{
			AccountCode: accountCode,
			Name:        fmt.Sprintf("%s (%s)", def.Name, currency),
			AccountType: def.AccountType,
			Currency:    currency,
		})

		if err != nil {
			if errors.Is(err, finance.ErrDuplicateAccountCode) {
				// Account already exists, fetch it from existing accounts
				fetchedAcc, fetchErr := r.findAccountByCode(ctx, accountCode)
				if fetchErr != nil {
					return nil, fmt.Errorf("fetch existing system account %s: %w", accountCode, fetchErr)
				}
				acc = fetchedAcc
			} else {
				return nil, fmt.Errorf("create system account %s: %w", accountCode, err)
			}
		}

		result[def.Role] = acc
	}

	return result, nil
}

func (r *accountRegistry) GetAccountByRole(ctx context.Context, role AccountRole, currency string) (*finance.Account, error) {
	accounts, err := r.EnsureSystemAccounts(ctx, currency)
	if err != nil {
		return nil, err
	}

	acc, ok := accounts[role]
	if !ok || acc == nil {
		return nil, fmt.Errorf("%w: role %s for currency %s", ErrMissingAccountMapping, role, currency)
	}

	return acc, nil
}

func (r *accountRegistry) findAccountByCode(ctx context.Context, code string) (*finance.Account, error) {
	// List accounts and filter by code
	page := commerce.Page{Limit: 100, Offset: 0}
	for {
		accounts, err := r.financeSvc.ListAccounts(ctx, page)
		if err != nil {
			return nil, err
		}
		if len(accounts) == 0 {
			break
		}
		for _, acc := range accounts {
			if acc.AccountCode == code {
				accountCopy := acc
				return &accountCopy, nil
			}
		}
		if len(accounts) < page.Limit {
			break
		}
		page.Offset += page.Limit
	}
	return nil, fmt.Errorf("%w: account_code %s", finance.ErrAccountNotFound, code)
}
