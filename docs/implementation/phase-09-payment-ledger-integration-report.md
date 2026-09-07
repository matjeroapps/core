# Phase 9 Payment Ledger Integration Foundation

## Summary
Phase 9 implements the Payment Ledger Integration Foundation, establishing an immutable accounting bridge between payment lifecycle events (`payments.payment.captured.v1`, `payments.payment.failed.v1`) and the Financial Ledger double-entry posting engine. The integration resides in a dedicated module (`internal/accounting/`) that translates domain events into balanced, immutable ledger journal entries using double-layer idempotency (consumer inbox store + reference constraints) and strict atomic transaction boundaries.

## Architecture Changes
```
                  Payment Event
                        |
                        v
        +-------------------------------+
        |  internal/accounting/Consumer |
        +-------------------------------+
                        |
                        +---> 1. Inbox Deduplication (inbox.Store)
                        +---> 2. Rule Resolution (accounting.RuleEngine)
                        +---> 3. Account Provisioning (accounting.AccountRegistry)
                        |
                        v
        +-------------------------------+
        |  Financial Ledger Engine      |
        |  (finance.Service)            |
        +-------------------------------+
                        |
            +-----------+-----------+
            |                       |
            v                       v
     journal_entries          outbox_events
     + journal_lines     (ledger.journal_entry.posted.v1)
```

1. **Dedicated Accounting Module**: Created `internal/accounting/` containing account registry, rules engine, domain errors, and event consumer.
2. **Ledger Service Extension**: Extended `finance.Service` with `PostJournalEntryTx` and `GetJournalEntryByReferenceTx` methods to allow event consumers to execute inbox recording, ledger posting, and outbox event enqueueing within a single atomic PostgreSQL transaction block.
3. **Strict Separation of Concerns**: Accounting rules and ledger posting decisions remain strictly inside Core (`internal/accounting/`). Payment aggregates and state machines remain unmodified.

## Repository Impact
- **`internal/finance/`**: Extended `Service` interface and repository to support transactional posting (`PostJournalEntryTx`) and reference-based lookup (`GetJournalEntryByReference`, `GetJournalEntryByReferenceTx`).
- **`internal/accounting/`**: Introduced new integration module:
  - `accounts.go`: System default accounts foundation and account registry.
  - `rules.go`: Payment accounting rules engine.
  - `consumer.go`: Transactional event consumer and handler.
  - `errors.go`: Domain-specific error definitions.
  - `rules_test.go`, `accounts_test.go`, `consumer_test.go`, `integration_test.go`: Unit and integration test coverage.

## Database Changes
No schema migrations were required for Phase 9. The implementation reuses the existing, production-hardened schema:
- `ledger_accounts`: Stores system default accounts (`1010-PAYMENT-CLEARING-<CURRENCY>`, `2010-CUSTOMER-FUNDS-<CURRENCY>`, `4010-PLATFORM-REVENUE-<CURRENCY>`).
- `journal_entries`: Holds immutable journal entries, enforced by `journal_entries_reference_uidx` on `(reference_type, reference_id)`.
- `journal_lines`: Holds debit and credit line items, enforced by debit/credit balance triggers and immutability rules.
- `processed_events`: Consumed by `inbox.Store` for consumer-level message deduplication (`accounting_payment_consumer`, `event_id`).
- `outbox_events`: Stores transactional outbox events (`ledger.journal_entry.posted.v1`).

## Accounting Rules

### 1. System Default Accounts (Chart of Accounts)
- **PAYMENT_CLEARING** (`1010-PAYMENT-CLEARING`, `ASSET`): Clearing account holding captured funds before settlement.
- **CUSTOMER_FUNDS** (`2010-CUSTOMER-FUNDS`, `LIABILITY`): Customer funds liability account representing un-settled customer balances.
- **PLATFORM_REVENUE** (`4010-PLATFORM-REVENUE`, `REVENUE`): Platform revenue account for fees (reserved for future phases).

System accounts are provisioned idempotently per currency (e.g. `1010-PAYMENT-CLEARING-SAR`).

### 2. Payment Captured Rule
- **Event**: `payments.payment.captured.v1`
- **Reference Type**: `PAYMENT_CAPTURED`
- **Reference ID**: `payment_id`
- **Debit Line**: `PAYMENT_CLEARING` (Asset) equal to `amount_minor`
- **Credit Line**: `CUSTOMER_FUNDS` (Liability) equal to `amount_minor`
- **Description**: `"Payment captured for order <order_id>"`

### 3. Payment Failed Rule
- **Event**: `payments.payment.failed.v1`
- **Handling**: Resolved explicitly with `ShouldPost = false`. Event processing is logged and recorded in consumer inbox; no financial posting occurs.

