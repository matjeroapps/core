# Marketplace Listing Resolution and Checkout Attribution Report

## Summary

Implemented the first transactional slice after the Unified Marketplace discovery MVP in `matjeroapps/core` only:
1. **Marketplace Listing Resolution**: Authoritative resolution API endpoint that turns a marketplace listing and requested quantity into a concrete, seller-scoped, fulfillment-eligible checkout handoff payload.
   ```text
   POST /internal/v1/markets/{market_code}/marketplace/listings/{listing_id}/resolve
   ```
2. **Single-Store Checkout Attribution**: Unambiguous, server-verified checkout attribution attached during checkout finalization, stored immutably in `marketplace_order_attributions`, integrated into checkout idempotency fingerprints, loaded with orders, and published on `order.created` events.

## Architectural Guarantees & Invariants

- **Single-Store Cart/Checkout Invariant**: Maintained strict single-seller checkout sessions. No multi-seller carts or split checkouts were introduced.
- **Authoritative Authority**: All transactional identifiers (`store_id`, `product_id`, `sku_id`, `price`, `market_code`, `fulfillment_location_id`) and attribution data are derived exclusively from Core transactional tables (`seller_listings`, `products`, `variants`, `skus`, `seller_listing_prices`, `fulfillment_locations`, `inventory_snapshots`, `supplier_offers`). Client-supplied pricing or location identifiers are never trusted.
- **Supplier Offer Revalidation**: If a product has a linked supplier offer, the resolution query re-verifies that the offer remains active, in the same market, in active supplier status, and within valid effective dating.
- **Fast-Delivery Local Invariant**: If `fast_delivery` is resolved, the inventory query specifically ensures an active store-owned fulfillment location in the target market with sufficient stock (`on_hand_qty - reserved_qty >= quantity`).
- **Attribution Verification**: When finalizing a checkout session with attribution:
  - `attribution.store_id` MUST match the checkout session / cart `store_id` (rejects with `ErrStoreMismatch`).
  - `attribution.market_code` MUST match the order/store market code.
  - `attribution.seller_listing_id` MUST match a listing associated with at least one item in the order (rejects with `ErrListingUnavailable`).
- **Idempotency Fingerprinting**: `Attribution` fields are included in `ComputeFinalizeFingerprint`. Replaying finalization with identical attribution returns the original order idempotently; modifying attribution generates a conflict fingerprint mismatch.
- **Information Privacy**: Internal reservation IDs, supplier costs, and raw inventory quantities are never exposed to callers.

## Database Changes

Added migration `000032_marketplace_attribution`:
- `migrations/000032_marketplace_attribution.up.sql`: Creates table `marketplace_order_attributions` with foreign keys referencing `orders(id)`, `checkout_sessions(id)`, `stores(id)`, `markets(code)`, and `seller_listings(id)` with unique index on `order_id` and query index on `(seller_listing_id, created_at DESC)`.
- `migrations/000032_marketplace_attribution.down.sql`: Drops table `marketplace_order_attributions`.

## API Contracts

### Endpoint

```text
POST /internal/v1/markets/{market_code}/marketplace/listings/{listing_id}/resolve
```

- **Authentication**: Core internal service auth (`platform`, `seller`, `admin`, `supplier`).
- **Request Body**:
  ```json
  {
    "quantity": 2,
    "source_collection": "fast_delivery",
    "locale": "ar"
  }
  ```
- **Response Body**:
  ```json
  {
    "seller_listing_id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
    "store_id": "8f8b88d3-5246-4e5a-939e-4c74c9351e39",
    "market_code": "EG",
    "product_id": "550e8400-e29b-41d4-a716-446655440000",
    "sku_id": "710e8400-e29b-41d4-a716-446655440001",
    "product_title": "ماكينة قهوة",
    "quantity": 2,
    "unit_price_minor": 150000,
    "currency_code": "EGP",
    "fulfillment_location_id": "220e8400-e29b-41d4-a716-446655440002",
    "attribution": {
      "seller_listing_id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
      "store_id": "8f8b88d3-5246-4e5a-939e-4c74c9351e39",
      "market_code": "EG",
      "source_collection": "fast_delivery"
    }
  }
  ```

### Checkout Finalization Request Extension

```json
{
  "session_id": "session-uuid",
  "shipping_address": { ... },
  "contact_email": "buyer@example.com",
  "attribution": {
    "seller_listing_id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
    "store_id": "8f8b88d3-5246-4e5a-939e-4c74c9351e39",
    "market_code": "EG",
    "source_collection": "fast_delivery"
  }
}
```

## Verification

1. `internal/marketplace`: Unit and Postgres integration tests (`TestServiceResolveListing*`, `TestIntegrationResolveListing`) verifying published resolution, fast-delivery store inventory, cross-market rejection, unpublished rejection, inactive product rejection, missing price rejection, and insufficient stock rejection.
2. `internal/coreapi`: Unit tests for service authentication, platform caller permissions, and closed error code mappings.
3. `modules/commerce`: Full checkout lifecycle integration tests (`TestMarketplaceAttributionCheckoutIntegration`, `TestMigration000032_UpAndDown`) validating attribution persistence, order queries (`GetOrderByID`, `GetGuestOrder`, `GetOrderByNumber`), idempotency replay fingerprints, and cross-store attribution rejections.
4. Toolchain: `gofmt`, `go vet`, and `openapi-gen` verified with zero regressions.
