# Phase 06: Shipping Foundation Implementation Report

## Feature Name & Summary
**Feature**: Phase 06 Shipping Foundation  
**Summary**: Implements the Core physical fulfillment domain, PostgreSQL shipping schema, state machine transitions, transactional outbox events (`ShipmentCreated`, `ShipmentStatusChanged`), internal HTTP APIs under `/internal/v1/shipments`, and OpenAPI specification updates. This completes the physical fulfillment domain foundation without external courier provider integrations.

## Architecture Changes
- **ADR-001 (PostgreSQL Source of Truth)**: Persistent transactional state in `shipments`, `shipment_items`, and `shipment_events`.
- **ADR-006 & ADR-018 (Transactional Outbox & Messaging)**: Enqueues `shipping.shipment.created.v1` and `shipping.shipment.status_changed.v1` events atomically inside database transactions using `outbox.Store{}.Enqueue`.
- **ADR-017 (Service Authorization & Boundaries)**: Internal endpoints under `/internal/v1/orders/{order_id}/shipments`, `/internal/v1/shipments/{shipment_id}/status`, and `/internal/v1/shipments/{shipment_id}` require service token authentication (`X-Matjero-Service`).
- **Money Invariants**: Shipping costs (`shipping_cost_minor`) and cash on delivery amounts (`cod_amount_minor`) are stored in integer minor units with ISO currency codes (e.g. `EGP`, `USD`).

## Database Changes
- **Migration**: `000017_create_shipping_schema.up.sql` / `000017_create_shipping_schema.down.sql`
- **Tables Created**:
  1. `shipments` (id, order_id, fulfillment_location_id, status, tracking_number, shipping_cost_minor, cod_amount_minor, currency, created_at, updated_at)
  2. `shipment_items` (id, shipment_id, order_item_id, quantity, created_at)
  3. `shipment_events` (id, shipment_id, status, notes, occurred_at)
- **Indexes Created**:
  - `shipments_order_id_idx` on `shipments(order_id)`
  - `shipments_status_idx` on `shipments(status)`
  - `shipment_items_shipment_id_idx` on `shipment_items(shipment_id)`
  - `shipment_events_shipment_id_idx` on `shipment_events(shipment_id, occurred_at ASC)`

## API Changes
Internal HTTP API Endpoints added to `internal/coreapi`:
1. `POST /internal/v1/orders/{orderID}/shipments` - Creates a new shipment and enqueues `ShipmentCreated` outbox event.
2. `PATCH /internal/v1/shipments/{shipmentID}/status` - Transitions shipment status using the domain state machine and enqueues `ShipmentStatusChanged` outbox event.
3. `GET /internal/v1/shipments/{shipmentID}` - Retrieves detailed shipment information including item snapshots and event history timeline.

OpenAPI Specification updated and regenerated at `docs/api/internal/openapi.json`.

## State Machine Transitions
Allowed states: `PENDING`, `PROCESSING`, `READY_FOR_PICKUP`, `SHIPPED`, `OUT_FOR_DELIVERY`, `DELIVERED`, `FAILED`, `RETURNED`.
- `PENDING` -> `PROCESSING`, `FAILED`
- `PROCESSING` -> `READY_FOR_PICKUP`, `SHIPPED`, `FAILED`
- `READY_FOR_PICKUP` -> `SHIPPED`, `FAILED`
- `SHIPPED` -> `OUT_FOR_DELIVERY`, `DELIVERED`, `FAILED`, `RETURNED`
- `OUT_FOR_DELIVERY` -> `DELIVERED`, `FAILED`, `RETURNED`
- `DELIVERED` -> `RETURNED`
- `FAILED` -> `PROCESSING`, `RETURNED`
- `RETURNED` -> none

## Testing Results
- **Unit Tests**: `TestStatusValidation`, `TestStateMachineTransitions` in `internal/shipping/state_machine_test.go` verifying valid and invalid state machine transitions.
- **Integration Tests**: `TestShipmentCreationAndOutbox`, `TestShipmentStatusTransitionLifecycle` verifying atomic database persistence and transactional outbox event insertion.
- **Concurrency Tests**: `TestShipmentConcurrentStatusUpdates` verifying thread-safe transitions using PostgreSQL `FOR UPDATE` row locking under high concurrency (10 worker goroutines).
- **Validation Suite Output**:
  - `gofmt -s -w .` -> PASSED
  - `go vet ./...` -> PASSED (0 issues)
  - `go test -count=1 ./internal/shipping/...` -> PASSED (`ok github.com/matjeroapps/core/internal/shipping 3.582s`)
  - `go run ./cmd/openapi-gen` -> PASSED

## PR Details & Status
- **Branch**: `feature/p6-shipping-foundation`
- **Target Branch**: `main`
- **PR Title**: `feat: Phase 6 Shipping Foundation`
- **Status**: Branch committed and pushed to remote; PR created targeting `main`. Pending review and CI checks.
