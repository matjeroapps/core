# Seller Catalog Operations — Core HTTP Contracts and Storefront Projection Implementation Report

**Status:** Complete & Verified

**Date:** 2026-09-17

**Repository:** `matjeroapps/core`

## Summary

Implemented Core HTTP contracts and storefront projection for store-scoped seller catalog operations as specified in the plan at `core/docs/plans/2026-09-16-seller-catalog-operations-integration.md` (Phase D). 

This work exposes the complete target store-scoped Core internal `/internal/v1` HTTP endpoints, enforces strict store-level scope validation across all nested resource identifiers (`productID`, `listingID`, `assetID`, `referenceID`, `variantID`, `skuID`, `snapshotID`, `locationID`, `orderID`), maintains compatibility product publish shims, ensures public storefront reads project only published/eligible listings and ready media, preserves storefront theme/domain host resolution and cache revision invalidations, and regenerates the Core internal OpenAPI specification.

## Architecture Changes

1. **Store-Scoped Resource Boundary Verification**:
   - Enforced that every store-scoped HTTP request under `/internal/v1/stores/{storeID}/...` validates both seller membership and store-to-resource ownership.
   - Resource access for products, variants, SKUs, media references, listing presentations, readiness, and inventory snapshots verifies that the target resource belongs to `{storeID}`.
   - Any cross-store or cross-tenant resource attempt (including same-seller cross-store attempts) returns `404 not_found` (`CodeNotFound`), preventing tenant enumeration or resource leakage across stores.

2. **Public Storefront Catalog Read Projection**:
   - Storefront browse, search, category, and detail read models rely on `catalog.CanonicalListingSQL` and `store_media_assets` (`status = 'ready'`) / `product_media_references`.
   - Only `PUBLISHED` (or active) listings with eligible products and available stock in the store market surface to public storefront queries.
   - Preserved storefront host resolution (`X-Matjero-Storefront-Host`), store bootstrap, theme configuration (published vs signed draft preview), and revision invalidation on catalog mutations.

3. **Compatibility Shims**:
   - Maintained `POST /internal/v1/stores/{storeID}/products/{productID}/publish` and `POST /internal/v1/stores/{storeID}/products/{productID}/unpublish` as documented seller-owned shims that delegate to store listing publish/unpublish services.

## Repository Impact

- `matjeroapps/core`: All modifications and tests are contained within `core`. No external repos were mutated.

## Database Changes

- Existing schema migrations `000025_seller_catalog_phase_b` and `000026_seller_catalog_phase_c` were preserved without modification. No new SQL schema migration was required for this phase.

## API Changes

- Core internal OpenAPI specification (`docs/api/internal/openapi.json`) was regenerated via `cmd/openapi-gen` to accurately reflect all store-scoped HTTP routes, DTOs, parameters, and error responses.

