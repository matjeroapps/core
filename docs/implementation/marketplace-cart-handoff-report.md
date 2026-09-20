# Marketplace Cart Handoff Implementation Report

## Summary

Implemented the single-store marketplace cart handoff in `matjeroapps/core`:
1. **Dedicated Marketplace Cart Endpoint**:
   ```text
   POST /internal/v1/markets/{market_code}/marketplace/carts/items
   ```
   Provides a server-to-server entrypoint restricted exclusively to the `platform` service caller, enabling marketplace catalog discovery traffic to transition into commerce carts without exposing seller-internal routing or trusting client-asserted store or pricing parameters.
2. **Authoritative Listing Resolution & Handoff**: Resolves the target listing, variant, SKU, pricing, store ownership, and local inventory authoritatively through `marketplace.Service.ResolveListing` before any cart mutation occurs.
3. **Verified Single-Store & Market Invariants**: Guarantees single-seller cart isolation. Rejects cross-store and cross-market mixing with conflict errors, reuses existing cart tokens for subsequent items in the same store, and preserves verified marketplace attribution for checkout finalization.

---

## Architecture & Design

```text
┌─────────────────────────────────────────────────────────────┐
│                 matjeroapps/platform                        │
│                 (Platform Service Caller)                   │
└──────────────────────────────┬──────────────────────────────┘
                               │ POST /internal/v1/markets/{market_code}/marketplace/carts/items
                               │ Header: X-Matjero-Service: platform
                               │ Header: Authorization: Bearer <platform-token>
                               ▼
┌─────────────────────────────────────────────────────────────┐
│                     matjeroapps/core                        │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 1. Service-Auth & Route Authorization                 │  │
│  │    Enforce caller == "platform" (Seller/Supplier 403) │  │
│  └───────────────────────────┬───────────────────────────┘  │
│                              ▼                              │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 2. Authoritative Marketplace Listing Resolution       │  │
│  │    Derive store_id, product_id, sku_id, price,        │  │
│  │    currency, and verify local inventory availability  │  │
│  └───────────────────────────┬───────────────────────────┘  │
│                              ▼                              │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 3. Cart Token & Single-Store Invariant Validation     │  │
│  │    - No token: CreateCart(store_id, market_code)      │  │
│  │    - Existing token: Verify same store & market       │  │
│  │    - Cross-store or cross-market: 409 Conflict        │  │
│  └───────────────────────────┬───────────────────────────┘  │
│                              ▼                              │
│  ┌───────────────────────────────────────────────────────┐  │
│  │ 4. Atomic Cart Mutation & Attribution Handoff         │  │
│  │    - AddCartItem with resolved SKU and authoritative  │  │
│  │      seller_listing_id                                │  │
│  │    - Return MarketplaceCartHandoffResponse            │  │
│  └───────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

---

## API Contract

### Endpoint

```text
POST /internal/v1/markets/{market_code}/marketplace/carts/items
```

- **Authentication**: Service-auth middleware requiring caller `platform`.
- **Headers**:
  - `Authorization: Bearer <platform-token>`
  - `X-Matjero-Service: platform`
  - `X-Matjero-Cart-Token: <optional cart token>`

### Request Body

```json
{
  "seller_listing_id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
  "sku_id": "710e8400-e29b-41d4-a716-446655440001",
  "quantity": 2,
  "source_collection": "fast_delivery",
  "cart_token": "optional-raw-cart-token",
  "locale": "ar"
}
```

### Response Body (`200 OK`)

```json
{
  "cart_id": "771e8400-e29b-41d4-a716-446655440099",
  "cart_token": "raw-cart-token-value",
  "store_id": "8f8b88d3-5246-4e5a-939e-4c74c9351e39",
  "market_code": "EG",
  "status": "active",
  "items": [
    {
      "id": "item-uuid-1",
      "seller_listing_id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
      "sku_id": "710e8400-e29b-41d4-a716-446655440001",
      "quantity": 2,
      "unit_price_minor": 150000,
      "currency_code": "EGP"
    }
  ],
  "attribution": {
    "seller_listing_id": "9b1deb4d-3b7d-4bad-9bdd-2b0d7b3dcb6d",
    "store_id": "8f8b88d3-5246-4e5a-939e-4c74c9351e39",
    "market_code": "EG",
    "source_collection": "fast_delivery"
  }
}
```

---

## Key Invariants & Guarantees

1. **Strict Single-Store Cart Boundary**:
   - Each cart is bound permanently to its creating `store_id` and `market_code`.
   - Adding a listing from a different store using an existing `cart_token` fails with `ErrStoreMismatch` (`409 Conflict`), preventing cross-store mixing.
2. **Strict Market Boundary**:
   - Adding a listing from another market using an existing `cart_token` fails with `ErrMarketMismatch` (`409 Conflict`).
3. **Authoritative Resolution & Trust Zero Client Values**:
   - Client cannot supply or alter `store_id`, `product_id`, `unit_price_minor`, `currency_code`, or fulfillment location.
   - All values are resolved server-side from Core transactional tables (`seller_listings`, `products`, `variants`, `skus`, `seller_listing_prices`, `fulfillment_locations`, `inventory_snapshots`).
4. **Resolution Before Mutation**:
   - If listing resolution fails (e.g. unpublished listing, inactive product, missing active price, out-of-stock inventory, invalid quantity), no cart is created and no existing cart is modified.
5. **Attribution Preservation**:
   - The returned `attribution` payload contains server-verified `seller_listing_id`, `store_id`, `market_code`, and `source_collection`.
   - When Platform finalizes checkout with this attribution, Core re-verifies that `attribution.seller_listing_id` belongs to an item in the cart, and immutably records the attribution in `marketplace_order_attributions`.
6. **Data Privacy**:
   - Raw supplier costs, reservation IDs, internal inventory warehouse locations, and backend sequence numbers are omitted from the response.

---

## Explicit Non-Goals

The following capabilities remain strictly out of scope for this single-store marketplace slice:
- ❌ Multi-seller aggregated carts.
- ❌ Multi-merchant order splitting.
- ❌ Marketplace commission calculations and settlement allocations during checkout.
- ❌ Payment gateway orchestration or direct charging in the cart route.
- ❌ Shipping carrier rate discovery or booking in the cart route.

---

## Verification & Test Results

### 1. Unit & Error Mapping Tests (`internal/coreapi/marketplace_test.go`)
- `TestAddMarketplaceCartItemRequiresServiceAuth`: Verifies unauthenticated requests are rejected with 401 Unauthorized.
- `TestAddMarketplaceCartItemRestrictedToPlatformCaller`: Verifies caller `platform` is permitted, while `seller`, `admin`, and `supplier` callers are rejected with 403 Forbidden.
- `TestAddMarketplaceCartItemResolutionErrorMappings`: Verifies closed vocabulary error mappings for invalid input (400), invalid quantity (400), not found (404), unpublished listing (409), inactive product (409), price unavailable (409), cross-market access (409), insufficient inventory (409), and store mismatch (409).

### 2. Live PostgreSQL Integration Tests (`internal/coreapi/marketplace_cart_integration_test.go`)
- `TestMarketplaceCartHandoffIntegration_FullFlow`:
  - New cart created when no token is provided.
  - Existing token reused when adding subsequent items for the same store.
  - Cross-store item addition rejected with 409 Conflict (`CodeConflict`).
  - Cross-market item addition rejected with 409 Conflict (`CodeMarketMismatch`).
  - Unpublished listings rejected with 409 Conflict without mutating cart state.
  - Out of stock listings rejected with 409 Conflict without creating a cart.
  - End-to-end checkout finalization attaches immutable marketplace order attribution.
  - Idempotent finalization replay returns original order and attribution.

### 3. Toolchain Verification
- `gofmt -s -w .`: Passed with zero diffs.
- `go vet ./...`: Passed with zero warnings.
- `go run ./cmd/openapi-gen`: Generated updated OpenAPI schema in `docs/api/internal/openapi.json`.
- `git diff --check`: Clean.
- Phase name leakage audit: Clean (no phase identifiers introduced).
