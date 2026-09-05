# Phase 5.7 Implementation Report — Storefront Cart, Checkout & Secure Guest Orders (Core)

**Base SHA**: `1ee5828e381b3d9e994c5844669d87fe10960212`  
**Branch**: `feature/p5-7-storefront-transactions`  
**Repository**: `matjeroapps/core`  
**PR**: Coordinated PR 1 of 2  

---

## 1. Overview & Architecture

Phase 5.7 implements secure storefront guest order access and pending guest order cancellation capabilities in Core, completing the end-to-end purchasing vertical slice for storefront clients.

Core remains the sole commerce and business authority per **ADR-017**:
- No browser communicates directly with Core.
- Core endpoints are internal and require authenticated Seller service identity (`X-Matjero-Service: seller`).
- Guest capabilities are presented over dedicated internal header `X-Matjero-Guest-Order-Token`.

---

## 2. Core API Surface Additions

Added dedicated internal storefront guest order operations to `core/internal/coreapi/storefront.go`:

```http
GET  /internal/v1/storefront/orders/{orderID}
POST /internal/v1/storefront/orders/{orderID}/cancel
```

### Authorization Header
- `X-Matjero-Guest-Order-Token`: Passes the raw guest capability token from the Seller proxy to Core.
- Combined with standard service authentication (`X-Matjero-Service: seller`, host header).

---

## 3. Host + Capability Verification Algorithm

Every guest Order read or cancellation strictly verifies dual authorization:
1. **Store Resolution**: Trusted `Host` header resolves to `store_id` via `StoreResolver`. The target `orderID` MUST belong to the resolved `store_id`.
2. **Constant-Time Capability Digest Verification**:
   - Raw token from `X-Matjero-Guest-Order-Token` is hashed via `SHA-256`.
   - Resulting digest is compared against `orders.guest_order_access_token_digest` using `subtle.ConstantTimeCompare`.
   - Raw tokens are never stored, logged, or returned in response payloads.

### Error Boundaries
- Missing/invalid capability token -> `401 Unauthorized` (`unauthorized`).
- Order belonging to another Store -> `404 Not Found` (`not_found`) to prevent cross-tenant existence disclosure.

---

## 4. Pending Guest Cancellation Lifecycle

Implemented capability-authorized `CancelGuestOrder` in `core/modules/commerce`:
- **State Machine Integration**: Uses canonical state machine transitions rather than ad-hoc SQL updates.
- **PENDING Only**:
  - `pending` -> `cancelled` (releases held inventory reservations exactly once, emits `OrderStatusChanged` outbox event).
  - `cancelled` -> `cancelled` (idempotent 200 response with cancelled order state).
  - `confirmed` / later status -> `400 Bad Request` (`invalid_order_transition`).
- **Inventory Safety**: Releases reserved inventory using deterministic lock ordering to preserve P5.4 invariants.

---

## 5. Canonical Listing & Availability Authority

- **Newest Canonical Listing**: Verified shared rule (`ORDER BY created_at DESC, id DESC`) in `modules/catalog/selection.go`.
- **Display & Cart Continuity**: Retail price and availability rendered on product detail page match Add-to-Cart SKU resolution.
- **Add-to-Cart Race Re-snapshot**: If a newer canonical listing becomes active before Add-to-Cart, Core re-snapshots the current canonical listing without rejecting with `price_changed` at Add-to-Cart.
- **Source-Aware Availability**:
  - Supplier-backed listings count only active supplier fulfillment locations for that offer/market.
  - Store-owned listings count only store-owned active locations.
- **No Cross-Listing Projection**: Price, source, and inventory belong strictly to the same canonical listing.

---

## 6. Verification Evidence & Test Coverage

### Core Unit & Integration Matrix
- `TestP57GuestOrderReadAndCancel`: Verifies Matrix 32–40 (correct Store + capability reads, wrong host rejection, wrong token rejection, UUID-only rejection, order-number rejection, pending cancellation, confirmed cancellation rejection, cancellation retry idempotency, capability digest comparison, response privacy).
- Matrix 108–111 & 128–136 verified across `modules/storefront`, `modules/commerce`, and `internal/coreapi`.

### Validation Suite
- `gofmt` & `go vet`: 100% clean.
- `go test ./...`: 100% green.
- OpenAPI internal spec updated via `go run ./cmd/openapi-gen`.
- Migration changes: NONE required (reused P5.1-P5.6 database schema).
