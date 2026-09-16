# Seller Catalog Operations Integration — Implementation Design and Plan

**Status:** Approved for implementation

**Date:** 2026-09-16

**Owners:** Core, Seller, Platform Infrastructure
**Primary boundary:** Core owns catalog state and invariants; Seller owns authenticated HTTP presentation and the seller-facing web experience.

## 1. Outcome

Deliver one production path for a seller to select an owned store and operate its catalog end to end:

1. create or import a product into a store;
2. manage variants and SKUs;
3. upload or reuse store-owned media;
4. set retail price, presentation, and inventory;
5. evaluate readiness and publish or unpublish a listing; and
6. observe the published result in the canonical storefront.

Every operation is explicitly store-scoped. A seller may own multiple stores, while Core enforces a configurable entitlement whose default permits one active store. The implementation extends the existing Core `/internal/v1` API and Seller `/v1/seller` API rather than creating another business backend.

The canonical customer storefront remains `seller/web/storefront`. The older mock storefront under `seller/web/seller/app/store/[slug]` is deprecated and removed after equivalent navigation is directed to the canonical storefront.

## 2. Current Baseline

The repository already provides these foundations, which this plan must extend rather than duplicate:

- Core stores, seller membership, store domains, seller-owned products, variants, SKUs, seller listings, listing prices, inventory snapshots, structured listing presentation, publish readiness, publish/unpublish transactions, and storefront revision invalidation.
- Core internal store routes under `/internal/v1/stores/{storeID}/...` and service authentication using `Authorization`, `X-Matjero-Service`, and `X-Matjero-Subject`.
- Seller API catalog operations under `/v1/seller/stores/{store_id}/...`, plus legacy query-scoped catalog and listing routes.
- A presigned S3-compatible upload flow backed by product-scoped `media_metadata` and `media_upload_intents`.
- Seller roles `seller_owner`, `seller_manager`, and `seller_staff`, with active seller membership stored in Core.
- The canonical multi-tenant storefront in `seller/web/storefront` and a separate mock storefront inside `seller/web/seller`.
- Local Postgres, Redis, RabbitMQ, ZITADEL, MinIO, Core, Seller API/web, and Storefront API/web services in `platform-infra/docker-compose.yml`.

The current media implementation is not the target library: an object belongs directly to one product, deduplication is by storage key rather than content checksum, and deleting product media can immediately delete the object. The new model separates immutable store assets from their reusable product references.

## 3. Scope

### Included

- Core `StoreEntitlementPolicy`, configured with a default active-store limit of `1`.
- Multiple owned stores in the data model and Seller store switcher.
- Explicit store scope in Seller page URLs and Seller API routes.
- A Core-owned, store-scoped, immutable media library with per-store SHA-256 deduplication, reusable references, safe deletion, and a presigned MinIO/S3 flow.
- Seller-owned product authoring, variants, SKUs, listing price and presentation, inventory, readiness, publish, and unpublish.
- Supplier-offer discovery, idempotent import, store-specific merchandising, readiness, publish, unpublish, and withdrawal behavior.
- Role and resource authorization at both the Seller boundary and Core business boundary.
- Seller and Core OpenAPI contract updates.
- Canonical storefront verification and removal of the duplicate mock storefront.
- Docker Compose end-to-end verification against real Core, Postgres, and MinIO services.

### Excluded

- Billing, subscription charging, invoicing, plan checkout, and payment-provider work.
- Customer payments, seller settlement, marketplace financial allocation, and ledger changes.
- Fulfillment, shipment, carrier, warehouse-routing, and order-lifecycle changes.
- Kubernetes manifests, Helm charts, operators, or cluster deployment design.
- Image transformation, transcoding, virus scanning, CDN provisioning, and DAM-style folders/tags.
- External seller-channel connectors in `seller-hub`.
- Supplier product-authoring changes; this plan only consumes existing eligible supplier offers.

The active-store limit is an entitlement check, not billing logic. This phase configures the limit but does not sell or charge for a higher limit.

## 4. Architecture and Ownership

```text
Seller browser
  -> seller/web/seller
  -> Seller API /v1/seller/stores/{store_id}/...
       - validates OIDC session and coarse role
       - forwards only trusted subject context
  -> Core /internal/v1/stores/{storeID}/...
       - resolves active seller member from subject
       - verifies store ownership and operation permission
       - applies entitlement and catalog invariants
       - persists PostgreSQL state and outbox work
  -> MinIO/S3
       - browser uploads through a short-lived presigned PUT
       - Core verifies the completed object and owns deletion

Customer browser
  -> seller/web/storefront (canonical)
  -> Storefront API
  -> Core storefront read model
```

