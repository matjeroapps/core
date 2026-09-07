# Phase 12 Marketplace Financial Rules Foundation

## Summary
Phase 12 introduces the **Marketplace Financial Rules Foundation** module into `matjeroapps/core`. Built directly on top of Payments Foundation (Phase 7), Financial Ledger (Phase 8), Accounting Rules (Phase 9), Balance Projection (Phase 10), and Settlement Calculation (Phase 11), this phase introduces configurable financial allocation rules that define how commerce revenue is distributed between platform participants: Sellers, Suppliers, and Platform.

Phase 12 only calculates settlement revenue allocations and defines financial rules. It does not implement payouts, withdrawals, bank transfers, payment provider settlement, or external financial integrations.

## Architecture Changes
The financial processing pipeline in MatjerHub is expanded as follows:

```
Payment
   │
Accounting Rules
   │
Financial Ledger
   │
Balance Projection
   │
Settlement Calculation (Phase 11)
   │
Marketplace Financial Rules (Phase 12)
```

- **Domain Isolation**: `internal/marketplace_finance` owns configurable financial rules, allocation calculation logic, database persistence, and outbox event notification.
- **Ledger & Settlement Alignment**: Allocation calculation operates strictly on immutable `Settlement` snapshots produced in Phase 11. No calculations bypass the accounting layer or calculate directly from raw orders.
- **Immutable Allocations**: Settlement allocations for finalized settlements are protected at both the application level and the database level (via PostgreSQL triggers). Once finalized, allocations cannot be altered or deleted.

## Repository Impact
- **New Domain Package**: `internal/marketplace_finance/`
  - `rules.go`: Domain models (`FinancialRule`, `SettlementAllocation`), types (`RuleType`: `PERCENTAGE`, `FIXED_AMOUNT`; `RuleStatus`: `ACTIVE`, `INACTIVE`; `AllocationType`: `SELLER_SHARE`, `SUPPLIER_SHARE`, `PLATFORM_SHARE`), and validation rules.
  - `calculator.go`: Allocation calculation engine evaluating percentage and fixed amount rules against settlement snapshots.
  - `repository.go`: Database operations with transactional support (`pgx.Tx`).
  - `service.go`: Service layer orchestrating rule creation, allocation calculation, and outbox event enqueueing.
  - `errors.go`: Domain error definitions mapped to internal API error responses.
  - Unit and integration tests (`rules_test.go`, `calculator_test.go`, `integration_test.go`).
- **Events Package**: `packages/events/marketplace_finance.go` & `marketplace_finance_test.go`
  - Event type `marketplace_finance.allocation.calculated.v1` wrapped in standard `EventEnvelope`.
- **Internal API**: `internal/coreapi/`
  - Added financial rules and allocations routes to `router.go`, DTOs to `contracts.go`, handlers to `marketplace_finance.go`, domain error mappings to `errors.go`, and OpenAPI specs to `spec.go`.
- **App Main Wiring**: `apps/core-api/main.go`
  - Wired `MarketplaceFinance` service into internal API dependencies.

## Database Changes
New migration: `migrations/000023_create_marketplace_financial_rules_schema.up.sql` (and `.down.sql`).

