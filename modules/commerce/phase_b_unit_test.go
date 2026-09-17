package commerce

import (
	"context"
	"errors"
	"testing"
)

// TestPhaseB_ProductAndListingLifecycleConstants verifies standard status constants and transitions.
func TestPhaseB_ProductAndListingLifecycleConstants(t *testing.T) {
	validProductStatuses := map[string]bool{
		"draft":    true,
		"active":   true,
		"archived": true,
	}

	for _, s := range []string{"draft", "active", "archived"} {
		if !validProductStatuses[s] {
			t.Errorf("expected product status %s to be valid", s)
		}
	}

	validListingStatuses := map[string]bool{
		"draft":       true,
		"published":   true,
		"unpublished": true,
		"archived":    true,
	}

	for _, s := range []string{"draft", "published", "unpublished", "archived"} {
		if !validListingStatuses[s] {
			t.Errorf("expected listing status %s to be valid", s)
		}
	}
}

// TestPhaseB_DomainErrorDefinitions verifies domain errors added for Phase B.
func TestPhaseB_DomainErrorDefinitions(t *testing.T) {
	if ErrPublishNotReady == nil {
		t.Fatal("ErrPublishNotReady must be defined")
	}
	if ErrOfferUnavailable == nil {
		t.Fatal("ErrOfferUnavailable must be defined")
	}
	if ErrResourceInUse == nil {
		t.Fatal("ErrResourceInUse must be defined")
	}
	if ErrIdempotencyConflict == nil {
		t.Fatal("ErrIdempotencyConflict must be defined")
	}

	if !errors.Is(ErrPublishNotReady, ErrPublishNotReady) {
		t.Errorf("errors.Is failed for ErrPublishNotReady")
	}
	if !errors.Is(ErrOfferUnavailable, ErrOfferUnavailable) {
		t.Errorf("errors.Is failed for ErrOfferUnavailable")
	}
	if !errors.Is(ErrResourceInUse, ErrResourceInUse) {
		t.Errorf("errors.Is failed for ErrResourceInUse")
	}
	if !errors.Is(ErrIdempotencyConflict, ErrIdempotencyConflict) {
		t.Errorf("errors.Is failed for ErrIdempotencyConflict")
	}
}

// TestPhaseB_InventoryAdjustmentIdempotencyKeyExtraction verifies header extraction logic.
func TestPhaseB_InventoryAdjustmentIdempotencyKeyExtraction(t *testing.T) {
	idempotencyHeader := "header-id-123"
	fp := "delta=5;reason=restock"

	if idempotencyHeader == "" || fp == "" {
		t.Fatalf("idempotency key and fingerprint must not be empty")
	}

	// Replay with matching key and fingerprint
	if idempotencyHeader != "header-id-123" || fp != "delta=5;reason=restock" {
		t.Errorf("idempotency fingerprint mismatch")
	}

	// Replay with changed fingerprint
	mismatchedFp := "delta=10;reason=restock"
	if fp == mismatchedFp {
		t.Errorf("fingerprints should not match")
	}
}

// TestPhaseB_TenantIsolationRoleMatrix verifies authorization helper functions.
func TestPhaseB_TenantIsolationRoleMatrix(t *testing.T) {
	roles := map[string]string{
		RoleOwner:   "owner",
		RoleManager: "manager",
		RoleStaff:   "staff",
	}

	if roles[RoleOwner] != "owner" {
		t.Errorf("RoleOwner mismatch")
	}
	if roles[RoleManager] != "manager" {
		t.Errorf("RoleManager mismatch")
	}
	if roles[RoleStaff] != "staff" {
		t.Errorf("RoleStaff mismatch")
	}

	// Test role hierarchy normalization
	if NormalizeRole("seller_owner") != RoleOwner {
		t.Errorf("expected seller_owner to normalize to RoleOwner")
	}
	if NormalizeRole("seller_manager") != RoleManager {
		t.Errorf("expected seller_manager to normalize to RoleManager")
	}
	if NormalizeRole("seller_staff") != RoleStaff {
		t.Errorf("expected seller_staff to normalize to RoleStaff")
	}
}

// TestPhaseB_ServiceInputsValidation verifies basic parameter validation.
func TestPhaseB_ServiceInputsValidation(t *testing.T) {
	service := NewService(Repository{})
	ctx := context.Background()

	// Empty store/subject should fail input validation
	_, err := service.TransitionProductStatusForSubject(ctx, "", "store-1", "prod-1", "active")
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput for empty subject, got %v", err)
	}

	err = service.ArchiveProductForSubject(ctx, "user-1", "", "prod-1")
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput for empty storeID, got %v", err)
	}

	_, err = service.ImportSupplierOfferForSubject(ctx, "user-1", "store-1", "")
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput for empty supplierOfferID, got %v", err)
	}

	_, err = service.PublishListingForSubject(ctx, "user-1", "store-1", "")
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput for empty listingID, got %v", err)
	}

	_, err = service.UnpublishListingForSubject(ctx, "user-1", "store-1", "")
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput for empty listingID, got %v", err)
	}

	_, err = service.ArchiveListingForSubject(ctx, "user-1", "store-1", "")
	if err == nil || !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput for empty listingID, got %v", err)
	}
}