Core is the only component allowed to decide whether a store can activate, an offer can be imported, an asset can be attached or deleted, or a listing can publish. Seller API performs transport validation and early role denial, but it never recreates Core business rules. Seller web does not call Core or MinIO credentials directly; it calls Seller API and uses only the returned presigned URL and required headers.

No new calls are introduced from Seller to the Core database. No Core Go packages are imported into Seller. Contract DTOs remain separately owned on each side of the HTTP boundary.

## 5. Store Entitlement and Store Selection

### 5.1 Core policy

Add a Core `StoreEntitlementPolicy` dependency to the commerce service. Its initial implementation has one configuration value, `DefaultMaxActiveStores`, loaded from `STORE_DEFAULT_MAX_ACTIVE_STORES` with a default of `1`; startup rejects values below `1`.

The policy answers whether a seller may transition a store into `active`. It counts active stores for the seller in the same transaction that locks the seller's store set and changes status. Creation with `status=active` and later activation use this same primitive. Creating additional non-active stores remains possible so a seller can prepare a replacement without bypassing the active-store entitlement.

The design supports multi-store without schema coupling to a pricing plan: raising the configured value permits more simultaneously active stores. A later billing project may provide a different policy implementation, but this phase does not add plan tables or billing calls.

Required behavior:

- The default configuration permits one active store per seller.
- Re-saving an already active store is idempotent and does not consume another slot.
- Deactivating one store releases a slot.
- Concurrent activations cannot exceed the limit; the transaction locks the seller row or uses an equivalent per-seller advisory lock before counting.
- Admin moderation may force a store inactive, but activation still goes through the policy.
- An entitlement denial returns `409 store_entitlement_exceeded` and includes no information about another seller.
- Supplier-retail stores use the same seller-backed policy because they are persisted as stores owned by the affiliated seller.

### 5.2 Store collection and switcher

`GET /v1/seller/stores` remains the switcher source. Extend its response to include `active_store_limit` and `active_store_count` alongside `items`, so the UI can explain why activation or creation-as-active is unavailable. Core computes these values; Seller does not infer them.

The switcher stores only a convenience preference in a cookie or local storage. It is never authorization input. The selected store is always present in the route, and Core re-authorizes it on every request. Selection rules are deterministic:

1. keep the route's store when it is still in the returned owned-store list;
2. otherwise select the first active store ordered by creation time and ID;
3. otherwise select the first owned store; and
4. show onboarding when the seller owns no store.

Target Seller page routes are:

```text
/dashboard/stores/{store_id}
/dashboard/stores/{store_id}/catalog/products
/dashboard/stores/{store_id}/catalog/products/new
/dashboard/stores/{store_id}/catalog/products/{product_id}
/dashboard/stores/{store_id}/catalog/supplier-offers
/dashboard/stores/{store_id}/catalog/listings/{listing_id}
/dashboard/stores/{store_id}/inventory
/dashboard/stores/{store_id}/media
/dashboard/stores/{store_id}/storefront
```

Existing unscoped dashboard links such as `/dashboard/catalog/products` become redirects that choose a store by the rules above. They do not remain independent feature routes.

## 6. Authorization Model

Authentication and authorization are separate layers:

- Seller API validates the end-user token and requires one of the existing seller roles.
- Seller API forwards the verified subject over its authenticated service connection.
- Core resolves the active `seller_members` record, checks the member's persisted role, resolves the store from the path, and verifies that the store belongs to that seller.
- Cross-seller identifiers return `404 not_found`, preventing tenant enumeration.

The Core membership role, not a forwarded browser role header, is authoritative for business permission. Do not add a client-settable role header.

| Capability | owner | manager | staff |
|---|---:|---:|---:|
| View stores, products, listings, media, inventory, readiness | yes | yes | yes |
| Create/edit products, variants, SKUs, media references, prices, presentation | yes | yes | no |
| Adjust inventory | yes | yes | yes |
| Import supplier offer | yes | yes | no |
| Publish/unpublish listing | yes | yes | no |
| Create store or change store status | yes | no | no |
| Permanently delete an unreferenced media asset | yes | yes | no |

Seller API applies the same matrix for fast rejection and UI capability flags, while Core repeats the decisive check from persisted membership. A missing or inactive member returns `403 forbidden`; a valid member requesting another seller's store receives `404 not_found`.

