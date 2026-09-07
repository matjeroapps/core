# Phase 11 Settlement Calculation Foundation

## Summary
Phase 11 introduces the **Settlement Calculation Foundation** module into `matjeroapps/core`. Built directly on top of the Financial Ledger (Phase 8), Accounting Rules (Phase 9), and Balance Projection (Phase 10), this phase provides the domain logic, persistence, calculation engine, outbox event publishing, and internal HTTP APIs required to calculate settlement obligations for platform participants.

Crucially, Phase 11 calculates settlement amounts only. It does not implement payouts, bank transfers, payment provider settlement APIs, supplier payment execution, seller wallet withdrawal, or external financial integrations.

## Architecture Changes
The financial processing pipeline in MatjerHub is structured as follows:

```
Payment
   |
Accounting Rules
   |
Financial Ledger
   |
Balance Projection
   |
Settlement Calculation (Phase 11)
```

- **Domain Isolation**: `internal/settlement` owns settlement calculation rules, snapshot creation, state transitions, and snapshot persistence.
- **Ledger Independence**: Calculation derives gross and net obligations directly from ledger-derived balance projections (`balance.AccountBalance`), guaranteeing financial consistency and avoiding direct calculations from raw order data.
- **Immutable Snapshots**: Settlements in `FINALIZED` status are protected at both the application level and the database level (via PostgreSQL triggers). Once finalized, a settlement cannot be altered or deleted.

## Repository Impact
- **New Domain Package**: `internal/settlement/`
  - `settlement.go`: Domain models (`Settlement`, `SettlementPeriod`), status definitions (`PENDING`, `CALCULATED`, `FINALIZED`), and state transition validators.
  - `calculator.go`: Settlement calculation engine mapping account balance projections to minor-unit settlement records.
  - `repository.go`: Database operations with transactional support (`pgx.Tx`).
  - `service.go`: Service layer orchestrating calculations, period status transitions, and outbox event enqueueing.
  - `errors.go`: Domain error definitions mapped to internal API error responses.
  - Unit and integration tests (`settlement_test.go`, `calculator_test.go`, `integration_test.go`).
- **Events Package**: `packages/events/settlements.go` & `settlements_test.go`
  - Event types `settlement.calculated.v1` and `settlement.finalized.v1` wrapped in standard `EventEnvelope`.
- **Internal API**: `internal/coreapi/`
  - Added settlement routes to `router.go`, DTOs to `contracts.go`, handlers to `settlements.go`, and OpenAPI specs to `spec.go`.

## Database Changes
New migration: `migrations/000022_create_settlement_calculation_schema.up.sql` (and `.down.sql`).

- **`settlement_periods` Status Update**:
  Extended status check constraint to support `'OPEN'`, `'CALCULATED'`, `'FINALIZED'`, `'CLOSED'`.
- **`settlements` Table**:
  - `id` (UUID PRIMARY KEY)
  - `settlement_period_id` (UUID NOT NULL REFERENCES settlement_periods(id))
  - `account_id` (UUID NOT NULL REFERENCES ledger_accounts(id))
  - `currency` (CHAR(3) NOT NULL)
  - `gross_amount_minor` (BIGINT NOT NULL DEFAULT 0)
  - `adjustment_amount_minor` (BIGINT NOT NULL DEFAULT 0)
  - `net_amount_minor` (BIGINT NOT NULL DEFAULT 0)
  - `status` (TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'CALCULATED', 'FINALIZED')))
  - `created_at`, `calculated_at`, `finalized_at` (TIMESTAMPTZ)
  - `CONSTRAINT settlements_net_amount_check CHECK (net_amount_minor = gross_amount_minor + adjustment_amount_minor)`
  - `CONSTRAINT settlements_period_account_uidx UNIQUE (settlement_period_id, account_id)`
- **Database Immutability**:
  - PL/pgSQL function `prevent_finalized_settlement_modification()` and trigger `trg_immutable_finalized_settlements` preventing `UPDATE` or `DELETE` on finalized settlements.

## Settlement Rules
1. **Calculation Basis**:
   - `gross_amount_minor` = account balance projection (`balance_minor`).
   - `adjustment_amount_minor` = `0` (no commissions, fees, taxes, or marketplace splits in Phase 11).
   - `net_amount_minor` = `gross_amount_minor + adjustment_amount_minor`.
2. **State Machine**:
   - Settlement Record: `PENDING` → `CALCULATED` → `FINALIZED`
   - Settlement Period: `OPEN` → `CALCULATED` → `FINALIZED` (or `CLOSED`)
   - Finalized records and periods cannot regress to calculated/open states.

## Event Flow
When calculation or finalization executes, events are enqueued in the database outbox in the same transaction:
- **`settlement.calculated.v1`**:
  Emitted when calculation completes for a period. Includes `settlement_id`, `period_id`, `account_id`, `currency`, `gross_amount_minor`, `adjustment_amount_minor`, `net_amount_minor`, `status`, and `calculated_at`.
- **`settlement.finalized.v1`**:
  Emitted when settlements in a period are finalized. Includes `settlement_id`, `period_id`, `account_id`, `currency`, `net_amount_minor`, `status`, and `finalized_at`.

## Security Considerations
- **Internal API Scoping**: Settlement calculation routes are mounted under `/internal/v1` behind `service-auth` middleware and require authorized caller credentials (`CallerAdmin`, `CallerSeller`).
- **No External Exposure**: Settlement endpoints are not exposed to public storefronts or raw clients.
- **Auditability**: Every event payload contains `correlation_id` and `causationID` fields for end-to-end tracing across service boundaries.
- **DB Trigger Protection**: Finalized settlement records are enforced immutable at PostgreSQL level.

## Testing

### Commands Executed & Results
1. `go test -v ./packages/events/...`
   - **Result**: PASS (0.010s)
2. `go test -v ./internal/settlement/...`
   - **Result**: PASS (1.276s)
3. `gofmt -s -w .`
   - **Result**: Formatted clean
4. `go vet ./...`
   - **Result**: Clean, no warnings
5. `go test ./...`
   - **Result**: PASS across all packages
6. `go run ./cmd/openapi-gen`
   - **Result**: Updated `docs/api/internal/openapi.json` cleanly

## Files Changed
- `migrations/000022_create_settlement_calculation_schema.up.sql`
- `migrations/000022_create_settlement_calculation_schema.down.sql`
- `packages/events/settlements.go`
- `packages/events/settlements_test.go`
- `internal/settlement/settlement.go`
- `internal/settlement/errors.go`
- `internal/settlement/calculator.go`
- `internal/settlement/repository.go`
- `internal/settlement/service.go`
- `internal/settlement/settlement_test.go`
- `internal/settlement/calculator_test.go`
- `internal/settlement/integration_test.go`
- `internal/coreapi/contracts.go`
- `internal/coreapi/settlements.go`
- `internal/coreapi/errors.go`
- `internal/coreapi/router.go`
- `internal/coreapi/spec.go`
- `docs/api/internal/openapi.json`
- `docs/implementation/phase-11-settlement-calculation-foundation-report.md`

## Known Limitations
- Fee calculation, commission calculation, tax deductions, and marketplace splits are omitted per Phase 11 specification and will be added in subsequent business rule phases.
- Payout execution, bank transfers, and wallet balance withdrawals are explicitly out of scope for Phase 11.

## Final Verification Status
- **Build**: PASS
- **Unit Tests**: PASS
- **Integration Tests**: PASS
- **OpenAPI Spec Generation**: PASS
- **Database Immutability**: VERIFIED
