# ADR-019: Supplier Retail Capability and Explicit Supplier-Seller Affiliation

## Status

Accepted with partial supersession by ADR-020

## Supersession by ADR-020

ADR-020 makes Merchant the canonical business and authorization tenant while preserving Retail and Supply as independent capabilities. This section records the precise scope of supersession without deleting or rewriting ADR-019's historical decisions.

- **Decision 1 — partially superseded**: The conclusion that Supplier/Supply and Seller/Retail are capabilities remains active. The `Supplier -> Seller` hierarchy is superseded as the canonical business and identity model; both profiles instead link to Merchant.
- **Decision 2 — active**: Stores remain strictly Seller-owned through `stores.seller_id`.
- **Decision 3 — partially superseded**: The prohibition on inferred identity remains active. `supplier_seller_affiliations` is superseded as the canonical identity mechanism and remains only compatibility and explicit backfill evidence until separately approved cleanup.
- **Decision 4 — active**: Core remains the single authority, and Supplier does not call Seller to execute Core business capabilities.
- **Decision 5 — partially superseded**: Owner-governed atomic Retail activation remains active. Creating a separate Seller membership and affiliation as the target identity/membership model is superseded by Merchant membership, capability state, and explicit Merchant profile linkage. The existing transaction remains valid compatibility behavior until the replacement is implemented and cut over.
- **Decision 6 — superseded for canonical membership**: Its behavior remains historical truth for already provisioned profiles, but separate Seller membership is not the target authorization model. Unified Merchant membership and capability-scoped permissions replace it after gated cutover.
- **Decisions 7, 8, 9, and 10 — active**: Listing offer lineage, derived sourcing semantics, own-products-first internal prioritization, public storefront privacy, and the Core access boundary remain in force.

ADR-020 does not authorize removal of legacy memberships, affiliations, profiles, or compatibility routes. Their retirement requires successful backfill, canonical cutover, rollback readiness, the compatibility observation window, and separate approval.

## Context

In the Matjero commerce platform, business entities operate as Suppliers (wholesalers / product source owners) and/or Sellers (retail merchants operating consumer-facing storefronts).

Previously, system capabilities assumed a business entity operated exclusively as either a Supplier or a Seller. However, real-world business models frequently require a Supplier to operate direct-to-consumer retail stores while remaining a wholesale supplier on the platform. Furthermore, a Supplier-operated retail store may sell both its own products and products sourced from other platform Suppliers.

To support direct retail operations by Suppliers without compromising tenant boundaries, platform security, or data integrity, Core must establish a canonical architecture for retail capabilities and explicit affiliations.

## Decisions

### 1. Capabilities vs Account Types

Supplier and Seller are **capabilities**, not mutually exclusive account types.

A single business entity may possess:
- Supplier capability (wholesaling, catalog management, offer creation, fulfillment)
- Seller / Retail capability (storefront management, custom domains, retail listings)

The canonical ownership and capability link is:

```
Supplier
   ↓ (explicit 1:1 affiliation)
Seller
   ↓
Store
```

### 2. Store Ownership Invariant

`Store` records remain strictly owned by `Seller`:

- `Store` → `seller_id`

**Invariants**:
- Never introduce `store.supplier_id`.
- Never make `stores` polymorphic (e.g. `seller_id OR supplier_id`).
- Stores remain 100% seller-owned database entities.

### 3. Explicit Supplier ↔ Seller Affiliation

Affiliation between a Supplier profile and a Seller profile MUST be explicit, persisted, and Core-owned in PostgreSQL (`supplier_seller_affiliations`).

**Critical Identity Rule**:
Core MUST NOT infer Supplier ↔ Seller affiliation from shared attributes such as:
- Same ZITADEL subject
- Same email address
- Same company name
- Same code
- Same owner or member lists

The affiliation table enforces a strict **1:1 relationship**:
- One Supplier profile ↔ at most one retail Seller profile.
- One Seller profile ↔ at most one Supplier profile.

### 4. Core as the Single Authority & Supplier Service Boundary

The Supplier service NEVER calls the Seller service to execute Core business capabilities. Core is the authoritative domain and data boundary.

Core provides dedicated Supplier-scoped internal APIs (`/internal/v1/suppliers/{supplierID}/...`) restricted exclusively to `serviceauth.CallerSupplier` for self-service retail capabilities and stores. Admin retail linking and moderation are deferred to explicit future Admin contracts.

### 5. Atomic Retail Capability Provisioning & Owner Governance

Retail capability provisioning is an **OWNER-level Supplier governance action**. Only an active Supplier member with `role = owner` and `status = active` may provision the retail capability. Active non-owner members (e.g., manager, viewer) are strictly forbidden from creating the linked Seller capability.

Provisioning MUST execute as a single atomic PostgreSQL transaction creating:
- `sellers` (Seller profile)
- `seller_settings` (Default seller settings)
- `seller_members` (Initial owner membership)
- `supplier_seller_affiliations` (Explicit 1:1 link)

If any operation fails (e.g., affiliation uniqueness conflict), the transaction rolls back completely, leaving no orphan Seller or SellerMember records.

### 6. Initial Seller Membership & Isolation

The authenticated Supplier OWNER subject that explicitly enables the retail capability becomes an active Seller member (`role = owner`, `status = active`) for the new Seller profile.

Supplier membership is NOT implicitly converted into Seller membership. Existing `supplier_members` rows are NOT copied into `seller_members`.

### 7. Explicit Listing Source Semantics & Derivation

The product flow for any storefront listing follows:

```
Store
  ↓
Seller Listing
  ↓
Supplier Offer
  ↓
Supplier
```

Source classification is derived at query time based on persisted relations:
- `source Supplier == affiliated Supplier` → **OWN** product/source
- `source Supplier != affiliated Supplier` → **NETWORK** product/source

**Invariants**:
- Do NOT persist `source_type` (e.g. `own` or `network`) as duplicated state.
- For Supplier-operated retail stores, selling own goods still references `supplier_offer_id`. `NULL` `supplier_offer_id` remains reserved for future seller-owned inventory models.
- Listing source integrity is strictly enforced: `seller_listing.supplier_offer_id` MUST point to an offer for the exact same `product_id` as the `seller_listing.product_id`.

### 8. Own-Products-First Sourcing Semantics

For Supplier sourcing and catalog discovery interfaces:
- Offers from the affiliated Supplier are prioritized before Network Supplier offers:
  ```sql
  CASE
    WHEN supplier_offer.supplier_id = affiliated_supplier_id THEN 0
    ELSE 1
  END
  ```
- This prioritization rule applies **ONLY** to Supplier internal sourcing and discovery tools.
- It does **NOT** alter public storefront product ranking, search relevance, or consumer-visible sorting.

### 9. Public Storefront Privacy

Customer-facing Storefront APIs MUST NOT expose internal sourcing metadata, including:
- Supplier identity or affiliation
- OWN vs NETWORK classification
- `supplier_offer_id`
- Wholesale pricing or supplier margins

### 10. Domain Management Future Compatibility

Supplier custom domain management will be executed via Core's reusable access primitive:
`RequireSupplierRetailStoreAccess(ctx, subject, supplierID, storeID)`.

This ensures future Supplier domain workflows remain direct from `supplier-api` → Core without service-to-service calls to `seller-api`.
