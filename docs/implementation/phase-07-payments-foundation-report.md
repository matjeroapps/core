# Phase 7 Payments Foundation Implementation Report

## Feature Name & Summary
**Feature**: Phase 7 Payments Foundation  
**Target Repository**: `matjeroapps/core`  
**Summary**: Established the Payments domain aggregate, status transition state machine (`CREATED`, `PENDING`, `AUTHORIZED`, `CAPTURED`, `FAILED`, `CANCELLED`, `REFUNDED`), transactional outbox event publishing (`payments.payment.captured.v1`, `payments.payment.failed.v1`), and the Webhook Inbox pattern with deduplication based on `(provider, provider_event_id)` for idempotent payment capture and Cash on Delivery (COD) tracking.

---

## Architecture & Design Compliance

1. **ADR-003 (Monetary Values)**: All monetary fields strictly use `amount_minor` (`BIGINT`) and ISO 4217 `currency` (`CHAR(3)`). No floating-point values are used anywhere.
2. **ADR-006 (Transactional Outbox)**: Outbox event envelopes (`PaymentCaptured`, `PaymentFailed`) are enqueued atomically inside the database transaction alongside payment status mutations.
3. **ADR-007 / Master Plan Rule 45 (Webhook Inbox)**: External payment webhooks are persisted to `webhook_inbox` prior to processing, enforcing strict deduplication via a unique index on `(provider, provider_event_id)`.
4. **ADR-017 (Core Internal API)**: Internal endpoints are exposed under `/internal/v1` and protected using `X-Matjero-Service` authorization (`serviceauth.RequireCaller(CallerSeller, CallerAdmin)`).

---

## Database & API Changes

### Database Migrations (`000019_create_payments_schema.up.sql`)
- `payments` table:
  - Columns: `id` (UUID PK), `order_id` (UUID FK to `orders`), `amount_minor` (BIGINT), `currency` (CHAR(3)), `payment_method` (TEXT), `status` (TEXT with CHECK constraint), `created_at`, `updated_at`.
  - Indexes: `payments_order_id_idx`, `payments_status_idx`.
- `payment_attempts` table:
  - Columns: `id` (UUID PK), `payment_id` (UUID FK to `payments`), `provider` (TEXT), `provider_reference` (TEXT), `status` (TEXT), `error_message` (TEXT), `created_at`, `updated_at`.
  - Index: `payment_attempts_payment_id_idx`.
- `webhook_inbox` table:
  - Columns: `id` (UUID PK), `provider` (TEXT), `connection_id` (TEXT), `provider_event_id` (TEXT), `event_type` (TEXT), `payload_json` (JSONB), `signature_verified` (BOOLEAN), `status` (TEXT), `attempt_count` (INT), `received_at`, `processed_at`.
  - Unique Constraint: `webhook_inbox_provider_event_uidx UNIQUE (provider, provider_event_id)`.
  - Index: `webhook_inbox_status_idx`.

### Internal HTTP Endpoints (`/internal/v1`)
- `POST /internal/v1/orders/{orderID}/payments`: Initializes a payment for an order (status `CREATED`).
- `PATCH /internal/v1/payments/{paymentID}/status`: Transitions payment status according to state machine rules and records attempt details. Emits outbox events when transitioning to `CAPTURED` or `FAILED`.
- `POST /internal/v1/webhooks/payments/inbox`: Persists raw provider webhook payload in `webhook_inbox` with deduplication handling.

---

## Testing Results

- `gofmt -s -w .`: PASS
- `go vet ./...`: PASS
- `go test -count=1 ./internal/payments/... ./packages/events/...`: PASS
  - Unit tests verifying payment state transitions (e.g. blocking `FAILED` -> `CAPTURED`, `REFUNDED` -> `CAPTURED`, valid forward transitions, self-transitions).
  - Integration tests verifying payment initialization, status transitions, atomic outbox event enqueueing (`payments.payment.captured.v1`, `payments.payment.failed.v1`), and `webhook_inbox` deduplication.
- `go run ./cmd/openapi-gen`: PASS (regenerated `docs/api/internal/openapi.json`).

---

## PR Status
- Branch: `feature/p7-payments-foundation`
- PR Title: `feat: Phase 7 Payments Foundation`
- Full git diff saved to: `docs/implementation/phase-07-payments-foundation-diff.txt`
