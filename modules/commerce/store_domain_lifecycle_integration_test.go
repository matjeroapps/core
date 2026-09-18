package commerce

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"core/internal/testdb"
	"core/packages/database"
)

type BlockingTXTResolver struct {
	enterLookup chan struct{}
	release     chan struct{}
	records     []string
	err         error
}

func (b *BlockingTXTResolver) LookupTXT(ctx context.Context, name string) ([]string, error) {
	if b.enterLookup != nil {
		b.enterLookup <- struct{}{}
	}
	if b.release != nil {
		<-b.release
	}
	if b.err != nil {
		return nil, b.err
	}
	return b.records, nil
}

func openTestDB(t *testing.T) (*database.Pool, Repository, Service) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://commerce:commerce@localhost:5432/commerce?sslmode=disable"
	}
	db := testdb.Open(t, dsn)
	applyStoreDomainMigrations(t, db)
	repo := NewRepository(db.Pool)
	svc := NewService(repo)
	svc.PlatformDomain = "matjero.com"
	return db, repo, svc
}

func TestStoreDomainLifecycleIntegration(t *testing.T) {
	_, repo, svc := openTestDB(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	// 1. Setup Seller A and Store A
	sellerA, err := repo.CreateSeller(ctx, "seller-a-"+suffix, "Seller A", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller A: %v", err)
	}
	subA := "subject-seller-a-" + suffix
	if _, err := repo.CreateSellerMember(ctx, sellerA.ID, subA, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember A: %v", err)
	}

	storeA, err := svc.CreateStoreForSubject(ctx, subA, sellerA.ID, "EG", "storea-"+suffix, "Store A", "active", nil)
	if err != nil {
		t.Fatalf("CreateStoreForSubject A: %v", err)
	}

	// 2. Setup Seller B and Store B
	sellerB, err := repo.CreateSeller(ctx, "seller-b-"+suffix, "Seller B", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller B: %v", err)
	}
	subB := "subject-seller-b-" + suffix
	if _, err := repo.CreateSellerMember(ctx, sellerB.ID, subB, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember B: %v", err)
	}

	storeB, err := svc.CreateStoreForSubject(ctx, subB, sellerB.ID, "EG", "storeb-"+suffix, "Store B", "active", nil)
	if err != nil {
		t.Fatalf("CreateStoreForSubject B: %v", err)
	}

	// Verify Store A has active primary platform domain
	primaryA, err := repo.GetActivePrimaryStoreDomain(ctx, storeA.ID)
	if err != nil {
		t.Fatalf("GetActivePrimaryStoreDomain A: %v", err)
	}
	if primaryA.DomainType != "platform" || !primaryA.IsPrimary || primaryA.Status != "active" {
		t.Fatalf("unexpected primary domain for store A: %+v", primaryA)
	}

	// 3. Authorization Tests (Seller A / Seller B isolation)
	t.Run("Seller isolation", func(t *testing.T) {
		// Seller B trying to list Store A domains -> ErrNotFound
		_, err := svc.ListStoreDomainsForSubject(ctx, subB, storeA.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound for cross-seller store list, got %v", err)
		}

		// Seller B trying to request custom domain for Store A -> ErrNotFound
		_, err = svc.RequestCustomStoreDomain(ctx, subB, storeA.ID, "hacked.com")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound for cross-seller domain request, got %v", err)
		}
	})

	// 4. Request Custom Domain Tests
	t.Run("Custom domain request validation", func(t *testing.T) {
		// Invalid custom domains
		invalids := []string{
			"matjero.com",     // claiming platform domain
			"sub.matjero.com", // claiming platform subdomain
			"http://shop.com", // scheme
			"127.0.0.1",       // IP
			"localhost",       // single label
			"bad_label.com",   // underscore
			"*.wildcard.com",  // wildcard
			"-start.com",      // hyphen start
		}
		for _, inv := range invalids {
			_, err := svc.RequestCustomStoreDomain(ctx, subA, storeA.ID, inv)
			if !errors.Is(err, ErrInvalidInput) {
				t.Errorf("RequestCustomStoreDomain(%q) expected ErrInvalidInput, got %v", inv, err)
			}
		}

		// Valid custom domain
		cd1, err := svc.RequestCustomStoreDomain(ctx, subA, storeA.ID, "  Shop.BrandA-"+suffix+".COM  ")
		if err != nil {
			t.Fatalf("RequestCustomStoreDomain valid: %v", err)
		}
		expectedDomain := fmt.Sprintf("shop.branda-%s.com", suffix)
		if cd1.Domain != expectedDomain {
			t.Fatalf("domain not canonicalized: got %q want %q", cd1.Domain, expectedDomain)
		}
		if cd1.Status != "pending" || cd1.DomainType != "custom" || cd1.IsPrimary {
			t.Fatalf("unexpected state for custom domain: %+v", cd1)
		}
		if cd1.VerificationToken == nil || *cd1.VerificationToken == "" {
			t.Fatal("verification token not generated")
		}

		// Duplicate domain across stores -> ErrConflict
		_, err = svc.RequestCustomStoreDomain(ctx, subB, storeB.ID, expectedDomain)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("expected ErrConflict for duplicate custom domain, got %v", err)
		}
	})

	// 5. DNS Verification Tests
	t.Run("DNS verification state machine", func(t *testing.T) {
		cd, err := svc.RequestCustomStoreDomain(ctx, subA, storeA.ID, "verify-me-"+suffix+".com")
		if err != nil {
			t.Fatalf("RequestCustomStoreDomain: %v", err)
		}

		recName := FormatTXTRecordName(cd.Domain)
		token := *cd.VerificationToken

		// Setup test resolver
		fakeResolver := &FakeTXTResolver{
			Records: make(map[string][]string),
		}
		testSvc := svc
		testSvc.TXTResolver = fakeResolver

		// Scenario A: Temporary DNS error / timeout -> ErrUnavailable, DB state unchanged (pending)
		fakeResolver.Err = &net.DNSError{Err: "i/o timeout", IsTimeout: true}
		_, err = testSvc.VerifyCustomStoreDomainForSubject(ctx, subA, storeA.ID, cd.ID)
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("expected ErrUnavailable on DNS timeout, got %v", err)
		}
		domainCheck, _ := repo.GetStoreDomain(ctx, cd.ID)
		if domainCheck.Status != "pending" {
			t.Fatalf("status changed on DNS timeout: %s", domainCheck.Status)
		}

		// Scenario B: Authoritative no record / TXT mismatch -> failed
		fakeResolver.Err = nil
		fakeResolver.Records[recName] = []string{"matjero-verification=wrongtoken"}
		vFailed, err := testSvc.VerifyCustomStoreDomainForSubject(ctx, subA, storeA.ID, cd.ID)
		if err != nil {
			t.Fatalf("VerifyCustomStoreDomainForSubject: %v", err)
		}
		if vFailed.Status != "failed" || vFailed.LastCheckedAt == nil {
			t.Fatalf("expected failed status, got %+v", vFailed)
		}

		// Scenario C: Retry after failed with correct TXT record -> verified
		fakeResolver.Records[recName] = []string{
			"unrelated=txt",
			FormatTXTRecordValue(token),
		}
		vSuccess, err := testSvc.VerifyCustomStoreDomainForSubject(ctx, subA, storeA.ID, cd.ID)
		if err != nil {
			t.Fatalf("VerifyCustomStoreDomainForSubject retry: %v", err)
		}
		if vSuccess.Status != "verified" || vSuccess.VerifiedAt == nil || vSuccess.LastCheckedAt == nil {
			t.Fatalf("expected verified status, got %+v", vSuccess)
		}

		// Scenario D: Re-checking verified domain is idempotent / no-op
		vRepeat, err := testSvc.VerifyCustomStoreDomainForSubject(ctx, subA, storeA.ID, cd.ID)
		if err != nil {
			t.Fatalf("repeat verify: %v", err)
		}
		if vRepeat.Status != "verified" {
			t.Fatalf("status changed on repeat verify: %s", vRepeat.Status)
		}
	})

	// 6. Custom Domain Activation & Primary Invariant
	t.Run("Custom domain activation and primary switching", func(t *testing.T) {
		cd, err := svc.RequestCustomStoreDomain(ctx, subA, storeA.ID, "active-custom-"+suffix+".com")
		if err != nil {
			t.Fatalf("RequestCustomStoreDomain: %v", err)
		}

		// Cannot activate PENDING domain
		_, err = svc.ActivateCustomStoreDomainForSubject(ctx, subA, storeA.ID, cd.ID)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("expected ErrConflict activating pending domain, got %v", err)
		}

		// Force verify domain
		recName := FormatTXTRecordName(cd.Domain)
		fakeResolver := &FakeTXTResolver{
			Records: map[string][]string{
				recName: {FormatTXTRecordValue(*cd.VerificationToken)},
			},
		}
		testSvc := svc
		testSvc.TXTResolver = fakeResolver
		vDomain, err := testSvc.VerifyCustomStoreDomainForSubject(ctx, subA, storeA.ID, cd.ID)
		if err != nil || vDomain.Status != "verified" {
			t.Fatalf("verify domain: %v, domain=%+v", err, vDomain)
		}

		// Activate custom domain
		actDomain, err := testSvc.ActivateCustomStoreDomainForSubject(ctx, subA, storeA.ID, cd.ID)
		if err != nil {
			t.Fatalf("ActivateCustomStoreDomainForSubject: %v", err)
		}
		if actDomain.Status != "active" || !actDomain.IsPrimary {
			t.Fatalf("expected custom domain active primary, got %+v", actDomain)
		}

		// Check primary domain in DB for Store A -> returns custom domain
		primaryNow, err := repo.GetActivePrimaryStoreDomain(ctx, storeA.ID)
		if err != nil {
			t.Fatalf("GetActivePrimaryStoreDomain: %v", err)
		}
		if primaryNow.ID != cd.ID {
			t.Fatalf("primary domain ID = %s, want %s", primaryNow.ID, cd.ID)
		}

		// Verify platform domain is still ACTIVE but non-primary
		platformDomain, err := repo.GetStoreDomain(ctx, primaryA.ID)
		if err != nil {
			t.Fatalf("GetStoreDomain platform: %v", err)
		}
		if platformDomain.Status != "active" || platformDomain.IsPrimary {
			t.Fatalf("platform domain should be active secondary, got %+v", platformDomain)
		}

		// Verify exactly one primary domain exists for Store A
		domains, err := repo.ListStoreDomains(ctx, storeA.ID)
		if err != nil {
			t.Fatalf("ListStoreDomains: %v", err)
		}
		primaryCount := 0
		for _, d := range domains {
			if d.IsPrimary {
				primaryCount++
			}
		}
		if primaryCount != 1 {
			t.Fatalf("expected exactly 1 primary domain, found %d", primaryCount)
		}
	})

	// 7. Admin Moderation: Disable and Re-enable
	t.Run("Admin disable fallback and re-enable flow", func(t *testing.T) {
		// Store A currently has custom primary and platform active secondary.
		activeCustom, err := repo.GetActivePrimaryStoreDomain(ctx, storeA.ID)
		if err != nil {
			t.Fatalf("GetActivePrimaryStoreDomain: %v", err)
		}
		if activeCustom.DomainType != "custom" {
			t.Fatalf("expected custom primary, got %s", activeCustom.DomainType)
		}

		// Admin disables custom primary domain
		disabledCustom, err := svc.AdminDisableDomain(ctx, activeCustom.ID)
		if err != nil {
			t.Fatalf("AdminDisableDomain: %v", err)
		}
		if disabledCustom.Status != "disabled" || disabledCustom.IsPrimary {
			t.Fatalf("disabled custom domain should be disabled non-primary: %+v", disabledCustom)
		}

		// Check platform domain fallback -> automatically promoted to primary!
		fallbackPrimary, err := repo.GetActivePrimaryStoreDomain(ctx, storeA.ID)
		if err != nil {
			t.Fatalf("GetActivePrimaryStoreDomain fallback: %v", err)
		}
		if fallbackPrimary.DomainType != "platform" || !fallbackPrimary.IsPrimary {
			t.Fatalf("expected platform domain promoted to primary, got %+v", fallbackPrimary)
		}

		// Admin re-enables custom domain -> becomes 'verified' non-primary (NOT active!)
		reEnabledCustom, err := svc.AdminEnableDomain(ctx, activeCustom.ID)
		if err != nil {
			t.Fatalf("AdminEnableDomain: %v", err)
		}
		if reEnabledCustom.Status != "verified" || reEnabledCustom.IsPrimary {
			t.Fatalf("re-enabled custom domain should be verified non-primary, got %+v", reEnabledCustom)
		}

		// StorefrontHost discovery still returns platform primary
		currentPrimary, err := repo.GetActivePrimaryStoreDomain(ctx, storeA.ID)
		if err != nil {
			t.Fatalf("GetActivePrimaryStoreDomain: %v", err)
		}
		if currentPrimary.DomainType != "platform" {
			t.Fatalf("primary should still be platform, got %s", currentPrimary.DomainType)
		}

		// Seller activates custom domain again -> becomes active primary
		reActivated, err := svc.ActivateCustomStoreDomainForSubject(ctx, subA, storeA.ID, activeCustom.ID)
		if err != nil {
			t.Fatalf("ActivateCustomStoreDomainForSubject: %v", err)
		}
		if reActivated.Status != "active" || !reActivated.IsPrimary {
			t.Fatalf("reactivated custom domain should be active primary: %+v", reActivated)
		}
	})

	// 8. Admin Moderation: Platform Disable and Enable
	t.Run("Platform domain enable fallback", func(t *testing.T) {
		// Store B has platform domain only.
		platformB, err := repo.GetActivePrimaryStoreDomain(ctx, storeB.ID)
		if err != nil {
			t.Fatalf("GetActivePrimaryStoreDomain B: %v", err)
		}

		// Disable Store B platform domain -> Store B has no primary domain
		_, err = svc.AdminDisableDomain(ctx, platformB.ID)
		if err != nil {
			t.Fatalf("AdminDisableDomain platform: %v", err)
		}

		_, err = repo.GetActivePrimaryStoreDomain(ctx, storeB.ID)
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound when store has no primary domain, got %v", err)
		}

		// Admin re-enables Store B platform domain -> becomes active primary because store has no primary
		reEnabledB, err := svc.AdminEnableDomain(ctx, platformB.ID)
		if err != nil {
			t.Fatalf("AdminEnableDomain platform: %v", err)
		}
		if reEnabledB.Status != "active" || !reEnabledB.IsPrimary {
			t.Fatalf("re-enabled platform domain should be active primary, got %+v", reEnabledB)
		}
	})

	// 9. Concurrency Test
	t.Run("Concurrent domain activation safety", func(t *testing.T) {
		// Create 2 verified custom domains for Store B
		cd1, err := repo.CreateStoreDomain(ctx, storeB.ID, "c1-"+suffix+".com", "custom", "verified", false, nil, nil)
		if err != nil {
			t.Fatalf("create custom 1: %v", err)
		}
		cd2, err := repo.CreateStoreDomain(ctx, storeB.ID, "c2-"+suffix+".com", "custom", "verified", false, nil, nil)
		if err != nil {
			t.Fatalf("create custom 2: %v", err)
		}

		var wg sync.WaitGroup
		errs := make(chan error, 2)

		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := svc.ActivateCustomStoreDomainForSubject(ctx, subB, storeB.ID, cd1.ID)
			if err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			_, err := svc.ActivateCustomStoreDomainForSubject(ctx, subB, storeB.ID, cd2.ID)
			if err != nil {
				errs <- err
			}
		}()
		wg.Wait()
		close(errs)

		for err := range errs {
			t.Logf("concurrent activation info: %v", err)
		}

		// Assert exactly 1 primary domain for Store B
		domains, err := repo.ListStoreDomains(ctx, storeB.ID)
		if err != nil {
			t.Fatalf("ListStoreDomains B: %v", err)
		}
		primaryCount := 0
		for _, d := range domains {
			if d.IsPrimary {
				primaryCount++
			}
		}
		if primaryCount != 1 {
			t.Fatalf("concurrent activation resulted in %d primary domains, want 1", primaryCount)
		}
	})
}

