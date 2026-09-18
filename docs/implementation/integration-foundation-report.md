# Phase 9 Integration Foundation Report

## Summary

Phase 9 introduces the **Integration Foundation** across `matjeroapps/core` and `matjeroapps/seller`. This module provides a centralized, provider-agnostic external integration data model, entity mapping registry, sync cursors, transactional webhook inbox, Core HTTP internal APIs, Coreclient methods, Seller actor HTTP APIs, and frontend integration management dashboards.

In accordance with architectural directives (ADR-017 & master plan sections 54-65), provider-specific business synchronization logic (for Salla, Shopify, WooCommerce, EasyOrders, and custom APIs) is decoupled into standalone integration contexts, while Commerce Core remains the single authoritative source of truth for products, listings, orders, inventory, payments, and balances.

---

## Architecture Changes

1. **Integration Foundation Domain (`internal/integration`)**:
   - `Connection` model for managing external platform credentials vault references, provider types (`salla`, `shopify`, `woocommerce`, `easyorders`, `custom_api`), statuses (`active`, `paused`, `error`, `disconnected`), and settings.
   - `EntityMapping` model for registering external-to-internal ID mappings (`external_id` <-> `internal_id`) across entity types (`product`, `variant`, `inventory`, `order`, `fulfillment`, `customer`).
   - `SyncCursor` model for tracking last successful sync tokens and reconciliation timestamps per connection and entity type.
   - `WebhookInboxItem` model for idempotent receipt and persistence of incoming raw provider webhooks.

2. **Database Schema & Constraints (`migrations/000027_integration_foundation.up.sql`)**:
   - `integration_connections` table.
   - `external_entity_mappings` table with strict unique constraints: `(connection_id, entity_type, external_id)` and `(connection_id, entity_type, internal_id)`.
   - `integration_sync_cursors` table.
   - `integration_webhook_inbox` table with unique constraint `(provider, idempotency_key)`.

3. **Internal Core API Exposure (`internal/coreapi/integration.go`)**:
   - Internal Core endpoints under `/internal/v1/integrations` protected by service authorization (`serviceauth.RequireCaller(CallerSeller, CallerSupplier, CallerAdmin)`).

4. **Seller Coreclient & Seller API (`matjeroapps/seller`)**:
   - Coreclient methods (`CreateConnection`, `ListConnections`, `UpsertEntityMapping`, `ListEntityMappings`) in `internal/coreclient/integration.go`.
   - Store-scoped HTTP endpoints (`/v1/seller/stores/{store_id}/integrations/...`) requiring `seller_owner` or `seller_manager` authorization.
   - Seller Web Integration Management dashboard in `web/seller/app/(dashboard)/dashboard/stores/[store_id]/integrations/page.tsx`.

---

## Repository Impact

- `matjeroapps/core`:
  - `migrations/000027_integration_foundation.up.sql` [NEW]
  - `migrations/000027_integration_foundation.down.sql` [NEW]
  - `internal/integration/integration.go` [NEW]
  - `internal/integration/repository.go` [NEW]
  - `internal/integration/service.go` [NEW]
  - `internal/coreapi/integration.go` [NEW]
  - `internal/coreapi/contracts.go` [MODIFY]
  - `internal/coreapi/router.go` [MODIFY]
  - `docs/api/internal/openapi.json` [MODIFY]
  - `docs/implementation/integration-foundation-report.md` [NEW]

- `matjeroapps/seller`:
  - `internal/coreclient/integration.go` [NEW]
  - `internal/sellerapi/integration.go` [NEW]
  - `internal/sellerapi/router.go` [MODIFY]
  - `internal/sellerapi/router_test.go` [MODIFY]
  - `docs/api/openapi.json` [MODIFY]
  - `web/seller/lib/api/types.ts` [MODIFY]
  - `web/seller/lib/api/client.ts` [MODIFY]
  - `web/seller/app/(dashboard)/dashboard/stores/[store_id]/integrations/page.tsx` [NEW]
  - `tests/e2e/seller-integration-foundation.spec.ts` [NEW]

---

## Database Changes

### Migration: `000027_integration_foundation.up.sql`

1. `integration_connections` Table:
   - `id` (VARCHAR(64), PK)
   - `actor_type` (VARCHAR(32), NOT NULL)
   - `actor_id` (VARCHAR(64), NOT NULL)
   - `provider` (VARCHAR(64), NOT NULL)
   - `name` (VARCHAR(255), NOT NULL)
   - `status` (VARCHAR(32), NOT NULL DEFAULT 'active')
   - `credentials_vault_ref` (VARCHAR(255))
   - `settings` (JSONB, DEFAULT '{}')
   - `created_at`, `updated_at` (TIMESTAMPTZ)
   - Indexes: `idx_integration_connections_actor`, `idx_integration_connections_provider`