## Event Flow
1. Payment event envelope (`payments.payment.captured.v1`) is received by `accounting.Consumer`.
2. Consumer opens a PostgreSQL transaction (`tx`).
3. Inbox store records `(accounting_payment_consumer, event_id)`. If already present, transaction commits and returns `nil` (idempotent skip).
4. Accounting rule engine validates payload and evaluates posting parameters.
5. Account registry resolves/provisions debit (`1010-PAYMENT-CLEARING-SAR`) and credit (`2010-CUSTOMER-FUNDS-SAR`) accounts.
6. Consumer checks if `(PAYMENT_CAPTURED, payment_id)` already exists in `journal_entries`. If present, transaction commits and returns `nil`.
7. `finance.Service.PostJournalEntryTx` posts the journal entry and lines, and enqueues `ledger.journal_entry.posted.v1` into `outbox_events`.
8. Transaction commits atomically. If any error occurs, the transaction rolls back completely (including inbox record), allowing safe retries.

## Security Considerations
- **Immutability Enforcement**: Database triggers (`trg_immutable_journal_entries`, `trg_immutable_journal_lines`) block UPDATE or DELETE operations on posted ledger entries.
- **Double-Layer Idempotency**: Prevents double-posting through both consumer inbox deduplication (`processed_events`) and unique reference index (`journal_entries_reference_uidx`).
- **Internal Authorization**: Accounting rules and ledger postings can only be triggered by internal system handlers; no direct client or public API bypass is permitted.
- **Traceability**: Correlation IDs and event Causation IDs are preserved across event payloads, outbox events, and journal entry records.

## Testing

### Commands and Results

#### Unit & Integration Tests:
```bash
go test -v ./internal/accounting/...
```
```
=== RUN   TestAccountRegistry
=== RUN   TestAccountRegistry/Ensure_System_Accounts_Idempotency
=== RUN   TestAccountRegistry/Get_Account_By_Role
=== RUN   TestAccountRegistry/Invalid_Currency_Rejection
--- PASS: TestAccountRegistry (0.00s)
=== RUN   TestConsumerHandleEvent
=== RUN   TestConsumerHandleEvent/Handle_Payment_Captured_Event_Success
=== RUN   TestConsumerHandleEvent/Duplicate_Inbox_Event_Skipped_Idempotently
=== RUN   TestConsumerHandleEvent/Duplicate_Ledger_Posting_Error_Treated_as_Idempotent_Success
=== RUN   TestConsumerHandleEvent/Handle_Payment_Failed_Event_No_Posting
=== RUN   TestConsumerHandleEvent/Invalid_Event_Envelope_Fails_Validation
--- PASS: TestConsumerHandleEvent (0.00s)
=== RUN   TestAccountingIntegration
=== RUN   TestAccountingIntegration/Payment_Captured_Event_End-to-End
=== RUN   TestAccountingIntegration/Duplicate_Event_Processing_Idempotency
=== RUN   TestAccountingIntegration/Payment_Failed_Event_Does_Not_Create_Journal_Entry
=== RUN   TestAccountingIntegration/Ledger_Failure_Rolls_Back_Inbox_Record_and_Allows_Safe_Retry
--- PASS: TestAccountingIntegration (0.90s)
=== RUN   TestPaymentAccountingRules
=== RUN   TestPaymentAccountingRules/Payment_Captured_Event_Rule
=== RUN   TestPaymentAccountingRules/Payment_Captured_Event_Rule_Invalid_Payload
=== RUN   TestPaymentAccountingRules/Payment_Failed_Event_Rule
=== RUN   TestPaymentAccountingRules/Unsupported_Event_Type
--- PASS: TestPaymentAccountingRules (0.00s)
PASS
ok  	github.com/matjeroapps/core/internal/accounting	0.902s
```

#### Full Test Suite:
```bash
go test ./...
```
```
ok  	github.com/matjeroapps/core/internal/accounting	0.910s
ok  	github.com/matjeroapps/core/internal/finance	0.958s
ok  	github.com/matjeroapps/core/internal/payments	(cached)
...
PASS
```

## Files Changed
- `internal/finance/repository.go`: Added `GetJournalEntryByReference` method.
- `internal/finance/service.go`: Added `PostJournalEntryTx`, `GetJournalEntryByReference`, and `GetJournalEntryByReferenceTx` methods.
- `internal/accounting/accounts.go`: System default accounts foundation and account registry.
- `internal/accounting/rules.go`: Payment accounting rules engine.
- `internal/accounting/consumer.go`: Event consumer and handler.
- `internal/accounting/errors.go`: Accounting domain error definitions.
- `internal/accounting/rules_test.go`: Unit tests for rules engine.
- `internal/accounting/accounts_test.go`: Unit tests for account registry.
- `internal/accounting/consumer_test.go`: Unit tests for event consumer.
- `internal/accounting/integration_test.go`: End-to-end database integration tests.
- `docs/implementation/phase-09-payment-ledger-integration-report.md`: Technical implementation report.

## Known Limitations
- **Seller Settlements & Supplier Payouts**: Reserved for future phases.
- **Marketplace Commissions**: Reserved for future phases.
- **Reconciliation Engine**: Reserved for future phases.

## Final Verification Status
- [x] Code formatted with `gofmt -s -w .`
- [x] Static analysis passed with `go vet ./...`
- [x] OpenAPI generator verified with `go run ./cmd/openapi-gen`
- [x] All unit and integration tests passing (`go test ./...`)
