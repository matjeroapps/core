# Phase 8 Financial Ledger Foundation

## Summary

Phase 8 introduces an immutable double-entry accounting foundation inside Core (`matjeroapps/core`). This module provides the baseline financial ledger required to support payments accounting, refund accounting, seller balances, supplier settlements, marketplace commissions, and future reconciliation workflows.

In accordance with architectural directives, settlement calculations and payment provider integrations are intentionally omitted from this phase, establishing purely the reusable immutable double-entry ledger domain model, posting engine service, transactional outbox events, database schema migrations, and internal Core APIs.

---

## Architecture Changes

1. **Immutable Double-Entry Ledger Domain (`internal/finance`)**:
   - Implements `Account` aggregate (types: `ASSET`, `LIABILITY`, `REVENUE`, `EXPENSE`, `EQUITY`; statuses: `ACTIVE`, `INACTIVE`, `FROZEN`).
   - Implements `JournalEntry` and `JournalLine` entities for double-entry records.
   - Core domain invariants:
     - Every entry must balance: $\sum \text{Debit} = \sum \text{Credit}$.
     - Each line must contain either `debit > 0` or `credit > 0`, never both or neither.
     - Currency uniformity across entry and accounts is strictly enforced.
     - Account status must be `ACTIVE` to accept ledger lines.

2. **Database Level Immutability**:
   - PostgreSQL trigger `prevent_ledger_modification()` blocks `UPDATE` and `DELETE` queries on `journal_entries` and `journal_lines`.
   - Corrections must occur exclusively through new compensating journal entries.

3. **Transactional Outbox Event Pattern**:
   - Event `ledger.journal_entry.posted.v1` is enqueued into `outbox_events` within the same database transaction as the ledger record creation.
   - Outbox routing key mapping in `packages/outbox/outbox.go` maps `ledger.journal_entry.posted.v1` to exchange `ledger.events` and routing key `journal_entry.posted`.

4. **Internal API Exposure (`internal/coreapi`)**:
   - Internal Core endpoints under `/internal/v1/ledger` protected by service authorization (`serviceauth.RequireCaller(CallerSeller, CallerAdmin)`).

---

## Repository Impact

- `internal/finance/`: Domain models, repository, posting service, domain errors, unit and database integration tests.
- `packages/events/`: Added `ledger.go` with `JournalEntryPostedPayload` and `NewJournalEntryPostedEvent`.
- `packages/outbox/`: Updated `ResolveRoutingKey` to support `ledger.journal_entry.posted.v1`.
- `migrations/`: Added `000020_create_ledger_schema.up.sql` and `000020_create_ledger_schema.down.sql`.
- `internal/coreapi/`: Added `ledger.go`, updated `contracts.go`, `errors.go`, `router.go`, and `spec.go`.
- `docs/api/internal/openapi.json`: Updated OpenAPI specification via `go run ./cmd/openapi-gen`.

---

## Database Changes

### Migration: `000020_create_ledger_schema.up.sql`

1. `ledger_accounts` Table:
   - `id` (UUID, PK)
   - `account_code` (TEXT, UNIQUE, NOT NULL)
   - `name` (TEXT, NOT NULL)
   - `account_type` (TEXT, CHECK: `ASSET`, `LIABILITY`, `REVENUE`, `EXPENSE`, `EQUITY`)
   - `currency` (CHAR(3), NOT NULL)
   - `status` (TEXT, CHECK: `ACTIVE`, `INACTIVE`, `FROZEN`)
   - `created_at`, `updated_at` (TIMESTAMPTZ)
   - Indexes: `ledger_accounts_account_code_idx`, `ledger_accounts_status_idx`

2. `journal_entries` Table:
   - `id` (UUID, PK)
   - `reference_type` (TEXT, NOT NULL)
   - `reference_id` (TEXT, NOT NULL)
   - `description` (TEXT, NOT NULL)
   - `currency` (CHAR(3), NOT NULL)
   - `posted_at`, `created_at` (TIMESTAMPTZ)
   - Unique Constraint: `journal_entries_reference_uidx UNIQUE (reference_type, reference_id)` for duplicate posting protection.
   - Indexes: `journal_entries_reference_idx`, `journal_entries_posted_at_idx`

