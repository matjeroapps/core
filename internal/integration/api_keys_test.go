package integration

import (
	"testing"
)

func TestAPIKeyGenerationAndScopeCheck(t *testing.T) {
	raw, prefix, hash, err := GenerateRawAPIKey(false)
	if err != nil {
		t.Fatalf("GenerateRawAPIKey failed: %v", err)
	}

	if len(raw) == 0 {
		t.Error("raw key is empty")
	}
	if prefix == "" || len(prefix) != 14 { // "mj_test_" (8) + 6 hex chars = 14
		t.Errorf("prefix = %q (len %d), want prefix of len 14", prefix, len(prefix))
	}
	if len(hash) != 64 { // SHA256 hex is 64 chars
		t.Errorf("hash len = %d, want 64", len(hash))
	}

	// Test Scope Check
	scopes := []string{ScopeProductsRead, ScopeOrdersWrite}
	if !HasScope(scopes, ScopeProductsRead) {
		t.Error("expected HasScope to return true for read:products")
	}
	if !HasScope(scopes, ScopeOrdersWrite) {
		t.Error("expected HasScope to return true for write:orders")
	}
	if HasScope(scopes, ScopeProductsWrite) {
		t.Error("expected HasScope to return false for write:products")
	}

	// Test full_access scope
	adminScopes := []string{ScopeFullAccess}
	if !HasScope(adminScopes, ScopeProductsWrite) {
		t.Error("full_access scope should allow any permission")
	}
}