All mutations write the authenticated subject and request correlation ID to existing audit/event fields where available. Media deletion jobs also record the requesting subject.

## 7. Core Data Model

Use a forward-only migration after the current migration set. Final migration numbering is chosen at implementation time from the repository head.

### 7.1 Media assets

Create `store_media_assets`:

| Column | Contract |
|---|---|
| `id` | UUID primary key |
| `store_id` | required FK to `stores`, delete restricted |
| `checksum_sha256` | lowercase 64-character hex digest of verified bytes |
| `storage_key` | immutable object key, unique |
| `content_type` | verified allowlisted MIME type |
| `byte_size` | verified positive size |
| `original_filename` | sanitized display metadata, never part of the object key |
| `status` | `ready`, `deleting`, or `deleted` |
| `created_by_subject` | audit subject |
| timestamps | creation and lifecycle timestamps |

Content fields are immutable after the asset becomes `ready`. Only lifecycle status and deletion timestamps may change. A partial unique index on `(store_id, checksum_sha256)` for `ready` and `deleting` rows enforces per-store deduplication. The same bytes in different stores produce separate rows and keys; deduplication never crosses a tenant boundary.

Create `product_media_references`:

| Column | Contract |
|---|---|
| `id` | UUID primary key; this is the media ID used by presentation sections |
| `store_id` | required FK used for tenant checks and indexes |
| `product_id` | required FK to product |
| `asset_id` | required FK to `store_media_assets`, delete restricted |
| `alt_text` | reference-specific localized/display text |
| `sort_order` | reference ordering |
| `is_primary` | reference-specific primary flag |
| timestamps | mutable reference timestamps |

Enforce one reference for `(store_id, product_id, asset_id)` and at most one primary reference per `(store_id, product_id)`. A constraint trigger or transaction-level validation must prove that the asset and listing/product context belong to the same store. The public storefront projects the reference ID, resolved asset URI, alt text, ordering, and primary flag; it never exposes storage keys or checksums.

Evolve `media_upload_intents` to bind `store_id`, expected SHA-256, expected size, content type, storage key, token digest, expiry, and completion. An intent does not count as a reusable asset until completion verifies the object.

Backfill existing product media only when its store ownership is unambiguous from a seller listing. Rows that cannot be mapped safely fail the migration before destructive changes. Create an asset and reference for each legacy row, preserving media IDs used by structured presentation. Delay removal of legacy columns/tables until the new read path and backfill checks pass; no dual-write period extends beyond this integration.

### 7.2 Listing lifecycle

Keep `seller_listings` as the store-specific sellable aggregate for both sources:

- `supplier_offer_id IS NULL` means `seller_owned` and requires the product's `seller_products` owner to match the store's seller.
- `supplier_offer_id IS NOT NULL` means `supplier_backed` and requires product, offer, and store market consistency.

Add only the indexes or import-idempotency constraint needed to guarantee one canonical listing per `(store_id, product_id)` and one imported occurrence of `(store_id, supplier_offer_id)`. Existing duplicates, if any, must be detected and resolved explicitly before the unique index is installed.

Do not copy supplier products, variants, or SKUs into seller-owned rows during import. Store-specific price, presentation, status, and optional store-media references belong to the listing. Supplier-owned product facts remain read-only to the seller.

## 8. Immutable Media Library and MinIO Flow

### 8.1 Browse and reuse

`GET /internal/v1/stores/{storeID}/media` lists only `ready` assets for the authorized store, with pagination and optional filename/content-type query filters. `POST /internal/v1/stores/{storeID}/products/{productID}/media-references` attaches an existing asset to an eligible product/listing in that store. Attaching reuses bytes without copying an object.

Alt text, sort order, and primary selection are properties of the reference and may be edited. Asset checksum, bytes, MIME type, object key, and owning store cannot be edited.

### 8.2 Presign

The browser computes SHA-256 and sends:

```json
{
  "filename": "front.webp",
  "content_type": "image/webp",
  "size_bytes": 184321,
  "checksum_sha256": "<64 lowercase hex characters>"
}
```

Core validates the MIME allowlist, positive size and configured maximum, checksum shape, store permission, and product/listing eligibility. It then checks the per-store checksum index:

- If a `ready` asset exists, return `200` with `mode: "reuse"` and the asset. No upload URL is issued.
- If another unexpired intent for the same store and checksum exists, return `409 upload_in_progress` so clients do not race duplicate uploads.
- Otherwise create an intent and return `201` with `mode: "upload"`, a short-lived presigned PUT URL, opaque completion token, storage key, expiry, and the exact required request headers.