3. `journal_lines` Table:
   - `id` (UUID, PK)
   - `journal_entry_id` (UUID, FK -> `journal_entries.id`)
   - `account_id` (UUID, FK -> `ledger_accounts.id`)
   - `debit_amount_minor` (BIGINT, CHECK >= 0)
   - `credit_amount_minor` (BIGINT, CHECK >= 0)
   - `created_at` (TIMESTAMPTZ)
   - Check Constraint: `CHECK ((debit_amount_minor > 0 AND credit_amount_minor = 0) OR (credit_amount_minor > 0 AND debit_amount_minor = 0))`
   - Indexes: `journal_lines_entry_id_idx`, `journal_lines_account_id_idx`

4. Immutability Triggers:
   - `prevent_ledger_modification()` trigger function attached `BEFORE UPDATE OR DELETE ON journal_entries` and `ON journal_lines`.

---

## API Changes

Internal Core API under `/internal/v1`:

- `POST /internal/v1/ledger/accounts`: Creates a new ledger account.
- `GET /internal/v1/ledger/accounts`: Lists ledger accounts with pagination.
- `GET /internal/v1/ledger/accounts/{id}`: Retrieves account details by ID.
- `POST /internal/v1/ledger/journal-entries`: Posts a balanced double-entry journal entry and enqueues outbox event atomically.
- `GET /internal/v1/ledger/journal-entries/{id}`: Retrieves journal entry and lines by ID.

---

## Security Considerations

- Internal Endpoints are protected via `serviceauth.RequireCaller(CallerSeller, CallerAdmin)` requiring valid service bearer authentication tokens.
- Subject identity context (`X-Matjero-Subject`) is extracted for audit causation tracking (`CausationID`).
- All monetary amounts use 64-bit integer minor units (`BIGINT`) with ISO 4217 currency strings (`CHAR(3)`). Floating-point math is strictly prohibited.

---

## Testing

### Automated Test Commands and Results

1. **Unit and Domain Validation Tests**:
   - Command: `go test -v ./internal/finance/... ./packages/events/...`
   - Results: PASS
   - Verified unbalanced entry rejection (`debit != credit`), line amount rules (`debit > 0 XOR credit > 0`), currency mismatch rejection, inactive account rejection.

2. **Integration and Database Tests**:
   - Command: `go test -v ./internal/finance/...`
   - Results: PASS
   - Verified account creation, journal entry posting, lines persistence, outbox event enqueueing, duplicate posting prevention via reference unique constraint, and database immutability triggers (UPDATE/DELETE blocked).

3. **OpenAPI Generation**:
   - Command: `go run ./cmd/openapi-gen`
   - Results: PASS (Regenerated `docs/api/internal/openapi.json`).

4. **Repository Test Suite**:
   - Command: `go test ./...`
   - Results: PASS.

---

## Files Changed

- `migrations/000020_create_ledger_schema.up.sql` [NEW]
- `migrations/000020_create_ledger_schema.down.sql` [NEW]
- `packages/events/ledger.go` [NEW]
- `packages/events/ledger_test.go` [NEW]
- `packages/outbox/outbox.go` [MODIFY]
- `internal/finance/ledger.go` [NEW]
- `internal/finance/errors.go` [NEW]
- `internal/finance/repository.go` [NEW]
- `internal/finance/service.go` [NEW]
- `internal/finance/ledger_test.go` [NEW]
- `internal/finance/ledger_integration_test.go` [NEW]
- `internal/coreapi/ledger.go` [NEW]
- `internal/coreapi/contracts.go` [MODIFY]
- `internal/coreapi/errors.go` [MODIFY]
- `internal/coreapi/router.go` [MODIFY]
- `internal/coreapi/spec.go` [MODIFY]
- `docs/api/internal/openapi.json` [MODIFY]
- `docs/implementation/phase-08-financial-ledger-foundation-report.md` [NEW]

---

## Known Limitations

- Settlement calculations, provider-specific payout logic, marketplace commissions, and automatic posting on payment capture are intentionally out of scope for Phase 8 and reserved for future phases.

---

## Final Verification Status

- `gofmt -s -w .`: PASS
- `go vet ./...`: PASS
- `go test ./...`: PASS
- `go run ./cmd/openapi-gen`: PASS