- **`financial_rules` Table**:
  - `id` (UUID PRIMARY KEY)
  - `name` (TEXT NOT NULL)
  - `rule_type` (TEXT NOT NULL CHECK (rule_type IN ('PERCENTAGE', 'FIXED_AMOUNT')))
  - `percentage` (NUMERIC(5, 2) NOT NULL DEFAULT 0.00 CHECK (percentage >= 0 AND percentage <= 100.00))
  - `fixed_amount_minor` (BIGINT NOT NULL DEFAULT 0 CHECK (fixed_amount_minor >= 0))
  - `currency` (CHAR(3) NOT NULL)
  - `allocation_type` (TEXT NOT NULL CHECK (allocation_type IN ('SELLER_SHARE', 'SUPPLIER_SHARE', 'PLATFORM_SHARE')))
  - `status` (TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE', 'INACTIVE')))
  - `created_at`, `updated_at` (TIMESTAMPTZ)
- **`settlement_allocations` Table**:
  - `id` (UUID PRIMARY KEY)
  - `settlement_id` (UUID NOT NULL REFERENCES settlements(id) ON DELETE CASCADE)
  - `account_id` (UUID NOT NULL REFERENCES ledger_accounts(id))
  - `allocation_type` (TEXT NOT NULL CHECK (allocation_type IN ('SELLER_SHARE', 'SUPPLIER_SHARE', 'PLATFORM_SHARE')))
  - `amount_minor` (BIGINT NOT NULL CHECK (amount_minor >= 0))
  - `currency` (CHAR(3) NOT NULL)
  - `created_at` (TIMESTAMPTZ)
  - `CONSTRAINT settlement_allocations_settlement_type_uidx UNIQUE (settlement_id, allocation_type)`
- **Database Immutability**:
  - PL/pgSQL function `prevent_finalized_settlement_allocation_modification()` and trigger `trg_immutable_finalized_settlement_allocations` preventing `UPDATE` or `DELETE` on allocations associated with finalized settlements.

## Financial Rules
1. **Rule Types**:
   - `PERCENTAGE`: Calculated as `round(gross_amount_minor * percentage / 100.0)`.
   - `FIXED_AMOUNT`: Minor-unit fixed deduction.
2. **Rule Status**:
   - `ACTIVE`: Evaluated during allocation calculation.
   - `INACTIVE`: Ignored during calculation.
3. **Allocation Calculation Invariant**:
   - `sum(Allocation.AmountMinor) == Settlement.GrossAmountMinor`.
   - Seller share automatically receives remaining balance after non-seller allocations (platform/supplier shares) are computed, unless explicitly overridden by a valid seller share rule.
   - Negative allocation amounts, currency mismatches, and invalid percentages are rejected.

## Allocation Model
- **Allocation Types**:
  - `SELLER_SHARE`
  - `SUPPLIER_SHARE`
  - `PLATFORM_SHARE`
- **Fields**: `id`, `settlement_id`, `account_id`, `allocation_type`, `amount_minor`, `currency`, `created_at`.

## Event Flow
When calculation executes, outbox events are enqueued within the database transaction:
- **`marketplace_finance.allocation.calculated.v1`**:
  Emitted for each generated settlement allocation. Includes `settlement_id`, `allocation_id`, `account_id`, `allocation_type`, `amount_minor`, `currency`, `correlation_id`, and `calculated_at`.

## Security Considerations
- **Internal API Scoping**: Financial rules and allocation endpoints are mounted under `/internal/v1` behind `service-auth` middleware requiring authorized caller credentials (`CallerAdmin`, `CallerSeller`).
- **No External Exposure**: Financial allocation endpoints are not exposed to public storefronts.
- **Auditability**: Every event payload contains `correlation_id` and `causationID` fields for end-to-end tracing across service boundaries.
- **DB Trigger Protection**: Allocations for finalized settlements are enforced immutable at PostgreSQL level.

## Testing

### Commands Executed & Results
1. `go test -v ./packages/events/...`
   - **Result**: PASS
2. `go test -v ./internal/marketplace_finance/...`
   - **Result**: PASS
3. `go test -v ./internal/coreapi/...`
   - **Result**: PASS
4. `gofmt -s -w .`
   - **Result**: Formatted clean
5. `go vet ./...`
   - **Result**: Clean, no warnings
6. `go test ./...`
   - **Result**: PASS across all packages
7. `go run ./cmd/openapi-gen`
   - **Result**: Updated `docs/api/internal/openapi.json` cleanly

## Files Changed
- `migrations/000023_create_marketplace_financial_rules_schema.up.sql`
- `migrations/000023_create_marketplace_financial_rules_schema.down.sql`
- `packages/events/marketplace_finance.go`
- `packages/events/marketplace_finance_test.go`
- `internal/marketplace_finance/rules.go`
- `internal/marketplace_finance/errors.go`
- `internal/marketplace_finance/calculator.go`
- `internal/marketplace_finance/repository.go`
- `internal/marketplace_finance/service.go`
- `internal/marketplace_finance/rules_test.go`
- `internal/marketplace_finance/calculator_test.go`
- `internal/marketplace_finance/integration_test.go`
- `internal/coreapi/contracts.go`
- `internal/coreapi/errors.go`
- `internal/coreapi/marketplace_finance.go`
- `internal/coreapi/marketplace_finance_test.go`
- `internal/coreapi/router.go`
- `internal/coreapi/spec.go`
- `apps/core-api/main.go`
- `docs/api/internal/openapi.json`
- `docs/implementation/phase-12-marketplace-financial-rules-foundation-report.md`

## Known Limitations
- Payout execution, bank transfers, payment provider settlement, and wallet withdrawals are explicitly out of scope for Phase 12.
- Rules currently apply globally per currency; custom seller/supplier tier rule overrides will be built in subsequent phases.

## Final Verification Status
- **Build**: PASS
- **Unit Tests**: PASS
- **Integration Tests**: PASS
- **OpenAPI Spec Generation**: PASS
- **Database Immutability**: VERIFIED
