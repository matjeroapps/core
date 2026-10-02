package main

import (
	"testing"
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