The storage key is generated by Core and includes the store ID plus random material; it never uses the raw filename. The bucket stays private. Normal media delivery uses the configured public/CDN base URL or a separate read policy, not the Seller API.

### 8.3 Upload and completion

The browser PUTs directly to MinIO with the signed content type and checksum header. It then posts the storage key and opaque completion token to the completion endpoint.

Core completion:

1. loads the intent and validates store, subject, expiry, completion token, and single-use state;
2. performs `HeadObject` and validates existence, size, and content type;
3. reads the bounded object once from MinIO and computes SHA-256 over the actual bytes; a client-provided metadata value alone is not trusted;
4. rejects and schedules deletion when the digest or size differs;
5. in one database transaction, inserts the immutable asset, marks the intent complete, and optionally creates the first product reference;
6. on a concurrent checksum conflict, reuses the winning store asset and schedules the losing object for deletion; and
7. returns the asset and reference. Repeating completion returns the same result.

This one verification read is part of upload ingestion; Core still does not proxy routine storefront media delivery.

### 8.4 Safe detach and deletion

Deleting product media means deleting a reference, not immediately deleting the asset. If the removed reference was primary, the transaction promotes the lowest `(sort_order, id)` remaining reference. Publish readiness is recalculated from remaining references.

Permanent library deletion is a separate `DELETE /stores/{storeID}/media/{assetID}` operation:

1. lock the asset row;
2. reject with `409 media_in_use` when any product reference exists;
3. atomically change `ready -> deleting` and enqueue an outbox-backed deletion job;
4. return `202`;
5. the existing general-worker process deletes the MinIO object idempotently and marks the row `deleted`; and
6. retry transient storage failures without returning the asset to `ready` or allowing new references.

`404` from object storage is treated as successful deletion. A `deleting` or `deleted` asset cannot be attached. Tombstones retain audit metadata but are omitted from normal list responses. A partial checksum uniqueness rule permits a fresh asset after the prior row reaches `deleted`.

Expired or failed upload intents and their orphan objects are cleaned by the same worker path. Do not use an untracked best-effort delete after removing the database row.

## 9. Catalog Lifecycles

### 9.1 Seller-owned product

1. `POST /stores/{storeID}/products` atomically creates the global product, seller ownership row, and draft store listing.
2. The seller edits translations/categories and creates variants and SKUs.
3. The seller uploads or reuses store media and attaches references.
4. The seller creates a store fulfillment location if needed and creates/adjusts inventory snapshots for active SKUs.
5. The seller sets the listing price and presentation.
6. `GET /stores/{storeID}/listings/{listingID}/readiness` returns structured blocking codes plus display-safe messages.
7. `POST /stores/{storeID}/listings/{listingID}/publish` repeats authoritative readiness checks inside a locking transaction, activates product/listing as appropriate, and bumps the storefront revision.
8. Unpublish deactivates the listing and bumps the revision without deleting authoring state.

Variants and SKUs remain product resources but are reachable only beneath the store and product that authorize them. IDs from another product or store return `404`.

### 9.2 Supplier-offer import

1. `GET /stores/{storeID}/supplier-offers` returns only active, available offers in the store market. It supports supplier, category, query, limit, and offset filters.
2. `POST /stores/{storeID}/supplier-offers/{offerID}/imports` validates offer/product/market consistency and creates one draft seller listing. Repeating the request returns the existing listing rather than duplicating it.
3. Supplier product facts, variants, and SKUs are read-only. The seller may set the store retail price, listing presentation, and store-specific media references without modifying the supplier's product.
4. Readiness requires an active store, eligible active offer, available supplier inventory, current supplier price where required by sourcing, current seller retail price in the store currency, at least one display media reference (store-specific or eligible supplier media), valid presentation, and at least one active/selectable supplier SKU.
5. Publishing activates only the seller listing, then bumps the storefront revision. It does not change supplier product or offer status.
6. If an offer becomes inactive, unavailable, market-incompatible, or loses sellable supplier inventory, storefront eligibility fails closed even before asynchronous cleanup. An event-driven projection may also mark the listing unavailable and bump the revision, but correctness must not depend on event timing.
7. Unpublish preserves the import and merchandising state. A separate remove-import operation is not part of this phase.