func TestDisabledCustomDomainCannotBeVerifiedBySeller(t *testing.T) {
	_, repo, svc := openTestDB(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	seller, err := repo.CreateSeller(ctx, "seller-dis-"+suffix, "Seller Dis", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	sub := "sub-dis-" + suffix
	if _, err := repo.CreateSellerMember(ctx, seller.ID, sub, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	store, err := svc.CreateStoreForSubject(ctx, sub, seller.ID, "EG", "store-dis-"+suffix, "Store Dis", "active", nil)
	if err != nil {
		t.Fatalf("CreateStoreForSubject: %v", err)
	}

	cd, err := svc.RequestCustomStoreDomain(ctx, sub, store.ID, "disabled-verify-"+suffix+".com")
	if err != nil {
		t.Fatalf("RequestCustomStoreDomain: %v", err)
	}

	recName := FormatTXTRecordName(cd.Domain)
	fakeResolver := &FakeTXTResolver{
		Records: map[string][]string{
			recName: {FormatTXTRecordValue(*cd.VerificationToken)},
		},
	}
	svc.TXTResolver = fakeResolver

	// Admin disables domain
	disabledDomain, err := svc.AdminDisableDomain(ctx, cd.ID)
	if err != nil {
		t.Fatalf("AdminDisableDomain: %v", err)
	}
	if disabledDomain.Status != "disabled" {
		t.Fatalf("status = %s, want disabled", disabledDomain.Status)
	}

	// Seller calls /verify -> MUST return ErrConflict
	_, err = svc.VerifyCustomStoreDomainForSubject(ctx, sub, store.ID, cd.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict when Seller calls verify on disabled domain, got %v", err)
	}

	// Domain remains disabled in DB
	checkDomain, err := repo.GetStoreDomain(ctx, cd.ID)
	if err != nil {
		t.Fatalf("GetStoreDomain: %v", err)
	}
	if checkDomain.Status != "disabled" || checkDomain.IsPrimary {
		t.Fatalf("domain should remain disabled non-primary: %+v", checkDomain)
	}

	// Seller calls /activate -> MUST return ErrConflict
	_, err = svc.ActivateCustomStoreDomainForSubject(ctx, sub, store.ID, cd.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict when Seller calls activate on disabled domain, got %v", err)
	}
}

func TestDNSVerificationRaceConditionWithAdminDisable(t *testing.T) {
	_, repo, svc := openTestDB(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	seller, err := repo.CreateSeller(ctx, "seller-race-"+suffix, "Seller Race", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	sub := "sub-race-" + suffix
	if _, err := repo.CreateSellerMember(ctx, seller.ID, sub, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	store, err := svc.CreateStoreForSubject(ctx, sub, seller.ID, "EG", "store-race-"+suffix, "Store Race", "active", nil)
	if err != nil {
		t.Fatalf("CreateStoreForSubject: %v", err)
	}

	cd, err := svc.RequestCustomStoreDomain(ctx, sub, store.ID, "race-verify-"+suffix+".com")
	if err != nil {
		t.Fatalf("RequestCustomStoreDomain: %v", err)
	}

	blockingResolver := &BlockingTXTResolver{
		enterLookup: make(chan struct{}, 1),
		release:     make(chan struct{}, 1),
		records:     []string{FormatTXTRecordValue(*cd.VerificationToken)},
	}
	svc.TXTResolver = blockingResolver

	var verifyErr error
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		_, verifyErr = svc.VerifyCustomStoreDomainForSubject(ctx, sub, store.ID, cd.ID)
	}()

	// Wait until Seller verification enters DNS lookup (has read domain as pending)
	<-blockingResolver.enterLookup

	// Admin disables domain while DNS lookup is in-flight
	disabledDomain, err := svc.AdminDisableDomain(ctx, cd.ID)
	if err != nil {
		t.Fatalf("AdminDisableDomain during race: %v", err)
	}
	if disabledDomain.Status != "disabled" {
		t.Fatalf("AdminDisableDomain status = %s", disabledDomain.Status)
	}

	// Release DNS lookup with matching TXT record
	blockingResolver.release <- struct{}{}

	wg.Wait()

	// Seller verification MUST return ErrConflict (conditional SQL UPDATE failed)
	if !errors.Is(verifyErr, ErrConflict) {
		t.Fatalf("expected ErrConflict from in-flight Seller verify after Admin disable, got %v", verifyErr)
	}

	// Final DB state MUST remain disabled (NOT verified, active, or failed)
	finalDomain, err := repo.GetStoreDomain(ctx, cd.ID)
	if err != nil {
		t.Fatalf("GetStoreDomain: %v", err)
	}
	if finalDomain.Status != "disabled" {
		t.Fatalf("final domain status = %s, want disabled", finalDomain.Status)
	}
}

func TestAdminEnablePreconditions(t *testing.T) {
	_, repo, svc := openTestDB(t)
	ctx := context.Background()
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())

	seller, err := repo.CreateSeller(ctx, "seller-enable-"+suffix, "Seller Enable", "active", nil)
	if err != nil {
		t.Fatalf("CreateSeller: %v", err)
	}
	sub := "sub-enable-" + suffix
	if _, err := repo.CreateSellerMember(ctx, seller.ID, sub, "owner", "active"); err != nil {
		t.Fatalf("CreateSellerMember: %v", err)
	}
	store, err := svc.CreateStoreForSubject(ctx, sub, seller.ID, "EG", "store-enable-"+suffix, "Store Enable", "active", nil)
	if err != nil {
		t.Fatalf("CreateStoreForSubject: %v", err)
	}

	platformPrimary, err := repo.GetActivePrimaryStoreDomain(ctx, store.ID)
	if err != nil {
		t.Fatalf("GetActivePrimaryStoreDomain: %v", err)
	}

	// 1. Calling Enable on ACTIVE platform domain -> ErrConflict
	_, err = svc.AdminEnableDomain(ctx, platformPrimary.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict calling Enable on active platform domain, got %v", err)
	}

	// 2. Calling Enable on PENDING custom domain -> ErrConflict
	cdPending, err := svc.RequestCustomStoreDomain(ctx, sub, store.ID, "pending-"+suffix+".com")
	if err != nil {
		t.Fatalf("RequestCustomStoreDomain: %v", err)
	}
	_, err = svc.AdminEnableDomain(ctx, cdPending.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict calling Enable on pending custom domain, got %v", err)
	}

	// 3. Calling Enable on VERIFIED custom domain -> ErrConflict
	recName := FormatTXTRecordName(cdPending.Domain)
	fakeResolver := &FakeTXTResolver{
		Records: map[string][]string{
			recName: {FormatTXTRecordValue(*cdPending.VerificationToken)},
		},
	}
	testSvc := svc
	testSvc.TXTResolver = fakeResolver
	cdVerified, err := testSvc.VerifyCustomStoreDomainForSubject(ctx, sub, store.ID, cdPending.ID)
	if err != nil || cdVerified.Status != "verified" {
		t.Fatalf("VerifyCustomStoreDomainForSubject: %v", err)
	}
	_, err = svc.AdminEnableDomain(ctx, cdVerified.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict calling Enable on verified custom domain, got %v", err)
	}

	// 4. Calling Enable on ACTIVE custom primary domain -> ErrConflict
	cdActive, err := testSvc.ActivateCustomStoreDomainForSubject(ctx, sub, store.ID, cdVerified.ID)
	if err != nil || cdActive.Status != "active" {
		t.Fatalf("ActivateCustomStoreDomainForSubject: %v", err)
	}
	_, err = svc.AdminEnableDomain(ctx, cdActive.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict calling Enable on active custom domain, got %v", err)
	}

	// 5. Admin disables custom primary domain -> disabled
	disabledCustom, err := svc.AdminDisableDomain(ctx, cdActive.ID)
	if err != nil || disabledCustom.Status != "disabled" {
		t.Fatalf("AdminDisableDomain: %v", err)
	}

	// 6. Admin enables disabled custom domain (verified_at != nil) -> verified non-primary
	reEnabledCustom, err := svc.AdminEnableDomain(ctx, disabledCustom.ID)
	if err != nil {
		t.Fatalf("AdminEnableDomain on disabled custom: %v", err)
	}
	if reEnabledCustom.Status != "verified" || reEnabledCustom.IsPrimary {
		t.Fatalf("re-enabled custom domain should be verified non-primary, got %+v", reEnabledCustom)
	}
}
