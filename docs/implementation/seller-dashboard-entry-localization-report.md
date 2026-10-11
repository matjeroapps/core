# Seller Dashboard Entry, Currency, and Arabic Localization — Core Implementation Report

Feature: `specs/033-seller-dashboard-entry-localization/` (tasks T014, T017, T020, T026, T028–T030; validation evidence for T035).

## Summary

This report covers the Core repository changes for feature 033. Core owns two halves of the
feature:

1. **Merchant first-store creation invariant (US5).** A merchant-linked retail seller may create a
   store only while it has zero store rows of **any** status. The check is enforced inside the
   creation transaction under the existing seller ownership lock, so concurrent second-create
   attempts cannot both pass. Existing store rows, their statuses, and their authorizations are
   never modified by a rejected attempt. First-store creation and non-merchant/supplier sellers are
   unaffected.
2. **Finance authoritative currency and error behavior (US3).** A balance read/account failure now
   returns the existing Core error contract instead of a fabricated `200` zero/`SAR` payload. A
   genuine zero balance is labeled with the ledger account's own currency.

Both behaviors are covered by focused tests that were executed and passed (see Validation).

## Changes

### Merchant first-store invariant (T026, T029, T030)

- New domain sentinel `ErrMerchantStoreAlreadyExists = errors.New("merchant store already exists")`
  in `core/modules/commerce/errors.go:22`.
- Enforcement helper `lockMerchantSellerWithoutStores` in `core/modules/commerce/repository.go:280`:
  - `SELECT merchant_id FROM sellers WHERE id = $1 FOR UPDATE` takes the existing seller
    ownership/serialization lock and asserts the seller is merchant-linked; a null merchant link
    returns `ErrInvalidInput`.
  - `SELECT COUNT(*) FROM stores WHERE seller_id = $1` counts **every** status (no status filter), so
    `draft`, `active`, and `inactive` rows all block a new store. Pre-existing "historical" rows are
    legacy store rows and are counted the same way (there is no separate `historical` status value in
    the store status vocabulary).
  - `count > 0` returns `ErrMerchantStoreAlreadyExists`; `count == 0` lets first-store creation
    proceed via `createStoreInTx`.
- Called at the top of the transaction in both merchant first-store primitives:
  `CreateFirstStoreForMerchantSeller` (`repository.go:232`) and
  `CreateFirstStoreWithDomainForMerchantSeller` (`repository.go:261`).
- Because rejection happens inside `r.withTx` before `createStoreInTx`, a rejected attempt issues no
  `INSERT`/`UPDATE`/`DELETE`: existing store rows/statuses and seller authorizations are untouched.
- Service routing preserved in `core/modules/commerce/service.go` (`CreateStoreForSubject`,
  `CreateStoreForMerchantSubject`) with `SellerHasMerchantLink` / `EnsureRetailSellerForMerchant` in
  `core/modules/commerce/platform_repository.go`; non-merchant sellers keep the unlimited
  `CreateStore` path.
- Error mapping in `core/internal/coreapi/errors.go:247-249` folds `ErrMerchantStoreAlreadyExists`
  onto the existing entitlement code:
  - code `CodeStoreEntitlementExceeded = "store_entitlement_exceeded"` (`errors.go:53`)
  - HTTP `409 Conflict`
  - message `"active-store entitlement limit exceeded"`

### Finance authoritative currency and error behavior (T014, T017, T020)

- `core/internal/coreapi/finance.go` (`handleGetStoreBalance`): the error branch now calls
  `writeDomainError(w, err)` and returns. The previous `200` fallback body with
  `Currency: "SAR"` and zero totals was removed; only the success path writes
  `StoreBalanceResponse`.
- Zero-balance currency is authoritative from the ledger account: `core/internal/balance/repository.go`
  `GetAccountBalance` falls back to `SELECT currency FROM ledger_accounts WHERE id = $1` and returns
  zero totals with that currency; if the ledger account is also absent it returns
  `ErrAccountNotFound`. The handler maps projection fields verbatim, so a real zero is labeled with
  the account currency (for example `EGP`) and never a default.
- Response contract `StoreBalanceResponse`: `available_minor`, `pending_minor`, `currency`,
  `updated_at` (`core/internal/coreapi/contracts.go`).
- Error contract via `writeDomainError` → `codeFor` → `writeError` → `httpx.WriteError`:
  - `balance.ErrAccountNotFound` → HTTP `404` / code `not_found` / `"resource not found"`
  - unexpected read failure → HTTP `500` / code `internal_error` / `"internal error"` (cause not leaked)
- Schema documentation in `core/internal/coreapi/spec.go`:
  - balance route now documents `StoreBalanceResponse` (was `AccountBalanceResponse`)
  - payouts collection now documents `CollectionResponse[PayoutResponse]` (was
    `CollectionResponse[SettlementResponse]`)
  - `readResponses` documents `401`/`403`/`404`/`500` error envelopes for the finance routes
- Generated contract refreshed: `core/docs/api/internal/openapi.json` (`available_minor` /
  `pending_minor` / `currency` / `updated_at` required on the balance response).

### Repository files changed (Core working tree, uncommitted)

- `core/docs/api/internal/openapi.json` (regenerated)
- `core/internal/coreapi/errors.go`
- `core/internal/coreapi/finance.go`
- `core/internal/coreapi/merchant_store_test.go`
- `core/internal/coreapi/spec.go`
- `core/internal/coreapi/spec_test.go`
- `core/modules/commerce/errors.go`
- `core/modules/commerce/platform_repository.go`
- `core/modules/commerce/repository.go`
- `core/modules/commerce/service.go`
- `core/modules/commerce/store_lifecycle_integration_test.go`
- `core/internal/coreapi/finance_test.go` (new, untracked)