The unified listing publish contract replaces product-specific publish as the preferred contract. Existing product publish/unpublish endpoints remain as temporary compatibility shims for seller-owned products, delegate to the listing service, are marked deprecated in OpenAPI, and are removed only after Seller web no longer calls them.

## 10. API Contracts

All routes below are target contracts. Routes already present keep compatible response fields unless this plan explicitly marks them deprecated.

### 10.1 Core internal API

| Method and path | Purpose | Roles |
|---|---|---|
| `GET /internal/v1/sellers/{sellerID}/stores` | owned stores plus entitlement summary | owner, manager, staff |
| `POST /internal/v1/sellers/{sellerID}/stores` | create store; active creation uses entitlement policy | owner |
| `POST /internal/v1/stores/{storeID}/status` | seller store status transition through policy | owner |
| `GET /internal/v1/stores/{storeID}/products` | list store catalog across both sources | owner, manager, staff |
| `POST /internal/v1/stores/{storeID}/products` | create seller-owned draft product/listing | owner, manager |
| existing nested variant/SKU routes | author seller-owned variants/SKUs | owner, manager |
| `GET /internal/v1/stores/{storeID}/supplier-offers` | browse eligible supplier offers | owner, manager, staff |
| `POST /internal/v1/stores/{storeID}/supplier-offers/{offerID}/imports` | idempotently create supplier-backed draft listing | owner, manager |
| `GET /internal/v1/stores/{storeID}/listings` | list store listings | owner, manager, staff |
| `GET /internal/v1/stores/{storeID}/listings/{listingID}` | source-aware authoring detail | owner, manager, staff |
| `PUT /internal/v1/stores/{storeID}/listings/{listingID}/price` | replace current retail price | owner, manager |
| existing presentation GET/PUT routes | read/update structured presentation | read all; write owner/manager |
| `GET /internal/v1/stores/{storeID}/listings/{listingID}/readiness` | structured readiness | owner, manager, staff |
| `POST /internal/v1/stores/{storeID}/listings/{listingID}/publish` | atomic source-aware publish | owner, manager |
| `POST /internal/v1/stores/{storeID}/listings/{listingID}/unpublish` | atomic unpublish | owner, manager |
| `GET /internal/v1/stores/{storeID}/media` | list ready store assets | owner, manager, staff |
| `POST /internal/v1/stores/{storeID}/media/uploads` | dedup lookup or presign | owner, manager |
| `POST /internal/v1/stores/{storeID}/media/uploads/{intentID}/complete` | verify object and create/reuse asset | owner, manager |
| `DELETE /internal/v1/stores/{storeID}/media/{assetID}` | enqueue safe permanent deletion | owner, manager |
| `POST /internal/v1/stores/{storeID}/products/{productID}/media-references` | attach reusable asset | owner, manager |
| `PUT /internal/v1/stores/{storeID}/products/{productID}/media-references/{referenceID}` | edit reference metadata | owner, manager |
| `DELETE /internal/v1/stores/{storeID}/products/{productID}/media-references/{referenceID}` | detach asset | owner, manager |
| existing store inventory routes | list/snapshot/adjust store inventory | read all; adjust all roles |

For listing price, presentation, publish, and media reference routes, Core verifies that `{listingID}`, `{productID}`, `{assetID}`, and nested IDs all resolve within `{storeID}`. Body fields cannot override path scope.

### 10.2 Seller public API boundary

Seller exposes the same resource hierarchy under `/v1/seller` and maps DTOs explicitly. The browser never receives Core service credentials, upload token digests, raw storage errors, storage keys after completion, supplier cost, or internal inventory reservation fields.

Legacy routes are replaced as follows:

| Deprecated | Replacement |
|---|---|
| `GET /v1/seller/catalog/offers?store_id=...` | `GET /v1/seller/stores/{store_id}/supplier-offers` |
| `GET /v1/seller/listings?store_id=...` | `GET /v1/seller/stores/{store_id}/listings` |
| `POST /v1/seller/listings/import` with `store_id` in body | `POST /v1/seller/stores/{store_id}/supplier-offers/{offer_id}/imports` |
| `POST /v1/seller/listings/{id}/price` | `PUT /v1/seller/stores/{store_id}/listings/{listing_id}/price` |
| `POST /v1/seller/listings/{id}/status` | explicit store-scoped publish/unpublish endpoints |

Deprecated routes may remain for one compatibility release, but they must delegate to the store-scoped application service, emit deprecation metadata, and accept no wider authorization behavior. Seller web switches atomically to replacements before removal.

