package main

import (
	"testing"

	"github.com/google/uuid"
)

func TestBackfillMetricsAccountingIdentity(t *testing.T) {
	validMetrics := BackfillMetrics{
		TotalScanned:        100,
		MappedProfiles:      85,
		QuarantinedProfiles: 10,
		DeferredProfiles:    5,
	}

	if err := validMetrics.AssertIdentityEquation(); err != nil {
		t.Fatalf("expected valid metrics to pass identity equation, got %v", err)
	}

	invalidMetrics := BackfillMetrics{
		TotalScanned:        100,
		MappedProfiles:      80,
		QuarantinedProfiles: 10,
		DeferredProfiles:    5,
	}

	if err := invalidMetrics.AssertIdentityEquation(); err == nil {
		t.Fatalf("expected invalid metrics to fail identity equation")
	}
}

func TestBackfillDryRunClassifiesStandaloneAndAffiliation(t *testing.T) {
	sellerID := uuid.New()
	supplierID := uuid.New()
	standaloneSellerID := uuid.New()
	sellers := map[uuid.UUID]profile{
		sellerID:           {ID: sellerID, Code: "seller-a", Name: "Seller A", Status: "active"},
		standaloneSellerID: {ID: standaloneSellerID, Code: "seller-b", Name: "Seller B", Status: "active"},
	}
	suppliers := map[uuid.UUID]profile{
		supplierID: {ID: supplierID, Code: "supplier-a", Name: "Supplier A", Status: "active"},
	}

	items := classify(
		sellers,
		suppliers,
		[]affiliation{{SupplierID: supplierID, SellerID: sellerID}},
		map[uuid.UUID][]string{
			sellerID:           {"acct:shared-owner"},
			standaloneSellerID: {"acct:seller-owner"},
		},
		map[uuid.UUID][]string{supplierID: {"acct:shared-owner"}},
	)
	report := &BackfillReport{DryRun: true, BatchSize: 100}
	for _, item := range items {
		classifyOnly(item, report)
	}

	if report.Metrics.TotalScanned != 3 {
		t.Fatalf("total scanned = %d, want 3", report.Metrics.TotalScanned)
	}
	if report.Metrics.MappedProfiles != 3 {
		t.Fatalf("mapped profiles = %d, want 3", report.Metrics.MappedProfiles)
	}
	if report.Metrics.QuarantinedProfiles != 0 {
		t.Fatalf("quarantined profiles = %d, want 0", report.Metrics.QuarantinedProfiles)
	}
	if err := report.Metrics.AssertIdentityEquation(); err != nil {
		t.Fatalf("identity equation failed: %v", err)
	}
}

func TestBackfillQuarantinesInvalidStatus(t *testing.T) {
	sellerID := uuid.New()
	items := classify(map[uuid.UUID]profile{
		sellerID: {ID: sellerID, Code: "seller-a", Name: "Seller A", Status: "deleted"},
	}, nil, nil, nil, nil)
	report := &BackfillReport{DryRun: true, BatchSize: 100}
	classifyOnly(items[0], report)

	if report.Metrics.QuarantinedProfiles != 1 {
		t.Fatalf("quarantined profiles = %d, want 1", report.Metrics.QuarantinedProfiles)
	}
	if got := report.AuditEntries[0].ReasonCode; got != "INVALID_PROFILE_STATUS" {
		t.Fatalf("reason = %q, want INVALID_PROFILE_STATUS", got)
	}
}

func TestBackfillQuarantinesAffiliationOwnerMismatch(t *testing.T) {
	sellerID := uuid.New()
	supplierID := uuid.New()
	items := classify(
		map[uuid.UUID]profile{
			sellerID: {ID: sellerID, Code: "seller-a", Name: "Seller A", Status: "active"},
		},
		map[uuid.UUID]profile{
			supplierID: {ID: supplierID, Code: "supplier-a", Name: "Supplier A", Status: "active"},
		},
		[]affiliation{{SupplierID: supplierID, SellerID: sellerID}},
		map[uuid.UUID][]string{sellerID: {"acct:seller-owner"}},
		map[uuid.UUID][]string{supplierID: {"acct:supplier-owner"}},
	)
	report := &BackfillReport{DryRun: true, BatchSize: 100}
	classifyOnly(items[0], report)

	if report.Metrics.QuarantinedProfiles != 2 {
		t.Fatalf("quarantined profiles = %d, want 2", report.Metrics.QuarantinedProfiles)
	}
	if got := report.AuditEntries[0].ReasonCode; got != "OWNER_SUBJECT_MISMATCH" {
		t.Fatalf("reason = %q, want OWNER_SUBJECT_MISMATCH", got)
	}
}
