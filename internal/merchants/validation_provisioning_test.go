package merchants

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"core/internal/testdb"
)

func TestValidationProvisionerRequiresEnabledLocalRuntime(t *testing.T) {
	req := ValidationProvisionRequest{
		LocalOnly:      true,
		Issuer:         "http://localhost:8081",
		IdempotencyKey: "feature-026-test",
		Actors:         validationActorSubjects(),
	}

	if _, err := NewValidationProvisioner(nil, false, "matjero.com").ProvisionValidationScenario(context.Background(), req); !errors.Is(err, ErrValidationProvisioningDisabled) {
		t.Fatalf("disabled provisioner error = %v, want %v", err, ErrValidationProvisioningDisabled)
	}

	req.LocalOnly = false
	if _, err := NewValidationProvisioner(nil, true, "matjero.com").ProvisionValidationScenario(context.Background(), req); !errors.Is(err, ErrValidationProvisioningUnsafe) {
		t.Fatalf("non-local request error = %v, want %v", err, ErrValidationProvisioningUnsafe)
	}

	req.LocalOnly = true
	req.Issuer = "https://auth.matjero.com"
	if _, err := NewValidationProvisioner(nil, true, "matjero.com").ProvisionValidationScenario(context.Background(), req); !errors.Is(err, ErrValidationProvisioningUnsafe) {
		t.Fatalf("non-local issuer error = %v, want %v", err, ErrValidationProvisioningUnsafe)
	}
}

func TestValidationProvisionerRequiresAllActorSubjects(t *testing.T) {
	req := ValidationProvisionRequest{
		LocalOnly:      true,
		Issuer:         "http://localhost:8081",
		IdempotencyKey: "feature-026-test",
		Actors:         validationActorSubjects(),
	}
	delete(req.Actors, "dual_owner")

	if _, err := NewValidationProvisioner(nil, true, "matjero.com").ProvisionValidationScenario(context.Background(), req); !errors.Is(err, ErrValidationProvisioningInvalid) {
		t.Fatalf("missing actor error = %v, want %v", err, ErrValidationProvisioningInvalid)
	}
}

func TestValidationProvisionerRequiresStorefrontDomain(t *testing.T) {
	req := ValidationProvisionRequest{
		LocalOnly:      true,
		Issuer:         "http://localhost:8081",
		IdempotencyKey: "feature-026-test",
		Actors:         validationActorSubjects(),
	}

	if _, err := NewValidationProvisioner(nil, true, "").ProvisionValidationScenario(context.Background(), req); err == nil || !strings.Contains(err.Error(), "missing storefront platform domain") {
		t.Fatalf("missing storefront domain error = %v, want missing storefront platform domain", err)
	}
}

func TestLocalIssuerAcceptsOnlyLocalZitadelOrigins(t *testing.T) {
	accepted := []string{
		"http://localhost:8081",
		"http://127.0.0.1:8081",
		"http://zitadel:8080",
	}
	for _, issuer := range accepted {
		if !localIssuer(issuer) {
			t.Fatalf("localIssuer(%q) = false, want true", issuer)
		}
	}

	rejected := []string{
		"",
		"localhost:8081",
		"https://auth.matjero.com",
	}
	for _, issuer := range rejected {
		if localIssuer(issuer) {
			t.Fatalf("localIssuer(%q) = true, want false", issuer)
		}
	}
}

func TestValidationProvisionerCreatesScenarioIdempotently(t *testing.T) {
	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("TEST_DATABASE_URL is required for database-backed validation provisioning tests")
	}
	db := testdb.Open(t, dbURL)
	paths := []string{
		"000001_event_delivery_foundation",
		"000002_market_reference_data",
		"000003_commerce_domain_schema",
		"000005_store_domain_lifecycle",
		"000006_store_domain_integrity",
		"000009_supplier_retail_capability",
		"000018_supplier_retail_affiliation",
		"000027_integration_foundation",
		"000028_supplier_integrations",
		"000029_seller_integrations",
		"000034_merchant_identity_foundation",
		"000035_seller_supplier_merchant_linkage",
		"000036_merchant_profile_cardinality",
		"000037_merchant_owned_integration_connections",
		"000038_merchant_integration_external_account_lifecycle",
		"000039_integration_supply_staging",
		"000040_merchant_membership_subject_index",
	}
	migrations := make([]string, 0, len(paths))
	for _, name := range paths {
		migrations = append(migrations, filepath.Join("..", "..", "migrations", name+".up.sql"))
	}
	testdb.ApplyMigrations(t, db, migrations...)

	req := ValidationProvisionRequest{
		LocalOnly:      true,
		Issuer:         "http://localhost:8081",
		IdempotencyKey: "feature-026-test",
		Actors:         validationActorSubjects(),
	}
	provisioner := NewValidationProvisioner(db.Pool, true, "matjero.com")

	first, err := provisioner.ProvisionValidationScenario(context.Background(), req)
	if err != nil {
		t.Fatalf("first provision: %v", err)
	}
	second, err := provisioner.ProvisionValidationScenario(context.Background(), req)
	if err != nil {
		t.Fatalf("second provision: %v", err)
	}

	if first.Merchants["dual_merchant"].ID != second.Merchants["dual_merchant"].ID {
		t.Fatalf("dual merchant was not idempotent: %s then %s", first.Merchants["dual_merchant"].ID, second.Merchants["dual_merchant"].ID)
	}
	if first.Supply["review_case"] != second.Supply["review_case"] {
		t.Fatalf("review case was not idempotent: %s then %s", first.Supply["review_case"], second.Supply["review_case"])
	}

	if first.Stores["dual_store_a"].Host != "f026-store-a.matjero.com" {
		t.Fatalf("dual_store_a storefront host = %q, want f026-store-a.matjero.com", first.Stores["dual_store_a"].Host)
	}
	if first.Stores["dual_store_b"].Host != "f026-store-b.matjero.com" {
		t.Fatalf("dual_store_b storefront host = %q, want f026-store-b.matjero.com", first.Stores["dual_store_b"].Host)
	}

	var domainRows int
	if err := db.QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM store_domains
		WHERE domain_type = 'platform'
			AND status = 'active'
			AND is_primary
			AND verified_at IS NOT NULL
			AND (domain = $1 AND store_id = $2)
			OR (domain = $3 AND store_id = $4)
	`, first.Stores["dual_store_a"].Host, first.Stores["dual_store_a"].ID,
		first.Stores["dual_store_b"].Host, first.Stores["dual_store_b"].ID).Scan(&domainRows); err != nil {
		t.Fatalf("count storefront fixture domains: %v", err)
	}
	if domainRows != 2 {
		t.Fatalf("storefront fixture domain rows = %d, want 2", domainRows)
	}

	var reviewCases int
	if err := db.QueryRow(context.Background(), `
		SELECT COUNT(*)
		FROM merchant_integration_review_cases
		WHERE reason_code = 'FEATURE_026_REVIEW'
	`).Scan(&reviewCases); err != nil {
		t.Fatalf("count review cases: %v", err)
	}
	if reviewCases != 1 {
		t.Fatalf("review case count = %d, want 1", reviewCases)
	}
}

func validationActorSubjects() map[string]string {
	actors := make(map[string]string, len(validationActorLabels))
	for _, label := range validationActorLabels {
		actors[label] = "subject-" + label
	}
	return actors
}