### 10.3 Response shapes

Readiness is machine-readable:

```json
{
  "is_ready": false,
  "reasons": [
    {"code": "retail_price_missing", "message": "Current retail price is required"},
    {"code": "media_missing", "message": "At least one product image is required"}
  ]
}
```

Media presign/reuse is a discriminated response:

```json
{
  "mode": "upload",
  "intent_id": "...",
  "upload_url": "...",
  "upload_token": "...",
  "required_headers": {
    "Content-Type": "image/webp",
    "x-amz-checksum-sha256": "..."
  },
  "expires_at": "..."
}
```

or:

```json
{
  "mode": "reuse",
  "asset": {
    "id": "...",
    "content_type": "image/webp",
    "byte_size": 184321,
    "url": "..."
  }
}
```

The reuse response intentionally omits another product's alt text and ordering because those belong to references.

## 11. Errors and Concurrency

Extend the closed Core-to-Seller error vocabulary and map it consistently:

| Code | HTTP | Meaning |
|---|---:|---|
| `validation_error` | 400 | malformed field, unsupported MIME, invalid transition input |
| `unauthorized` | 401 | invalid service or end-user authentication |
| `forbidden` | 403 | authenticated member lacks the operation role |
| `not_found` | 404 | resource absent or outside the caller's seller/store scope |
| `conflict` | 409 | general uniqueness or state conflict |
| `store_entitlement_exceeded` | 409 | active-store limit would be exceeded |
| `upload_in_progress` | 409 | same store/checksum already has a live intent |
| `checksum_mismatch` | 422 | uploaded bytes do not match the declared digest |
| `media_in_use` | 409 | permanent deletion requested while references exist |
| `publish_not_ready` | 422 | listing readiness failed; response includes structured reasons |
| `offer_unavailable` | 409 | supplier offer cannot be imported or published |
| `service_unavailable` | 503 | Core or object storage is unavailable |

Unexpected Core errors and MinIO details collapse to safe public messages. Logs may include correlation IDs and internal causes, but never service tokens, upload tokens, signed URLs, or object credentials.

Use database uniqueness and locks as the final concurrency authority:

- per-seller lock for active-store transitions;
- unique store/checksum index for asset deduplication;
- unique product/asset reference index;
- unique store/offer import index;
- row locks plus readiness re-evaluation for publish; and
- asset row lock for attach-versus-delete races.

Endpoints whose retries are expected—supplier import, upload completion, attach, publish, and unpublish—return the existing resulting resource when the requested state already exists. Payload-changing retries against the same idempotency identity return `409`.

## 12. Seller Web and Canonical Storefront

Implement the seller catalog UI only in `seller/web/seller` and customer rendering only in `seller/web/storefront`.

Seller web work:

- load owned stores and entitlement summary after authentication;
- add a store switcher to the dashboard shell;
- move catalog navigation to `/dashboard/stores/{store_id}/...`;
- add source-aware product/listing views (`seller_owned` and `supplier_backed`);
- provide seller-owned product editing for translations, categories, variants, SKUs, media, price, inventory, and presentation;
- provide supplier-offer browsing/import and lock supplier-owned facts read-only;
- provide media-library upload/reuse, detach, and safe-delete states;
- show structured readiness reasons and role-aware actions; and
- link storefront preview/live navigation to the host returned by the existing store storefront-host endpoint.

Role-based hiding is usability only; APIs remain authoritative. Switching stores cancels in-flight queries, clears store-keyed client cache, and navigates to a route containing the new store ID. Query keys include store ID to prevent data from the prior store flashing in the new context.

Canonical storefront work is limited to consuming the final listing/media projection and proving that publish/unpublish and store revision changes appear correctly. Catalog authoring logic does not move into storefront code.

Deprecate the duplicate mock storefront by:

1. removing navigation and links to `/store/[slug]` from Seller web;
2. replacing any required local preview link with the canonical storefront host;
3. deleting `seller/web/seller/app/store/[slug]`, `seller/web/seller/components/storefront`, `seller/web/seller/lib/storefront/mock-data.ts`, and their mock-only tests after coverage exists in canonical storefront tests; and
4. updating documentation that still describes the embedded mock as a supported storefront.

Do not modify `seller-hub`; it remains reserved for external channel connectors.

## 13. Security Requirements

