# Curated Marketplace Discovery Report

## Summary

Implemented the Core-only curated marketplace discovery read model and internal API endpoint:

```text
GET /internal/v1/markets/{market_code}/marketplace/collections/{collection_type}
```

The implementation reconciles the approved plan with the current schema and deliberately avoids speculative read-model tables. No migration was added because the supported collections can be served from authoritative transactional tables.

## Implemented Collections

- `best_sellers`: published listings for active products with a current price, ordered by delivered `order_items.quantity` descending and `seller_listing_id` ascending. Only `orders.status = 'delivered'` contributes to the aggregate.
- `new_products`: published listings for active products with a current price, ordered by `seller_listings.created_at DESC` and `seller_listing_id ASC`. No `published_at` field is invented.
- `unique_products`: published listings for active products with a current price where the product has exactly one eligible published listing in the requested market.
- `fast_delivery`: published listings for active products with a current price and positive available inventory at an active store-owned fulfillment location in the requested market. The badge means local availability only, not a delivery SLA.

## Deferred or Unavailable Collections

- `offers`: unavailable. The current schema stores only the current seller listing price, so there is no reliable discount baseline or promotion source.
- `trending`: unavailable. The current schema has no authoritative engagement or trend metric source and no refresh process.

Requests for unavailable collection types return the existing internal `unavailable` error code instead of an empty or speculative collection.

## Architecture Changes

- Added `internal/marketplace` with domain types, service validation, cursor handling, and PostgreSQL repository queries.
- Added `internal/coreapi/marketplace.go` and mounted the endpoint behind service-auth protection.
- Wired the marketplace service into `apps/core-api/main.go`.
- Updated the internal OpenAPI route declarations and regenerated `docs/api/internal/openapi.json`.
- Batched ordered migration SQL in the Core integration-test fixtures to reduce database round trips while preserving isolated schemas and migration coverage.
- The focused marketplace repository fixture improved from approximately 88 seconds to 9 seconds, and the representative Core API integration fixture improved from approximately 105 seconds to 9 seconds on the local PostgreSQL infrastructure.

## Repository Impact

Only `matjeroapps/core` was modified. No seller, platform, supplier, admin, or UI SDK repositories were changed.

## Database Changes

No migration was added.

Latest migration verified before implementation:

```text
000031_integration_api_hardening.up.sql
```

No `marketplace_listing_metrics` table was created. If a metrics table is introduced later, it must define its authoritative source, refresh/backfill mechanism, staleness behavior, absent-metric behavior, market isolation, indexes, and cursor ordering before the API treats metrics-backed collections as complete.

## API Changes

Endpoint:

```text
GET /internal/v1/markets/{market_code}/marketplace/collections/{collection_type}?locale=ar&limit=20&cursor=...
```

Supported collection types:

```text
best_sellers
new_products
unique_products
fast_delivery
offers      # returns unavailable
trending    # returns unavailable
```

Responses use stable seek pagination with opaque cursors. Locale fallback uses `product_translations`, falling back between Arabic and English and then to the product slug.

## Security Considerations

- Route is mounted behind the existing service-auth middleware.
- Only authenticated internal services may call it.
- Queries are strictly scoped by `market_code`.
- SQL uses parameterized queries and fixed collection ordering fragments selected from enum values.
- No supplier costs, internal inventory quantities, reservation identifiers, or fulfillment location identifiers are exposed.

## Testing and Verification

Implemented focused tests for:

- Empty collections
- Market isolation
- Arabic-to-English locale fallback
- Invalid collection type
- Cursor stability across pages
- Unpublished listings excluded
- Inactive products excluded
- Delivered-only best-seller aggregation
- Current-price filtering
- Unavailable local inventory excluded from fast delivery
- Service-auth protection

Commands run:

```bash
gofmt -s -w .
go vet ./...
go test -timeout=20m ./...
go run ./cmd/openapi-gen
git diff --check
```

Shared infrastructure:

```bash
cd ../platform-infra
make up-infra
make ps
```

PostgreSQL was verified ready with the project container and `pg_isready`. The repository-wide test run exercises the database-backed suites against the local infrastructure. `make test` uses the same explicit 20-minute Go package timeout because the existing integration-heavy `modules/commerce` package takes longer than Go's default 10-minute package timeout even when all tests pass. The clean-tree OpenAPI check passes once the generated spec is included with the feature change.

## Files Changed

- `apps/core-api/main.go`
- `docs/api/internal/openapi.json`
- `docs/implementation/curated-marketplace-discovery-report.md`
- `internal/coreapi/errors.go`
- `internal/coreapi/integration_test.go`
- `internal/coreapi/marketplace.go`
- `internal/coreapi/marketplace_test.go`
- `internal/coreapi/router.go`
- `internal/coreapi/seller_catalog_contract_test.go`
- `internal/coreapi/spec.go`
- `internal/coreapi/store_isolation_contract_test.go`
- `internal/coreapi/supplier_retail_capability_integration_test.go`
- `internal/marketplace/domain.go`
- `internal/marketplace/errors.go`
- `internal/marketplace/integration_test.go`
- `internal/testdb/testdb.go`
- `modules/commerce/migration_test_helpers_test.go`
- `modules/commerce/migration_external_test_helpers_test.go`
- `modules/commerce/customer_cart_integration_test.go`
- `modules/commerce/orders_integration_test.go`
- `modules/commerce/repository_integration_test.go`
- `modules/commerce/seller_catalog_integration_test.go`
- `modules/commerce/store_domain_integration_test.go`
- `modules/commerce/store_lifecycle_integration_test.go`
- `modules/commerce/storefront_revision_integration_test.go`
- `modules/commerce/supplier_composites_integration_test.go`
- `modules/commerce/supplier_retail_capability_integration_test.go`
- `Makefile`
- `internal/marketplace/repository.go`
- `internal/marketplace/service.go`
- `internal/marketplace/service_test.go`

## Known Limitations

- `fast_delivery` is a local inventory availability signal only. It does not promise delivery time.
- `offers` and `trending` are unavailable until authoritative sources and refresh/backfill processes exist.
- No materialized marketplace metrics table exists in this slice.

## Final Verification Status

Complete for the Core slice. No new migration was added, so migration validation used the repository's existing `make migrate-check` tooling; there was no new up/down pair to execute.
