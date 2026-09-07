# Phase 10 Balance Projection Foundation

## Summary

Phase 10 implements the financial balance projection foundation on top of MatjerHub Core's existing double-entry immutable ledger. By decoupling balance querying from real-time historical ledger scans (`SUM(journal_lines)`), this module provides fast, O(1) balance lookups while preserving full transactional integrity, sign conventions, and idempotent event processing. Additionally, it establishes a minimal settlement foundation (`SettlementPeriod`) to support future payout and settlement workflows without introducing premature financial logic.

## Architecture Changes

```
Ledger Events (ledger.journal_entry.posted.v1)
                    ↓
Balance Projection Consumer (inbox deduplicated within DB tx)
                    ↓
       Account Balances Table (UPSERT)
                    ↓
    Internal Balance Query API (/internal/v1/balances/accounts)
                    ↓
      Settlement Foundation (SettlementPeriod)
```

- **Core Ownership**: Core remains the authoritative owner of financial balance projections and invariants.
- **Ledger Immutability**: Existing journal entries and lines remain strictly immutable and read-only.
- **Asynchronous Projection**: Balances are derived asynchronously from posted journal entry events without modifying historical ledger records.

## Repository Impact

- `internal/balance/`: New domain package managing account balance projections, idempotent event consumption, and settlement period entities.
- `internal/coreapi/`: Extended internal HTTP API with routes `/internal/v1/balances/accounts/{accountID}` and `/internal/v1/balances/accounts`, DTO definitions, error mapping, and OpenAPI specification.
- `migrations/`: Added `000021_create_balance_projection_schema.up.sql` and `000021_create_balance_projection_schema.down.sql`.
- `docs/api/internal/openapi.json`: Updated OpenAPI documentation for internal balance projection endpoints.

## Database Changes

Added database migration `000021_create_balance_projection_schema.up.sql`:

1. `account_balances`:
   - `id`: `UUID PRIMARY KEY`
   - `account_id`: `UUID NOT NULL REFERENCES ledger_accounts(id) ON DELETE RESTRICT`
   - `currency`: `CHAR(3) NOT NULL`
   - `debit_total_minor`: `BIGINT NOT NULL DEFAULT 0 CHECK (debit_total_minor >= 0)`
   - `credit_total_minor`: `BIGINT NOT NULL DEFAULT 0 CHECK (credit_total_minor >= 0)`
   - `balance_minor`: `BIGINT NOT NULL DEFAULT 0`
   - `updated_at`: `TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()`
   - `CONSTRAINT account_balances_account_currency_uidx`: `UNIQUE (account_id, currency)`

2. `settlement_periods`:
   - `id`: `UUID PRIMARY KEY`
   - `start_date`: `TIMESTAMPTZ NOT NULL`
   - `end_date`: `TIMESTAMPTZ NOT NULL`
   - `status`: `TEXT NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN', 'CLOSED'))`
   - `created_at`: `TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()`
   - `closed_at`: `TIMESTAMPTZ NULL`

## Balance Calculation Rules

Accounting Sign Convention:
```
balance_minor = debit_total_minor - credit_total_minor
```

For every journal line in `ledger.journal_entry.posted.v1`:
- **Debit Entry of amount D**:
  - `debit_total_minor` += D
  - `balance_minor` += D
- **Credit Entry of amount C**:
  - `credit_total_minor` += C
  - `balance_minor` -= C

Atomic DB Update Query:
```sql
INSERT INTO account_balances (
    id, account_id, currency, debit_total_minor, credit_total_minor, balance_minor, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp())
ON CONFLICT (account_id, currency) DO UPDATE SET
    debit_total_minor = account_balances.debit_total_minor + EXCLUDED.debit_total_minor,
    credit_total_minor = account_balances.credit_total_minor + EXCLUDED.credit_total_minor,
    balance_minor = account_balances.balance_minor + EXCLUDED.balance_minor,
    updated_at = clock_timestamp();
```

## Event Flow

1. Journal Entry is posted to `journal_entries` and `journal_lines`.
2. Outbox event `ledger.journal_entry.posted.v1` is enqueued into `outbox_events`.
3. Event worker delivers envelope to `balance_projection_consumer`.
4. Consumer opens a database transaction `tx`.
5. Consumer records event execution in `processed_events` inbox table (`consumer_name = 'balance_projection_consumer'`).
   - If event ID was previously processed, `inbox.RecordProcessed` returns `false`, transaction commits, and processing returns idempotently without side effects.
6. For each line in payload, consumer executes `UpsertAccountBalanceTx`.
7. Transaction commits atomically. Any database error causes `tx.Rollback`, reverting inbox entry and balance changes for safe retry.

## Settlement Foundation

Minimal domain entities introduced:
- `SettlementPeriod` struct with statuses `OPEN` and `CLOSED`.
- Domain operations: `CreateSettlementPeriod`, `GetSettlementPeriod`, `CloseSettlementPeriod`, `ListSettlementPeriods`.
- **Note**: No payouts, money movement, commission calculation, or external bank transfers are implemented in this phase.

## Security Considerations

- **Access Control**: `/internal/v1/balances/...` endpoints require service-auth authentication restricted to authorized callers (`CallerSeller`, `CallerAdmin`).
- **Ledger Invariants**: Direct user balance mutations are prohibited. Balances can only be updated through system consumer processing of valid `ledger.journal_entry.posted.v1` events.
- **Immutability**: Immutable triggers on `journal_entries` and `journal_lines` remain active.

## Testing

Commands executed:
```bash
go test -v ./internal/balance/...
go test -v ./internal/coreapi/...
go vet ./...
gofmt -s -w .
go run ./cmd/openapi-gen
```

Test coverage:
- **Unit Tests**: Domain validations, sign conventions, consumer envelope validation.
- **Integration Tests**: Postgres end-to-end ledger event handling, atomic balance calculations, duplicate event deduplication, rollback safety, settlement period operations.
- **Concurrency Tests**: Goroutines concurrently processing multiple ledger events verifying total balance accuracy without race conditions.

## Files Changed

- `migrations/000021_create_balance_projection_schema.up.sql` [NEW]
- `migrations/000021_create_balance_projection_schema.down.sql` [NEW]
- `internal/balance/balance.go` [NEW]
- `internal/balance/errors.go` [NEW]
- `internal/balance/repository.go` [NEW]
- `internal/balance/service.go` [NEW]
- `internal/balance/consumer.go` [NEW]
- `internal/balance/balance_test.go` [NEW]
- `internal/balance/consumer_test.go` [NEW]
- `internal/balance/integration_test.go` [NEW]
- `internal/coreapi/balances.go` [NEW]
- `internal/coreapi/contracts.go` [MODIFY]
- `internal/coreapi/errors.go` [MODIFY]
- `internal/coreapi/router.go` [MODIFY]
- `internal/coreapi/spec.go` [MODIFY]
- `internal/coreapi/integration_test.go` [MODIFY]
- `docs/api/internal/openapi.json` [MODIFY]
- `docs/implementation/phase-10-balance-projection-foundation-report.md` [NEW]

## Known Limitations

- Balance projections are per `(account_id, currency)`. Multi-currency aggregations across different currencies require explicit FX translation (out of scope for this phase).
- Settlement periods are purely domain containers; payout rules and balance locking per settlement period will be implemented in future settlement phases.

## Final Verification Status

PASSED: All unit, integration, concurrency, OpenAPI generation, and code formatting checks completed successfully.