## Validation

All commands below were executed in this workspace. Results are reported verbatim.

### Full Core suite

```bash
cd core && go test ./...
```

Result: **partial failure, isolated to one package.**

```
--- FAIL: TestBackfillDryRunApplyAndIdempotency (0.00s)
--- FAIL: TestBackfillQuarantinePersistsInvalidStatus (0.00s)
--- FAIL: TestBackfillQuarantinesOwnerMismatch (0.00s)
--- FAIL: TestMigration000036CardinalityConstraint (0.00s)
FAIL    core/cmd/merchant-backfill    0.008s
```

Every failure is `TEST_DATABASE_URL is required for database-backed Feature 022 validation`.
`TEST_DATABASE_URL` is unset in this environment. All other packages reported `ok` / `[no test
files]`, including `core/internal/coreapi`, `core/modules/commerce`, and
`core/internal/balance`. This failure is pre-existing environment configuration for the
`merchant-backfill` CLI, not a feature 033 regression.

### Focused commerce/coreapi suite (fresh, no cache)

```bash
cd core && go test -count=1 ./modules/commerce/... ./internal/coreapi/...
```

Result: `ok core/modules/commerce 219.546s` and `ok core/internal/coreapi 110.798s`; exit 0.

### Focused feature tests (verbose, run vs skip confirmed)

```bash
cd core && go test -count=1 -v -run 'TestMerchantStoreCreationRejectsAnyExistingStoreStatus|TestMerchantStoreCreationConcurrentSecondAttemptCannotPass|TestMerchantStoreCreationPreservesExistingMultipleStoresAndAuthorization' ./modules/commerce/
cd core && go test -count=1 -v -run 'TestMerchantStoreCreateSecondStoreReturnsStoreEntitlementConflict|TestSellerStoreCreateForMerchantLinkedSellerRejectsConcurrentSecondStore|TestGetStoreBalance' ./internal/coreapi/
```

Results (all `--- PASS`):

- `TestMerchantStoreCreationRejectsAnyExistingStoreStatus` — sub-tests `draft`, `active`, `inactive`
  each rejected with `ErrMerchantStoreAlreadyExists`
- `TestMerchantStoreCreationPreservesExistingMultipleStoresAndAuthorization` — pre-existing multiple
  stores retained, draft store B still `draft`, authorizations intact
- `TestMerchantStoreCreationConcurrentSecondAttemptCannotPass` — two concurrent creates: exactly one
  success and one rejection
- `TestSupplierStoreCreationIsNotLimitedByMerchantTotalStoreRule` — supplier path unaffected
- `TestMerchantStoreCreateSecondStoreReturnsStoreEntitlementConflict` — HTTP `409`, code
  `store_entitlement_exceeded`
- `TestSellerStoreCreateForMerchantLinkedSellerRejectsConcurrentSecondStore` — concurrent direct
  requests: one success, one HTTP `409`
- `TestGetStoreBalanceUsesAuthoritativeBalanceCurrencyForRealZero` — HTTP `200`,
  `available_minor == 0`, `currency == "EGP"` (authoritative), `updated_at` preserved
- `TestGetStoreBalanceReturnsErrorContractForUnresolvedAccount` — HTTP `404`, code `not_found`
- `TestGetStoreBalanceReturnsErrorContractForReadFailure` — HTTP `500`, code `internal_error`,
  sanitized message

The DB-backed tests used a reachable local PostgreSQL and **ran to PASS** — they were not skipped
(`--- PASS` lines were observed, not `--- SKIP`).

### Checks not run (Core)

- Browser/container acceptance (`cd platform-infra && make test-seller-live`) — **attempted; blocked
  by a missing-browser environment**. The exact command `cd platform-infra && make test-seller-live`
  was executed. Playwright started 6 tests, and all 6 failed before test execution because Chromium
  was missing at
  `/home/ahmed/.cache/ms-playwright/chromium_headless_shell-1234/chrome-headless-shell-linux64/chrome-headless-shell`;
  Playwright suggested `npx playwright install`. No test body or assertion ran.
- `TEST_DATABASE_URL`-backed `core/cmd/merchant-backfill` Feature 022 tests — blocked by the unset
  variable.

## Known Limitations

- `cd core && go test ./...` cannot be fully green here: `core/cmd/merchant-backfill` fails because
  `TEST_DATABASE_URL` is unset. This is an environment gap, not a feature 033 change.
- The specification/quickstart describes a "historical" store state. The actual store status
  vocabulary is `draft` / `active` / `inactive`; "historical" refers to pre-existing legacy rows,
  which the unfiltered `COUNT(*)` counts like any other row.
- Browser/container and live acceptance for the SC matrix (`specs/033-seller-dashboard-entry-localization/quickstart.md`)
  was attempted and did not run to any assertion: `cd platform-infra && make test-seller-live` was
  executed, Playwright started 6 tests, and all 6 failed before test execution because Chromium was
  missing at
  `/home/ahmed/.cache/ms-playwright/chromium_headless_shell-1234/chrome-headless-shell-linux64/chrome-headless-shell`
  (Playwright suggested `npx playwright install`). This is an environment blocker, not an assertion
  failure, so SC-001–SC-008 are not confirmed by Core.
- `specs/033-seller-dashboard-entry-localization/tasks.md` checkboxes T033, T034, and T036 are checked;
  T035 is now eligible to be checked because all applicable unit/integration suites, Seller Web
  typecheck/build, locale scanner, and the browser command were executed, with browser acceptance
  blocked before assertions by the missing Chromium executable and that blocker documented. The
  Seller-side report lives at
  `seller/docs/implementation/seller-dashboard-entry-localization-report.md`.
- Changes are uncommitted in the Core working tree on branch `main`; no commit or PR was created.