2. `external_entity_mappings` Table:
   - `id` (VARCHAR(64), PK)
   - `connection_id` (VARCHAR(64), FK -> `integration_connections.id` ON DELETE CASCADE)
   - `entity_type` (VARCHAR(64), NOT NULL)
   - `internal_id` (VARCHAR(64), NOT NULL)
   - `external_id` (VARCHAR(255), NOT NULL)
   - `external_version` (VARCHAR(64))
   - `mapping_status` (VARCHAR(32), NOT NULL DEFAULT 'synced')
   - `sync_direction` (VARCHAR(32), NOT NULL DEFAULT 'bidirectional')
   - `conflict_status` (VARCHAR(64))
   - `metadata` (JSONB, DEFAULT '{}')
   - `last_synced_at`, `created_at`, `updated_at` (TIMESTAMPTZ)
   - Unique Constraints: `uq_external_entity_mappings_external`, `uq_external_entity_mappings_internal`

3. `integration_sync_cursors` Table:
   - `id` (VARCHAR(64), PK)
   - `connection_id` (VARCHAR(64), FK -> `integration_connections.id` ON DELETE CASCADE)
   - `entity_type` (VARCHAR(64), NOT NULL)
   - `cursor_token` (TEXT)
   - `last_successful_sync`, `last_reconciled_at`, `created_at`, `updated_at` (TIMESTAMPTZ)
   - Unique Constraint: `uq_integration_sync_cursors`

4. `integration_webhook_inbox` Table:
   - `id` (VARCHAR(64), PK)
   - `connection_id` (VARCHAR(64), FK -> `integration_connections.id` ON DELETE SET NULL)
   - `provider` (VARCHAR(64), NOT NULL)
   - `event_type` (VARCHAR(128), NOT NULL)
   - `idempotency_key` (VARCHAR(255), NOT NULL)
   - `payload` (JSONB, DEFAULT '{}')
   - `status` (VARCHAR(32), NOT NULL DEFAULT 'received')
   - `error_message` (TEXT)
   - `received_at`, `processed_at` (TIMESTAMPTZ)
   - Unique Constraint: `uq_integration_webhook_inbox_idempotency`

---

## API Changes

### Internal Core API (`/internal/v1`)
- `POST /internal/v1/integrations/connections`: Creates a provider connection.
- `GET /internal/v1/integrations/connections`: Lists connections by actor.
- `GET /internal/v1/integrations/connections/{id}`: Fetches connection details.
- `PATCH /internal/v1/integrations/connections/{id}/status`: Updates connection status.
- `POST /internal/v1/integrations/mappings`: Upserts an external entity mapping.
- `GET /internal/v1/integrations/mappings`: Lists entity mappings by connection and type.
- `GET /internal/v1/integrations/mappings/external`: Resolves mapping by external ID.
- `GET /internal/v1/integrations/mappings/internal`: Resolves mapping by internal ID.
- `POST /internal/v1/integrations/sync-cursors`: Updates sync cursor state.
- `GET /internal/v1/integrations/sync-cursors`: Fetches sync cursor state.
- `POST /internal/v1/integrations/webhooks/inbox`: Idempotently persists incoming provider webhooks.

### Seller HTTP API (`/v1/seller/stores/{store_id}`)
- `GET /v1/seller/stores/{store_id}/integrations/connections`: Lists store connections.
- `POST /v1/seller/stores/{store_id}/integrations/connections`: Creates store connection.
- `GET /v1/seller/stores/{store_id}/integrations/mappings`: Lists store entity mappings.

---

## Security Considerations

- Internal Endpoints are protected via `serviceauth.RequireCaller(CallerSeller, CallerSupplier, CallerAdmin)` requiring valid service bearer authentication tokens.
- Credentials vault references are stored as opaque string tokens (`credentials_vault_ref`); raw secrets/tokens are never stored unencrypted in platform databases.
- Store endpoints enforce `seller_owner` and `seller_manager` role checks.

---

## Testing & Verification

1. **Core Go Suite**:
   - `gofmt -s -w .`, `go vet ./...`, `go test -v ./internal/coreapi`: PASS.
2. **Seller Go Suite**:
   - `gofmt -s -w .`, `go vet ./...`, `go test ./...`: PASS (100% of unit/contract tests pass).
3. **OpenAPI Spec Generation**:
   - `go run ./cmd/openapi-gen` in both `core` and `seller`: PASS.
4. **Seller Web Production Build**:
   - `npm run build` in `web/seller`: PASS.

---

## Final Verification Status

- `gofmt -s -w .`: PASS
- `go vet ./...`: PASS
- `go test`: PASS
- `go run ./cmd/openapi-gen`: PASS
- `npm run build`: PASS