- Keep MinIO private; expose only short-lived PUT capability URLs and public/CDN read URLs intended for storefront use.
- Presigned URLs are bound to one generated key, content type, checksum, size policy, and short expiry.
- Persist only a digest of the opaque completion token and compare it in constant time.
- Recompute checksum from bounded object bytes before accepting an asset.
- Sanitize filenames for display and response headers; never interpolate them into keys or filesystem paths.
- Allowlist supported image MIME types and verify actual upload metadata. File extension is not authoritative.
- Enforce request and response size limits in Seller and Core.
- Strip inbound `X-Matjero-*` identity headers at the public boundary; Seller coreclient sets trusted headers.
- Authorize every nested resource against the store path and return safe not-found on cross-tenant access.
- Never deduplicate across stores or reveal whether another store owns identical bytes.
- Do not expose supplier cost, storage key, checksum, internal subject, upload intent, or deletion-job data to the public storefront.
- Maintain storefront escaping and structured-presentation validation for all seller-supplied text.
- Rate-limit presign, completion, import, publish, and inventory adjustment operations at the Seller API boundary.

## 14. Implementation Sequence

### Phase A — Core store policy and authorization

1. Add configuration parsing/validation for the default active-store limit.
2. Add `StoreEntitlementPolicy` and atomic store activation service methods.
3. Resolve full active seller membership, including role, for catalog operations.
4. Apply the permission matrix to store and catalog services.
5. Add errors and Core internal mappings.
6. Add unit and Postgres concurrency tests.

### Phase B — Core media library

1. Add migrations for assets, references, upload-intent checksum fields, uniqueness, and deletion work.
2. Add safe legacy media backfill with invariant checks.
3. Add repository and service methods for list, dedup/presign, complete/verify, attach, update reference, detach, and delete.
4. Add MinIO get/hash support and outbox-backed deletion worker handling.
5. Change authoring and storefront projections to resolve media references.
6. Add migration, repository, service, API contract, race, and failure-retry tests.

### Phase C — Unified listing lifecycle

1. Add source-aware listing detail and structured readiness.
2. Add idempotent supplier-offer import and uniqueness.
3. Add unified listing price, publish, and unpublish services.
4. Re-evaluate source-specific readiness inside the publish transaction.
5. Keep product publish routes as documented compatibility shims.
6. Regenerate and verify Core OpenAPI.

### Phase D — Seller API boundary

1. Add store-scoped coreclient methods and DTOs.
2. Add Seller handlers and role middleware for each capability.
3. Deprecate query/body-scoped legacy routes without widening access.
4. Map every new Core error to a safe Seller error.
5. Regenerate Seller OpenAPI and add contract tests comparing method, path, status, and response shape.

### Phase E — Seller web and storefront consolidation

1. Add store bootstrap/switcher and explicit store route layout.
2. Implement source-aware products, supplier offers/import, listings, variants/SKUs, media, inventory, presentation, readiness, and publish UI.
3. Make action availability follow role/capability data while preserving API enforcement.
4. Point live/preview navigation to `seller/web/storefront`.
5. Remove the embedded mock storefront and its mock data/tests.
6. Verify canonical storefront media and listing projections.

### Phase F — Compose E2E and rollout

1. Extend `platform-infra/docker-compose.yml` or add a Compose E2E overlay using the existing services: Postgres, Redis, RabbitMQ, ZITADEL/test issuer, MinIO, Core API, general worker, Seller API/web, Storefront API/web, and a Playwright runner.
2. Add a deterministic seed/fixture command that creates users/members, two stores, seller and supplier catalog data, and MinIO bucket policy without bypassing Core invariants for the behavior under test.
3. Run migrations and health checks before tests; use service DNS names inside Compose.
4. Collect service logs and Playwright artifacts on failure and tear the project down with volumes in CI.
5. Roll out migrations and Core first, then Seller API, then Seller web/storefront. Remove compatibility routes only in a later release after access logs show no callers.

No Kubernetes work is required for this rollout.

## 15. Testing Strategy

### Core unit and integration

- Policy default, configured higher limit, invalid config, deactivate/reactivate, and concurrent activation.
- Owner/manager/staff matrix plus inactive membership and cross-seller safe-not-found cases.
- Per-store checksum dedup, cross-store non-dedup, duplicate completion, expired token, bad token, size/MIME/checksum mismatch, and upload race.
- Reuse of one asset across multiple products in the same store.
- Detach without object deletion, primary promotion, in-use delete rejection, attach/delete race, deletion retry, and missing-object success.
- Migration backfill preserves reference IDs used by presentation and fails on ambiguous ownership.
- Seller-owned draft through publish/unpublish, including concurrent readiness invalidation.
- Supplier import idempotency, cross-market rejection, offer withdrawal, supplier availability/inventory loss, retail pricing, and publish/unpublish.
- Storefront projections contain public URL/reference metadata but never checksum, storage key, supplier cost, or internal IDs that are not part of the public contract.