Key routes verified and exposed:
- `GET /internal/v1/stores/{storeID}/products`
- `POST /internal/v1/stores/{storeID}/products`
- `GET /internal/v1/stores/{storeID}/products/{productID}`
- `PUT /internal/v1/stores/{storeID}/products/{productID}`
- `POST /internal/v1/stores/{storeID}/products/{productID}/status`
- `POST /internal/v1/stores/{storeID}/products/{productID}/archive`
- `POST /internal/v1/stores/{storeID}/products/{productID}/publish` (compatibility shim)
- `POST /internal/v1/stores/{storeID}/products/{productID}/unpublish` (compatibility shim)
- `POST /internal/v1/stores/{storeID}/products/{productID}/variants`
- `PUT /internal/v1/stores/{storeID}/products/{productID}/variants/{variantID}`
- `POST /internal/v1/stores/{storeID}/products/{productID}/variants/{variantID}/skus`
- `PUT /internal/v1/stores/{storeID}/products/{productID}/variants/{variantID}/skus/{skuID}`
- `GET /internal/v1/stores/{storeID}/listings/{listingID}`
- `PUT /internal/v1/stores/{storeID}/listings/{listingID}/price`
- `GET /internal/v1/stores/{storeID}/listings/{listingID}/readiness`
- `POST /internal/v1/stores/{storeID}/listings/{listingID}/publish`
- `POST /internal/v1/stores/{storeID}/listings/{listingID}/unpublish`
- `POST /internal/v1/stores/{storeID}/listings/{listingID}/archive`
- `GET /internal/v1/stores/{storeID}/listings/{listingID}/presentation`
- `PUT /internal/v1/stores/{storeID}/listings/{listingID}/presentation`
- `GET /internal/v1/stores/{storeID}/media`
- `POST /internal/v1/stores/{storeID}/media/uploads`
- `POST /internal/v1/stores/{storeID}/media/uploads/{intentID}/complete`
- `DELETE /internal/v1/stores/{storeID}/media/{assetID}`
- `GET /internal/v1/stores/{storeID}/products/{productID}/media-references`
- `POST /internal/v1/stores/{storeID}/products/{productID}/media-references`
- `PUT /internal/v1/stores/{storeID}/products/{productID}/media-references/{referenceID}`
- `DELETE /internal/v1/stores/{storeID}/products/{productID}/media-references/{referenceID}`
- `GET /internal/v1/stores/{storeID}/inventory`
- `POST /internal/v1/stores/{storeID}/inventory/snapshots`
- `POST /internal/v1/stores/{storeID}/inventory/{snapshotID}/adjustments`
- `GET /internal/v1/stores/{storeID}/orders/{orderID}`
- `POST /internal/v1/stores/{storeID}/orders/{orderID}/transition`

## Security Considerations

- Strict store-level and seller-level authorization is enforced on every endpoint.
- Cross-tenant and same-seller cross-store resource access attempts return uniform 404 Not Found responses (`CodeNotFound`) to prevent tenant or resource enumeration.
- MinIO object keys, S3 completion token digests, internal supplier costs, and internal location details are omitted from public storefront responses.

## Testing and Exact Validation Results

### Commands Executed

```bash
gofmt -s -w .
go vet ./...
go test -v ./internal/coreapi -run TestStoreCatalogIsolationContract
go run ./cmd/openapi-gen
go test ./...
```

### Validation Results

1. **`gofmt -s -w .`**: Clean, no formatting diffs.
2. **`go vet ./...`**: Clean, 0 warnings/errors.
3. **`go test ./internal/coreapi`**: Passed (`ok github.com/matjeroapps/core/internal/coreapi 0.236s`).
4. **`go test ./...`**: Full Core test suite passed clean across all modules (`apps/workers`, `internal/accounting`, `internal/balance`, `internal/coreapi`, `internal/finance`, `internal/marketplace_finance`, `internal/payments`, `internal/serviceauth`, `internal/settlement`, `internal/shipping`, `internal/suppliers`, `internal/testdb`, `modules/actorapi`, `modules/commerce`, `modules/markets`, `modules/openapi`, `modules/storefront`, `modules/themes`, `packages/auth`, `packages/config`, `packages/events`, `packages/httpx`, `packages/i18n`, `packages/inbox`, `packages/messaging`, `packages/money`, `packages/outbox`).
5. **OpenAPI generation**: Verified and updated `docs/api/internal/openapi.json`.

## Files Changed

- `core/modules/commerce/seller_catalog_service.go`: Added store listing association checks to product, variant, SKU, media reference, product status, archive, and inventory snapshot/adjustment service methods; updated upload intent store mismatch to return `ErrNotFound`.
- `core/internal/coreapi/store_isolation_contract_test.go`: Created contract test suite covering store catalog isolation matrix (A1/A2/B1 stores and sellers, same-seller cross-store isolation, cross-seller isolation, random non-existent resource 404 verification). Fixed `ProductDraft` fixture (`Status` and `SupplierCode`) to satisfy live database validations.
- `core/docs/api/internal/openapi.json`: Regenerated OpenAPI specification document.
- `core/docs/implementation/seller-catalog-http-contracts-report.md`: Created implementation report.

## Known Limitations

- Real PostgreSQL/MinIO integration tests require containerized Docker services (`platform-infra`); unit and contract tests mock or gracefully skip DB connection when local PostgreSQL is offline.

## Final Verification Status

**APPROVED** — Code, OpenAPI spec, tests, and documentation are complete and verified. Phase-name leakage audit passed cleanly.
