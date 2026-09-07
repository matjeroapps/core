# ADR-019 Supplier Retail Provisioning Implementation Report

## Feature Name & Summary
**Feature Name**: ADR-019 Supplier Retail Provisioning Flow  
**Summary**: Established the explicit, atomic 1:1 Supplier <-> Seller capability link within the Core repository (`matjeroapps/core`). This enables Suppliers to provision and operate direct-to-consumer retail storefronts safely without polymorphic store ownership.

## Architecture Changes
- Created domain service `internal/suppliers/retail.go` providing `ProvisionRetailCapabilityTx`, an atomic PostgreSQL transaction handling the creation of:
  1. `sellers` record.
  2. `seller_settings` record.
  3. `seller_members` inserting strictly the authenticated Supplier Owner as the Seller Owner (ADR-019).
  4. 1:1 `supplier_seller_affiliations` record.
- Any transaction failure (e.g. 1:1 affiliation conflict or duplicate seller code) rolls back completely leaving zero orphaned seller records.
- Enforced strict authorization: non-owner supplier members (e.g., `manager` role) receive HTTP 403 Forbidden.
- Implemented internal HTTP handler in `internal/coreapi/suppliers_retail.go` mounted at `POST /internal/v1/suppliers/{supplierID}/retail-capability` restricted to `X-Matjero-Service: supplier` callers.

## Database & API Changes
- **Migration**: Added `migrations/000018_supplier_retail_affiliation.up.sql` and `migrations/000018_supplier_retail_affiliation.down.sql` enforcing `ON DELETE RESTRICT` foreign keys on `supplier_seller_affiliations`.
- **API Endpoint**: `POST /internal/v1/suppliers/{supplierID}/retail-capability`
  - Body: `{"name": "Retail Store Name", "code": "ret-prefix"}`
  - Response: Created `Seller` profile DTO and `SupplierSellerAffiliation`.
- **OpenAPI**: Updated `docs/api/internal/openapi.json` via generator `go run ./cmd/openapi-gen`.

## Testing & Validation Results
- `gofmt -s -w .`: Executed with clean exit code 0.
- `go vet ./...`: Executed with clean exit code 0.
- `go test -count=1 ./internal/suppliers/...`: PASSED (3.8s)
  - `TestProvisionRetailCapability_Success`
  - `TestProvisionRetailCapability_SecurityNonOwnerForbidden`
  - `TestProvisionRetailCapability_OneToOneConstraint`
  - `TestProvisionRetailCapability_AtomicRollbackOnFailure`
  - `TestGetSupplierSellerAffiliation`
- `go test -count=1 ./internal/coreapi/...`: PASSED (50.6s)
  - `TestSupplierRetailAPI_SecurityAndCapabilities`
- `go run ./cmd/openapi-gen`: Executed with clean exit code 0.

## PR Status
- Branch: `feature/p2-supplier-retail-provisioning`
- Target Branch: `main`
- Commit & PR created: Pending user review and CI checks (not auto-merged).
