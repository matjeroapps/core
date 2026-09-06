# MatjerHub Phase 5.8 — Core Seller Catalog Operations Report

## Metadata
- **Base SHA**: `a4efd86706a8ac87a58a597a5b65ba07d68a6828`
- **Branch**: `feature/p5-8-seller-catalog-operations`
- **Reviewed Head SHA**: `7753bd0152f6dd94ac567eb781a32a21095a1c6e`
- **PR Status**: Ready for review (Not merged)

## Migration 000014
- `000014_seller_catalog_authoring.up.sql` / `000014_seller_catalog_authoring.down.sql`
- Created `seller_products` mapping table to represent explicit Seller product ownership without modifying global `products`.
- Created `seller_listing_presentations` storing structured section JSONB content and product-level purchase behavior overrides (`inherit`, `add_to_cart`, `buy_now`).
- Extended `media_metadata` with `storage_key` and `is_primary`, with partial index enforcing maximum one primary image per product.

## Key Capabilities Implemented
1. **Seller Product Ownership & Authorization**: Enforced tenant isolation where Seller A cannot access Seller B products (`404 Not Found`). Closed the listing creation gap when `supplier_offer_id == nil` to enforce Seller ownership.
2. **Product Authoring & Variants/SKUs**: Atomic seller product creation, localized EN/AR translations, category mapping, variants, and 1-active-SKU MVP authoring guard.
3. **S3-Compatible Media Upload Flow**: Presigned PUT URL generation, HeadObject verification, upload completion, MIME/size validation, and metadata ordering/primary flags.
4. **Seller Inventory & Fulfillment Locations**: Store fulfillment locations, inventory snapshot management, delta adjustments via `AdjustInventory`, and protection against manual `reserved_qty` mutations.
5. **Product Page Presentation & Purchase Behavior**: Structured sections (`description`, `highlights`, `image_text`, `specifications`, `faq`, `final_cta`) with strict HTML/script tag validation and purchase behavior resolution (`add_to_cart` / `buy_now`).
6. **Publish Readiness & Transactions**: Multi-invariant readiness verification before publish, atomic publish/unpublish DB transactions, and automatic storefront revision cache invalidations.
7. **Seller Order Operations**: Store-scoped order listing and detail retrieval with strict transition dispatch (`pending -> confirmed -> processing -> ready_for_shipping`), preserving outbox and timeline correlation.

## Automated Verification
- `gofmt -w` executed cleanly.
- `go list ./...` & `go vet ./...` executed cleanly.
- Integration tests in `modules/commerce` and `internal/coreapi` passed 100% cleanly (including `TestFirstLiveProductAndOrderCoreIntegration` and `TestSellerProductTenantIsolationAndSecurity`).
- Focused test suite passed x10 runs without failures.
- OpenAPI specification regenerated via `go run ./cmd/openapi-gen`.
