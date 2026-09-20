package main

import (
	"testing"

	"core/internal/serviceauth"
	"core/packages/config"
)

func TestServiceAuthConfigIncludesConfiguredCallerTokens(t *testing.T) {
	cfg := config.Config{
		InternalPlatformToken: "platform-token",
		InternalSellerToken:   "seller-token",
		InternalAdminToken:    "admin-token",
		InternalSupplierToken: "supplier-token",
	}

	authCfg := serviceAuthConfig(cfg)
	expectedTokens := map[serviceauth.Caller]string{
		serviceauth.CallerPlatform: "platform-token",
		serviceauth.CallerSeller:   "seller-token",
		serviceauth.CallerAdmin:    "admin-token",
		serviceauth.CallerSupplier: "supplier-token",
	}

	for caller, expectedToken := range expectedTokens {
		if got := authCfg.Tokens[caller]; got != expectedToken {
			t.Errorf("%s token = %q, want %q", caller, got, expectedToken)
		}
	}
}