### Seller API contract

- Every route forwards the subject and exact store path to Core.
- Body/query store IDs cannot override path scope.
- All roles receive the intended status for every capability.
- New error codes map exactly; unknown Core codes collapse to `internal_error`.
- Signed URL and completion-token values are never logged or returned outside the upload response.
- OpenAPI includes all target paths and marks compatibility routes deprecated.

### Seller web

- Store selection fallback, switch navigation, store-keyed query cache, and zero-store onboarding.
- Direct navigation to an unowned store renders not-found without leaking store details.
- Seller-owned authoring and supplier-backed read-only distinctions.
- Media reuse does not perform a PUT; new upload performs PUT then completion.
- Role-gated actions, readiness reasons, publish result, and unpublish result.
- No routes or imports remain for the embedded mock storefront.

### Compose end to end

The mandatory Compose scenario uses real Core and MinIO, not `cmd/fake-core`:

1. owner signs in and sees store A selected;
2. owner creates seller product, variant, SKU, media upload, price, location, inventory, and presentation;
3. same asset is reused for a second product without a second MinIO object;
4. publish makes the first listing visible on the canonical storefront;
5. switch to store B proves catalog and media isolation;
6. default entitlement prevents activating store B while store A is active;
7. supplier offer is imported, merchandised, published, and visible;
8. offer withdrawal removes storefront eligibility;
9. unpublish removes the seller-owned listing after revision propagation;
10. referenced asset deletion fails, detaching all references allows queued deletion, and the worker removes the object; and
11. staff can adjust inventory but cannot import, edit catalog, publish, or delete an asset.

Run the existing Core Go tests, Seller Go tests, frontend unit/type checks, OpenAPI generation checks, migration up/down verification where supported, and Playwright Compose suite. Tests must not rely on sleep for correctness; wait on health, API state, revision, or worker outcome with bounded polling.

## 16. Acceptance Criteria

- [ ] A seller can own and switch among multiple stores, and every operational browser/API URL contains the store ID.
- [ ] Core defaults to one active store per seller and atomically enforces a configurable higher limit.
- [ ] Store selection never grants access; Core authorizes every request from persisted subject membership and store ownership.
- [ ] Role permissions match the owner/manager/staff matrix in both Seller API and Core.
- [ ] A seller-owned product can complete translations, variants, SKUs, media, price, inventory, presentation, readiness, publish, storefront visibility, and unpublish.
- [ ] An eligible supplier offer can be imported idempotently, merchandised per store, published, and made unavailable safely when its source becomes ineligible.
- [ ] Media bytes are represented by immutable Core-owned store assets with verified SHA-256, per-store deduplication, and reusable product references.
- [ ] Identical bytes in different stores do not share an asset or disclose deduplication information.
- [ ] Detaching media never deletes an object still referenced elsewhere; permanent deletion is asynchronous, retryable, and blocks new references.
- [ ] Browser upload is direct to MinIO through a short-lived presigned PUT, while Core verifies actual bytes before accepting the asset.
- [ ] Seller API exposes only store-scoped catalog routes and safe DTOs; compatibility routes are visibly deprecated.
- [ ] `seller/web/storefront` is the only supported customer storefront, and the mock storefront inside `seller/web/seller` is removed.
- [ ] Publish/unpublish updates the canonical storefront through existing revision semantics.
- [ ] Compose E2E proves real Postgres/Core/MinIO/Seller/Storefront integration and tenant isolation.
- [ ] OpenAPI documents, migrations, unit/integration tests, frontend tests, and Compose E2E all pass.
- [ ] No billing, payments, fulfillment, or Kubernetes implementation is included.

## 17. Completion Evidence

The implementation report must record:

- migration identifiers and invariant/backfill results;
- final Core and Seller API route tables and generated OpenAPI paths;
- configured entitlement default and validation behavior;
- media checksum verification and object-deletion retry evidence;
- role/tenant isolation test names;
- seller-owned and supplier-backed lifecycle test names;
- canonical storefront removal/replacement file list;
- Compose command and passing Playwright scenarios; and
- explicit confirmation that billing, payments, fulfillment, and Kubernetes were untouched.
