# Distributed Commerce Platform

# Master Architecture & Implementation Plan

## 1. Purpose

Build a scalable distributed commerce platform connecting:

* Suppliers and brands
* Sellers
* Native seller stores
* External seller stores
* End customers
* Shipping providers
* Payment providers
* External e-commerce platforms
* Future marketplace consumers

The platform should allow sellers to:

1. Create a native online store.
2. Connect an existing external store.
3. Browse supplier products available within their market.
4. Sell supplier products without owning stock.
5. Sell their own inventory alongside supplier products.
6. Configure pricing independently.
7. Process customer orders.
8. Use integrated shipping and payment providers.
9. Track order lifecycle, returns, balances, and settlements.
10. Benefit later from marketplace exposure, reputation, product intelligence, protected catalog access, and microbrand capabilities.

The platform must be designed from the beginning for:

* Arabic and English.
* RTL and LTR.
* Multiple countries and markets.
* Multiple currencies.
* Multiple seller stores.
* Multiple supplier operations.
* Multiple storefront themes.
* Independent external integrations.
* Event-driven processing.
* Horizontal scalability.
* Strong financial and inventory correctness.

The implementation must still remain evolutionary and avoid unnecessary infrastructure complexity before real demand exists.

---

# 2. Fundamental Engineering Principle

The system should follow:

```text
Correct Domain Model
        ↓
Reliable Transactions
        ↓
Strong Data Integrity
        ↓
Clear Service Boundaries
        ↓
Operational Reliability
        ↓
Measured Scaling
```

Scalability readiness must not become an excuse for premature complexity.

Design for future scale.

Deploy only what is currently justified.

---

# 3. Implementation Strategy

The project must be implemented through ordered phases.

A dependent phase must not begin until its required foundation is stable.

Primary dependency chain:

```text
Engineering Foundation
        ↓
Identity + Localization + Markets
        ↓
Commerce Domain Foundation
        ↓
P0 Seller Stores + P0 Supplier Portal / Wholesale Commerce Operations
        ↓
Staged Product Import Lifecycle
        ↓
Multiple Storefront Themes
        ↓
Complete Checkout / Order Lifecycle
        ↓
Operational Store Experience
        ↓
Shipping & Payments Operational Integration
        ↓
Financial Ledger & Settlements
        ↓
External Integration Foundation (Supplier & Seller Integrations)
        ↓
Public Integration API
        ↓
Unified Marketplace (Deferred to lowest priority until ecosystems are complete)
        ↓
Reputation + Intelligence
        ↓
Protected Commerce
        ↓
Microbrand
        ↓
Additional Markets
        ↓
Advanced Scale
```

Parallel work is allowed only when there are no unresolved dependencies.

---

## 3.1 Approved Execution Priority & Strategic Roadmap Alignment

### Immediate Strategic Milestone
The approved MatjerHub execution priority is strictly ordered as:

```text
P0 Seller Stores + P0 Supplier Portal / Wholesale Commerce Operations
        ↓
Product Import Lifecycle
        ↓
Multiple Storefront Themes
        ↓
Complete Checkout / Order Lifecycle
        ↓
Operational Store Experience
```

### Strategic Interpretation of Platform Ecosystems

The platform operates through two distinct, decoupled operational ecosystems:

1. **Seller Store Ecosystem (Primary Retail Commerce Environment)**:
   This is the primary customer-facing retail commerce environment. It includes seller store creation and configuration, custom domains/subdomains, store settings, catalog management, seller-owned products, supplier-backed products imported into seller stores, variants/SKUs, media assets, retail pricing, inventory management, multiple storefront themes, storefront publishing, customer browsing, cart, checkout, payment/COD, orders, fulfillment, shipping, and financial lifecycle.
   The Seller Store is the main retail implementation priority.

2. **Supplier Portal / Wholesale Commerce Ecosystem (Primary Wholesale Supply Operation)**:
   This is primarily the supplier-side wholesale and supply operation. It includes supplier onboarding, supplier markets, wholesale products, variants/SKUs, wholesale pricing, supplier inventory across fulfillment locations, supplier media, Market Offers (with MOQ, effective dating, and market currency constraints), offer availability, offer publishing, seller discovery/import of supplier offers, supplier order/fulfillment operations, dropship workflows, stock synchronization, and supplier operational management.
   The Supplier Portal's primary commerce responsibility is wholesale supply and fulfillment. The Supplier Portal MAY have an affiliated/direct retail-store capability where the architecture already supports it, but this capability is strictly secondary and must NOT be interpreted as requiring the Supplier Portal to become a second full retail-store platform at the same priority as Seller Stores.

Conceptually, the Supplier Portal structure follows:
```text
Supplier Portal
├── Wholesale Catalog & Market Offers      PRIMARY
├── Inventory / Availability               PRIMARY
├── Fulfillment / Dropship Operations      PRIMARY
├── Supplier Operations / Settings
└── Affiliated Retail Store                SECONDARY
```
No second independent retail commerce architecture is created for suppliers.

**Unified Marketplace Deferral**: Any new Unified Marketplace discovery, search, merchandising, ranking, recommendations, and cross-store consumer features are explicitly deferred to the lowest priority until Seller and Supplier ecosystems are substantially complete and production-ready in live operational environments. Foundational marketplace infrastructure merged through Core PR #70 (such as curated discovery read models in PR #68, platform service authentication in PR #69, and marketplace listing resolution with checkout attribution in PR #70) remains fully supported in `matjeroapps/core` as architectural dependencies, but further consumer marketplace feature development is paused.

### Immediate Product Goal
* **Seller Retail Store Operations**: A Seller can operate a production-ready customer-facing store with Core integration, native product creation, importing/linking supplier offers into store listings, variants/SKUs, media management, retail pricing, inventory tracking, multiple storefront themes, single-store cart, checkout execution, order management, multi-language/RTL localization, security, and complete retail operational lifecycle.
* **Supplier Wholesale Operations**: A Supplier can operate production-ready wholesale commerce and supply operations with Core integration, wholesale product authoring, variants/SKUs, wholesale pricing, inventory availability across fulfillment locations, market-specific offer publishing (with MOQ and effective dating), dropship order routing and fulfillment, stock synchronization, and financial ledger settlements (with direct/affiliated retail store capabilities remaining secondary).

### Required End-to-End Operational Flows

#### 1. End-to-End Seller Retail Flow
The complete operational retail cycle for Seller Stores:
```text
Seller → Store → Catalog → Product / Listing → Variants / SKUs → Media → Pricing → Inventory → Theme → Storefront → Cart → Checkout → Payment/COD → Order → Fulfillment → Shipping → Delivery → Financial lifecycle
```

1. **Seller Registration & Tenant Onboarding**: Seller registers, creates a store, and receives a store-scoped tenant context with role-based access control.
2. **Store Setup & Custom Domain Configuration**: Seller configures store profile, custom domain mapping or platform subdomain (`seller.matjerhub.com`), currency, and localization (Arabic/English, RTL/LTR).
3. **Catalog Setup & Product Management**: Seller creates native products directly via Seller Dashboard or initiates a staged bulk product import.
4. **Variants, SKUs, Media, Pricing & Inventory**: Seller configures product options (color, size), unique SKUs/barcodes, attaches media assets to products/variants, sets retail pricing, and allocates stock to store fulfillment locations.
5. **Storefront Theme Selection & Publishing**: Seller chooses and configures a storefront theme, customizing logo, colors, and layout, and publishes active seller listings.
6. **Customer Browsing & Cart Execution**: Buyer visits seller store on Next.js storefront, browses localized product catalog, selects variants, and adds items to single-store cart.
7. **Checkout & Atomic Inventory Reservation**: Buyer proceeds to checkout. Core validates store context, market currency, and reserves stock atomically in `inventory_snapshots` within a database transaction.
8. **Payment, Shipping & Order Placement**: Buyer completes payment (COD or electronic gateway) and submits shipping address. Core finalizes order aggregate and publishes `order.created` event via transactional outbox.
9. **Seller Order Operations & Fulfillment**: Seller views incoming order in Seller Dashboard, updates fulfillment state machine (processing -> shipped -> delivered), and tracks balance/payout in financial ledger.

#### 2. End-to-End Supplier-to-Seller Flow
The supplier-backed product flow preserves existing reference and ownership semantics:
```text
Supplier Product → Supplier Offer → Seller discovers offer → Seller imports/links offer → Seller creates store-specific listing → Seller sets retail price/presentation → Seller publishes listing → Customer purchases through Seller Store
```

1. **Supplier Registration & Tenant Onboarding**: Supplier registers, establishes supplier tenant context, and configures fulfillment capabilities.
2. **Supplier Catalog & Market Offer Creation**: Supplier defines wholesale products, variants, SKUs, wholesale cost pricing, minimum order quantities (MOQ), and market offers with effective dating.
3. **Offer Publishing to Market Catalog**: Supplier publishes active offers to the Core market catalog, making them discoverable to verified sellers.
4. **Seller Discovery & Offer Linking**: Seller browses available supplier offers in Seller Dashboard, selects offers, links them to seller store listings, and defines retail pricing markups and presentation without copying or modifying supplier-owned product facts.
5. **Inventory & Price Synchronization**: Real-time or batch synchronization reflects supplier stock availability and wholesale price updates on linked seller listings.
6. **Customer Purchase on Seller Storefront**: Buyer purchases the seller listing on the seller's storefront.
7. **Atomic Order Finalization & Inventory Reservation**: Core validates supplier offer validity, stock, and reserves inventory atomically across supplier fulfillment locations.
8. **Dropship Order Routing & Supplier Fulfillment**: Core routes dropship order items to Supplier Dashboard / Integration API. Supplier accepts, packages, ships item, and uploads tracking information.
9. **Automated Settlement & Ledger Reconciliation**: Double-entry financial ledger records buyer payment, deducts platform commission, credits supplier wholesale price, credits seller margin, and queues payout settlement.

### Staged Product Import Lifecycle

Product creation via bulk import or integration feeds must adhere strictly to the 10-stage import lifecycle:

```text
Import → Validate → Map → Review → Create/Update Product → Variants/SKUs → Media → Pricing → Inventory → Publish
```

The platform strictly distinguishes between:
* **Supplier-side wholesale product/catalog ingestion**: Ingesting supplier product facts, SKUs, inventory locations, and wholesale market offers into the supplier catalog.
* **Seller-side retail product creation**: Authoring seller-owned products, SKUs, store inventory, and retail listings.
* **Seller importing/linking supplier offers into a Seller Store**: Creating store-scoped seller listings linked to active supplier offers using reference semantics without copying supplier-owned product data into seller product entities.

These three distinct operations must never be collapsed into an ambiguous "import products" feature.

1. **Import**: Receive raw feed/file (CSV, JSON, Excel) or API payload into isolated staging tables (`import_jobs`, `import_records`).
2. **Validate**: Perform strict schema validation, check required fields (title, SKU, price), verify market currency, and detect duplicates.
3. **Map**: Map external attributes, category hierarchies, option values, and units to MatjerHub Core taxonomy and domain schemas.
4. **Review**: Present staging summary, warnings, and error logs to Seller or Supplier in dashboard for manual or automated review before database mutation.
5. **Create/Update Product**: Transactionally create or update `products` domain entities with localized Arabic/English titles and descriptions.
6. **Variants/SKUs**: Generate variant permutations and create `skus` records enforcing unique SKU code and barcode constraints.
7. **Media**: Process, store, and link image assets and galleries to products and variants via Object Storage.
8. **Pricing**: Establish seller listing prices (`seller_listing_prices`) or supplier offer prices (`supplier_offers`) with currency validation.
9. **Inventory**: Initialize or update `inventory_snapshots` for assigned fulfillment locations.
10. **Publish**: Update `seller_listings` or `supplier_offers` status to active/published, exposing listings for storefront resolution or seller catalog discovery.

*Note: This lifecycle reuses existing Core database tables, transactional outbox patterns, and service boundaries without inventing competing architectures.*

### Core Ownership & Repository Boundaries

Repository boundaries must be strictly preserved across all operational capabilities:

* **`matjeroapps/core`**: Owns all domain models, persistence schemas, business invariants, authorization, tenant isolation, financial ledger, and outbox event publishing. Serves private HTTP/JSON API (`/internal/v1`) behind service-auth tokens (`X-Matjero-Service`, `Authorization: Bearer`).
* **`matjeroapps/seller`**: Seller actor application (Next.js / React). Communicates with Core solely via internal APIs. No direct database access or Core package imports.
* **`matjeroapps/supplier`**: Supplier actor application (React). Communicates with Core solely via internal APIs.
* **`matjeroapps/platform`**: Public Platform application and native Seller Storefront (Next.js). Resolves stores via trusted host mapping and communicates with Core internal APIs.
* **`matjeroapps/admin`**: Platform Admin application. Communicates with Core internal APIs.
* **`matjeroapps/ui-sdk`**: Shared `@matjerhub/ui-sdk` package containing design tokens, shared UI components, and accessibility primitives.

### Status of Merged Marketplace Work

Foundational marketplace capabilities merged through Core PR #70 remain supported in `matjeroapps/core`:
* Curated discovery collection read models (`GET /internal/v1/markets/{market_code}/marketplace/collections/{collection_type}` in PR #68).
* Platform service authentication (`X-Matjero-Service: platform` in PR #69).
* Marketplace listing resolution (`POST /internal/v1/markets/{market_code}/marketplace/listings/{listing_id}/resolve` in PR #70).
* Single-store checkout attribution stored in `marketplace_order_attributions` and published on `order.created` (PR #70).

All future consumer marketplace discovery, multi-seller catalog aggregation, cross-store search, merchandising, ranking algorithms, and recommendations are deferred until Seller Stores and Supplier Portal / Wholesale Commerce ecosystems achieve complete operational readiness.

---

## 3.2 Measurable Exit Criteria for Immediate Operational Milestones

To verify completion of the immediate operational priorities, the following measurable exit criteria must be met:

### 1. Seller Completeness Exit Criteria
* [ ] **Store Configuration**: Sellers can complete onboarding, map custom domains/subdomains, set store locale defaults, and select active storefront themes.
* [ ] **Catalog Management**: Sellers can create, update, archive, and view products, variants, SKUs, barcodes, and media galleries.
* [ ] **Inventory & Pricing**: Sellers can set retail prices, configure currency defaults, assign stock across fulfillment locations, and track available vs reserved inventory.
* [ ] **Order Management**: Sellers can view orders, update order status through the state machine (pending -> confirmed -> processing -> shipped -> delivered), process cancellations, and view order financial snapshots.
* [ ] **Localization & Security**: 100% Arabic/English and RTL/LTR rendering accuracy across Seller Dashboard; tenant isolation verified with zero cross-tenant data leaks.
* [ ] **Core Integration**: All mutations emit versioned outbox events (`seller.created`, `product.updated`, `order.updated`); unit, vet, lint, and build suites pass cleanly.

### 2. Supplier Completeness Exit Criteria
* [ ] **Supplier Onboarding & Wholesale Catalog**: Suppliers can register, establish wholesale supplier tenant context, create wholesale catalogs, define product variants, SKUs, and wholesale cost pricing.
* [ ] **Market Offers**: Suppliers can publish market offers with effective dating, minimum order quantities (MOQ), and market currency constraints.
* [ ] **Fulfillment & Dropship Routing**: Suppliers receive routed dropship orders from linked seller sales, confirm fulfillment, submit tracking information, and emit `supplier_order.fulfilled` events.
* [ ] **Stock Feeds**: Real-time or batch inventory snapshot updates reflect accurately across all linked seller listings.
* [ ] **Financial Settlement**: Wholesale order fulfillments generate double-entry financial ledger entries; balance projections and payouts reconcile accurately.

### 3. Product Import Lifecycle Exit Criteria
* [ ] **Full 10-Stage Pipeline**: CSV, JSON, and API feed imports execute through all 10 stages (`Import -> Validate -> Map -> Review -> Create/Update Product -> Variants/SKUs -> Media -> Pricing -> Inventory -> Publish`).
* [ ] **Validation & Error Reporting**: Staging tables capture schema errors, missing required fields, and mapping failures with line-by-line diagnostic reports.
* [ ] **Review Interface**: Sellers and Suppliers can inspect staged import records, review mapping warnings, and approve batch creation in their respective workspaces.
* [ ] **Transactional Safety & Idempotency**: Re-running imports with identical feed identifiers updates existing products without creating duplicate SKUs or orphaned database records.

### 4. Storefront & Themes Exit Criteria
* [ ] **Multiple Configurable Themes**: At least multiple distinct storefront themes rendered by Next.js storefront, supporting customizable logos, color tokens, typography, and section layouts.
* [ ] **Theme Isolation**: Themes depend strictly on shared components from `@matjerhub/ui-sdk` and token primitives, with 0 dependencies on other themes.
* [ ] **Separation of Business Logic**: Theme components contain 0 business logic, cart state calculation, or direct API calls; all logic resides in storefront application services.
* [ ] **Localization & RTL**: 100% Arabic and English rendering with mirror-image RTL/LTR layout transitions, zero horizontal scroll overflow, and WCAG AA contrast compliance.
* [ ] **Storefront Caching & Performance**: Caching strategy includes store context in cache keys (`store:{store_id}:...`); theme assets load cleanly from CDN.

### 5. Checkout & Order Lifecycle Exit Criteria
* [ ] **Single-Store Cart & Checkout**: Single-seller checkout session creation validated against tenant and market invariants.
* [ ] **Atomic Inventory Reservations**: Inventory reservations use atomic database transactions with concurrency control, eliminating over-selling under load.
* [ ] **Payment & Shipping Execution**: Support COD and electronic payment flows; webhook processing handles idempotency keys (`Idempotency-Key`); shipment state machine handles package creation and tracking.
* [ ] **Attribution & Outbox Reliability**: Marketplace-attributed checkouts store immutable snapshots in `marketplace_order_attributions`; `order.created` and `order.finalized` events publish reliably through outbox background workers.
* [ ] **Ledger Integration**: Completed, cancelled, and refunded orders generate immutably balanced double-entry financial ledger postings.

---

# 4. Core Technology Stack

## Backend

Primary backend language:

# Go / Golang

Go must be used for:

* Backend APIs
* Commerce application services
* Workers
* Schedulers
* Integration applications
* Webhook processors
* Event consumers
* Internal background services

---

## Frontend

### Management Applications

Use:

# React.js

For:

```text
Admin Dashboard
Seller Dashboard
Supplier Dashboard
```

### Seller Storefront

Use:

# Next.js

Because the storefront requires:

* SSR
* SEO
* Dynamic domains
* Dynamic stores
* Theme rendering
* CDN integration
* Server-side metadata
* Product indexing
* Sitemap generation
* Social previews
* High-performance caching
* Future static generation where appropriate

---

# 5. Infrastructure Stack

Target architecture:

```text
PostgreSQL
Redis
RabbitMQ
Transactional Outbox
ZITADEL
Object Storage
CDN
```

The initial production deployment runs this same stack: there is no separate
"later" broker to grow into. RabbitMQ is the single asynchronous messaging
backbone, not a temporary MVP transport, and no additional messaging broker is
planned. See
[ADR-018](adr/ADR-018-rabbitmq-asynchronous-messaging-backbone.md).

Event and message contracts must be transport-stable and versioned from the
beginning, independent of broker implementation details.

---

# 6. PostgreSQL

PostgreSQL is the authoritative source of truth for:

* Markets
* Sellers
* Suppliers
* Stores
* Catalog
* Supplier offers
* Listings
* Inventory
* Reservations
* Orders
* Payments
* Shipments
* Returns
* Financial journal
* Settlements
* Payouts

Do not split the initial platform into separate databases unnecessarily.

Use one PostgreSQL cluster initially with clear logical module ownership.

Separate databases may be introduced later only when operational evidence justifies them.

---

# 7. Redis

Redis is a performance and coordination layer.

Allowed uses:

* Cache
* Rate limits
* Sessions where required
* Short-lived idempotency state
* Short-lived locks
* Store resolution cache
* Storefront cache
* Temporary integration state
* Request throttling

Redis must not be authoritative for:

* Inventory
* Orders
* Money
* Payments
* Settlements
* Seller balances
* Supplier balances

Loss of Redis must not corrupt transactional business state.

---

# 8. RabbitMQ

RabbitMQ is the single asynchronous messaging backbone. It carries asynchronous
commands, work queues, background jobs, domain events, integration events,
fan-out, notifications, webhook processing, search indexing, and long-running
workflows. See
[ADR-018](adr/ADR-018-rabbitmq-asynchronous-messaging-backbone.md).

Command and job examples:

```text
integration.product.pull
integration.product.push

integration.inventory.pull
integration.inventory.push

integration.order.import
integration.order.export

shipping.create
shipping.cancel

payment.process

notification.send

webhook.process

reconciliation.run
```

Domain and integration event examples:

```text
ProductCreated
ProductUpdated
SupplierOfferChanged
InventoryChanged
OrderCreated
OrderConfirmed
OrderCancelled
ShipmentCreated
ShipmentDelivered
PaymentCaptured
RefundCompleted
SettlementCompleted
```

These event names are illustrative. No event exists until the phase that owns it
implements it.

RabbitMQ responsibilities include:

* Async jobs
* Commands
* Domain and integration events
* Retries
* Work distribution
* Rate-controlled processing
* Provider-specific workers

Must support:

* Retry policy
* Dead-letter queues
* Poison-message handling
* Retry count
* Correlation IDs
* Idempotent consumers
* Observability

Delivery is designed as at-least-once. Consumers with side effects must be
idempotent; exactly-once delivery must not be assumed.

---

# 9. Fan-Out

Several consumers needing the same event is not a reason to introduce a second
broker. A RabbitMQ exchange with independently bound queues gives each consumer
its own backlog, failure isolation, retry policy, and dead-letter path.

```text
                  domain event
                       │
                       ▼
                    Exchange
                       │
        ┌──────────────┼──────────────┐
        ▼              ▼              ▼
    search queue   notification   integration
                      queue          queue
```

RabbitMQ is not the mechanism for synchronous request/response business
capabilities. Those use HTTP/JSON. RabbitMQ RPC is not a platform default, and
RabbitMQ must never carry database access instructions.

---

# 10. ZITADEL Identity Architecture

ZITADEL is the centralized identity provider.

It handles:

* Authentication
* OIDC
* OAuth2
* MFA
* Service accounts
* Identity lifecycle
* Coarse roles

Used by:

```text
Admin Web
Seller Web
Supplier Web
```

Initial coarse roles:

```text
platform_admin

seller_owner
seller_manager
seller_staff

supplier_owner
supplier_manager
supplier_staff
```

ZITADEL does not replace resource-level authorization.

Example:

```text
ZITADEL:
user = seller_manager

Platform:
user belongs to Seller #100
user may access Store #200
user may not access Store #300
```

Resource ownership remains platform-controlled.

---

# 11. Main Application Boundaries

## Backend APIs

Independent deployments:

```text
admin-api
seller-api
supplier-api
storefront-api
```

These are separate external application boundaries.

They may scale and deploy independently.

They must not duplicate Commerce Core business rules.

---

## Frontends

```text
admin-web      → React.js
seller-web     → React.js
supplier-web   → React.js
storefront-web → Next.js
```

---

## Background Runtime

```text
general-worker
scheduler
```

Responsibilities include:

### General Worker

* Notifications
* Internal async jobs
* Shipment jobs
* Payment-related jobs
* Webhook deliveries
* Cache invalidation
* Background calculations

### Scheduler

* Reconciliation
* Reservation expiry
* Settlement cycles
* Payout cycles
* Cleanup
* Periodic synchronization
* Aggregation
* Scheduled maintenance

---

# 12. Commerce Core

The Commerce Core owns shared commercial business truth.

Main modules:

```text
Markets
Sellers
Suppliers
Stores
Catalog
Supplier Offers
Listings
Inventory
Reservations
Orders
Payments
Fulfillment
Returns
Finance
Marketplace
Billing
```

The APIs invoke Commerce Core application/domain services.

Avoid:

```text
Admin Order Business Logic
Seller Order Business Logic
Supplier Order Business Logic
```

as separate implementations.

Use one set of business rules.

---

# 13. API Boundary Rule

APIs are actor-specific entry points, not independent domains.

Example:

```text
Admin API
    ↓
Commerce Core

Seller API
    ↓
Commerce Core

Supplier API
    ↓
Commerce Core

Storefront API
    ↓
Commerce Core
```

Do not create:

```text
Admin API → Seller API
Seller API → Supplier API
Supplier API → Admin API
```

for internal commerce operations.

They should invoke shared domain/application logic directly.

---

# 14. Multi-Language Architecture

Initial languages:

```text
Arabic  ar
English en
```

Full support required from the beginning for:

```text
RTL
LTR
```

The architecture must support future languages without schema redesign.

---

## Frontend

All UI strings must use structured i18n resources.

No hardcoded customer-facing strings inside components.

Arabic must be tested as a first-class language, not added afterward.

---

## Backend

Language negotiation may use:

```http
Accept-Language: ar
Accept-Language: en
```

where localized responses are needed.

---

## Translated Business Content

Use translation entities.

Example:

```text
product_translations

product_id
locale
name
description
```

Also use translations for:

```text
categories
marketplace content
store pages
theme content
SEO content
```

Avoid:

```text
name_ar
name_en
name_fr
```

because this makes future languages expensive.

---

# 15. Localized Slugs and SEO

Localized content should support localized slugs where necessary.

Example:

```text
/product/coffee-maker
/ar/product/ماكينة-قهوة
```

The exact URL design may vary, but the data model must not assume one universal slug for every language.

Storefront architecture should support:

* Canonical URLs
* hreflang
* Localized metadata
* Localized structured data
* Localized sitemap entries

---

# 16. Multi-Market Architecture

The platform uses:

# Market

as a first-class domain concept.

Examples:

```text
Egypt
Saudi Arabia
United Arab Emirates
```

Each market contains:

```text
country
currency
default locale
supported locales
timezone
status
market configuration
shipping providers
payment providers
```

Examples:

```text
EG
Currency = EGP
Locales = ar, en

SA
Currency = SAR
Locales = ar, en

AE
Currency = AED
Locales = ar, en
```

---

# 17. Market Isolation

Each Seller Store belongs to exactly one Market.

Each Supplier Offer belongs to exactly one Market.

Initial business rule:

```text
Seller Store Market
=
Supplier Offer Market
```

No cross-border supplier sourcing initially.

This deliberately avoids early complexity involving:

* Foreign currencies
* International shipping
* Customs
* Cross-border returns
* Taxes
* Import regulations
* International supplier settlements

---

# 18. Database-Enforced Market Integrity

Market isolation must not rely only on Go validation.

Model:

```text
Store
(id, market_id)

SupplierOffer
(id, market_id)

SellerListing
(
  store_id,
  supplier_offer_id,
  market_id
)
```

Use database constraints so:

```text
SellerListing(store_id, market_id)
→ Store(id, market_id)
```

and:

```text
SellerListing(supplier_offer_id, market_id)
→ SupplierOffer(id, market_id)
```

Therefore:

```text
Store.market
=
Listing.market
=
SupplierOffer.market
```

invalid combinations cannot enter PostgreSQL.

Changing Store Market after transactional activity exists should normally be prohibited.

---

# 19. Supplier Multi-Market Model

A Supplier may operate in multiple markets.

```text
Supplier
    ↓
Supplier Market
```

Example:

```text
Supplier A

├── Egypt
├── Saudi Arabia
└── UAE
```

Each operation may have its own:

* Pricing
* Inventory
* Fulfillment locations
* Integrations
* Shipping capabilities
* Settlement settings

---

# 20. Seller Multi-Market Model

A Seller may own multiple Stores.

Each Store belongs to one Market.

Example:

```text
Seller
├── Egypt Store
└── Saudi Store
```

This provides clean separation for:

* Currency
* Catalog
* Shipping
* Payment
* Pricing
* Marketplace behavior

---

# 21. Fulfillment Location

Fulfillment Location must be a first-class domain entity from the beginning.

Do not model inventory as merely:

```text
Supplier + SKU
```

Instead:

```text
Supplier
   ↓
Supplier Market
   ↓
Fulfillment Location
   ↓
SKU Inventory
```

A Fulfillment Location may represent:

* Supplier warehouse
* Supplier branch
* 3PL warehouse
* Platform warehouse in the future

Example:

```text
fulfillment_locations

id
supplier_id
market_id
name
type
status
address
shipping_capabilities
```

This prevents major redesign when multiple warehouses or 3PL fulfillment are introduced.

---

# 22. Inventory Ownership Model

The authoritative stock belongs to an inventory pool associated with:

```text
SKU
+
Fulfillment Location
```

not directly to a seller listing.

Multiple seller listings may consume the same supplier inventory pool.

Example:

```text
Supplier SKU Inventory = 100

Seller A Listing
Seller B Listing
Seller C Listing

All consume the same 100-unit pool.
```

---

# 23. Money Representation

Money must never use floating-point arithmetic.

Do not use:

```go
float32
float64
```

for monetary calculations.

Use:

```text
amount_minor
currency
```

where appropriate.

Example:

```text
EGP 150.25

amount_minor = 15025
currency = EGP
```

If a market/currency requires more complex decimal precision, use an explicit decimal model.

Centralize:

* Currency precision
* Rounding rules
* Tax rounding
* Commission rounding
* Percentage calculation behavior

Financial calculations must be deterministic.

---

# 24. Order Commercial Snapshot

Order history must not depend on current product data.

Every Order Item must capture a commercial snapshot.

Example:

```text
order_item

product_id
variant_id
sku_id

product_name_snapshot
variant_name_snapshot
sku_snapshot

seller_listing_id
supplier_offer_id
supplier_id

seller_price_snapshot
supplier_cost_snapshot

platform_fee_snapshot
discount_snapshot
shipping_allocation_snapshot

currency

quantity
```

Changing a Product, Supplier Offer, Listing, or Price tomorrow must not rewrite yesterday's order economics.

---

# 25. Strong vs Eventual Consistency

Consistency requirements must be explicit.

## Strongly Consistent

Must remain transactional:

```text
Inventory reservation
Inventory release
Order creation
Payment financial state
Ledger posting
Settlement reservation
Payout state transitions
Market integrity
```

---

## Eventually Consistent

May propagate asynchronously:

```text
External seller store inventory
External supplier inventory observations
Search index
Analytics
Notifications
Marketplace ranking
Cache
Seller dashboards aggregates
External order status synchronization
```

The platform's transactional source of truth must remain distinguishable from external synchronized views.

---

# PHASE 0 — Engineering and Architecture Foundation

## Objective

Establish engineering conventions and lock critical architectural decisions before commerce development starts.

---

## Repository

Recommended monorepo:

```text
/apps
    admin-api/
    seller-api/
    supplier-api/
    storefront-api/

    workers/
        general-worker/
        scheduler/

    integrations/
        suppliers/
        sellers/

/web
    admin/
    seller/
    supplier/
    storefront/

/internal
    markets/
    sellers/
    suppliers/
    stores/
    catalog/
    listings/
    inventory/
    orders/
    payments/
    fulfillment/
    returns/
    finance/
    events/

/packages
    auth/
    contracts/
    messaging/
    observability/
    database/
```

---

## Engineering Standards

Implement:

* Go project conventions
* Dependency boundaries
* Configuration management
* Database migration framework
* Structured logging
* API error format
* Validation
* Request IDs
* Correlation IDs
* Graceful shutdown
* Context propagation
* Health endpoints
* Readiness endpoints
* Feature flags
* Environment handling

---

# 26. Architecture Decisions Required During Phase 0

Formalize ADRs covering:

```text
PostgreSQL as transactional source of truth

Market isolation

Fulfillment Location model

Inventory reservation strategy

Transactional Outbox

Consumer Inbox / event idempotency

Money representation

Double-entry ledger

Order commercial snapshots

Independent integration deployment model

Storefront tenant resolution

Asynchronous messaging backbone
```

These must be settled before downstream implementations depend on them.

---

# 27. CI/CD

Implement:

* Go formatting
* Go linting
* Unit tests
* Integration tests
* Frontend lint
* Frontend tests
* Type checks
* Docker builds
* Migration checks
* Dependency security scans
* Secret scanning
* Build reproducibility
* Deployment workflow

---

# 28. Observability Foundation

Use OpenTelemetry-compatible instrumentation.

Monitor:

```text
API latency
API errors
Request count
Database latency
Database connection utilization
Redis hit ratio
RabbitMQ queue depth
Worker retries
DLQ size
Webhook lag
Integration failures
Inventory reservation failures
Order processing latency
Payment failures
Settlement failures
```

Business metrics should also be observable later.

---

# PHASE 1 — Identity, Localization and Markets

## Implement

### Identity

* ZITADEL integration
* Admin authentication
* Seller authentication
* Supplier authentication
* MFA-compatible flows
* Role mapping
* Resource authorization foundation

### Localization

* Arabic
* English
* RTL
* LTR
* Translation framework
* Localized validation messages

### Markets

* Countries
* Currencies
* Markets
* Market locales
* Market configuration

Initial configuration can include:

```text
Egypt
Saudi Arabia
UAE
```

even if only Egypt launches first.

---

## Exit Criteria

* Authentication works.
* Authorization works.
* Arabic works.
* English works.
* RTL works.
* LTR works.
* Market configuration works.
* Tenant/resource boundaries are tested.

---

# PHASE 2 — Commerce Domain Foundation

## Supplier Domain

Implement:

```text
Supplier
Supplier Member
Supplier Market
Supplier Settings
Fulfillment Location
```

---

## Seller Domain

Implement:

```text
Seller
Seller Member
Seller Settings
```

---

## Store Domain

Implement:

```text
Store
Store Domain
Store Settings
Store Market
```

---

## Catalog

Implement:

```text
Product
Product Translation
Variant
SKU
Category
Category Translation
Attributes
Options
Media
```

---

## Supplier Commerce

Implement:

```text
Supplier Product
Supplier Offer
Supplier Price
Supplier Availability
```

---

## Seller Commerce

Implement:

```text
Seller Listing
Seller Price
Seller Listing Status
```

---

## Inventory Foundation

Implement:

```text
Inventory Snapshot
Inventory Movement
Inventory Reservation
Inventory Adjustment
```

Even if checkout is introduced later, the inventory model must be designed correctly here.

---

# 29. Inventory Snapshot + Movement Ledger

Use both:

```text
inventory_snapshot
inventory_movements
```

Example movements:

```text
RECEIVED +100
RESERVED -2
RELEASED +1
SHIPPED -1
RETURNED +1
ADJUSTED -3
```

The movement ledger provides traceability.

The snapshot provides efficient current-state access.

---

# 30. Inventory Reservation Concurrency

Reservation must be atomic.

Preferred initial strategy:

```sql
UPDATE inventory_snapshot
SET reserved = reserved + $quantity
WHERE id = $inventory_id
  AND available - reserved >= $quantity
RETURNING id;
```

If no row is returned:

```text
Insufficient stock
```

For more complex multi-row reservation workflows, use row-level locks where justified.

Do not default the entire checkout path to PostgreSQL SERIALIZABLE isolation unless actual need is demonstrated.

---

# 31. Inventory Reservation Entity

Example:

```text
inventory_reservations

id
inventory_id
order_id
checkout_id
quantity
status
expires_at
idempotency_key
created_at
updated_at
```

Statuses:

```text
ACTIVE
CONSUMED
RELEASED
EXPIRED
```

Reservation expiry must be idempotent.

---

# PHASE 3 — Admin, Supplier and Seller Platforms

## Admin

Implement:

* Markets
* Suppliers
* Sellers
* Stores
* Products
* Categories
* Moderation
* Users
* Audit
* Basic configuration

---

## Supplier Portal / Dashboard (Primary: Wholesale Commerce & Market Offers)

The Supplier Portal's primary workspace is wholesale catalog management and market offer publishing. Suppliers are responsible for wholesale supply, inventory availability, and order fulfillment. Direct/affiliated retail-store management is secondary.

Implement:

* **Wholesale Catalog & Market Offers (PRIMARY)**:
  * Supplier profile & operational settings
  * Supplier markets
  * Fulfillment locations
  * Wholesale products & product authoring
  * Variants and unique SKUs/barcodes
  * Wholesale cost pricing
  * Inventory availability tracking across fulfillment locations
  * Market Offers (with MOQ, effective dating, and market currency constraints)
  * Offer publishing and lifecycle management
  * Supplier media assets
* **Fulfillment & Dropship Operations (PRIMARY)**:
  * Inbound dropship order routing from linked seller sales
  * Package fulfillment state machine and tracking updates
  * Double-entry settlement tracking
* **Supplier Members & Security**:
  * Role-based access (`supplier_owner`, `supplier_manager`, `supplier_staff`)
* **Affiliated Retail Store (SECONDARY)**:
  * Optional access to an affiliated seller retail store where supported by architecture, reusing standard seller store infrastructure without creating a parallel retail platform.

Manual wholesale product and offer management must work before external supplier integrations exist.

---

## Seller Dashboard (Primary: Customer-Facing Retail Store Operations)

The Seller Dashboard's primary workspace is operating the customer-facing retail store.

Implement:

* **Store Management & Configuration**:
  * Seller profile & store settings
  * Custom domain / platform subdomain mapping
  * Multi-store management and store switcher (with Core entitlement enforcement)
  * Storefront theme selection and customization
* **Unified Catalog & Merchandising**:
  * Native seller-owned product authoring (translations, categories, variants, SKUs, retail pricing, presentation)
  * Store-scoped media library (presigned uploads, SHA-256 deduplication, reusable references)
  * Browse supplier wholesale catalog & market offers filtered by market
  * Idempotent import / linking of supplier offers into store listings (preserving reference semantics and supplier ownership of source facts)
  * Retail pricing markups and store-specific presentation
  * Listing lifecycle management (`DRAFT <-> PUBLISHED <-> UNPUBLISHED -> ARCHIVED`)
  * Publish readiness evaluation
* **Store Inventory & Operations**:
  * Store fulfillment locations and stock allocation for seller-owned SKUs
  * Real-time availability synchronization for supplier-backed listings
  * Order management and retail fulfillment lifecycle
  * Basic analytics
  * Store members (`seller_owner`, `seller_manager`, `seller_staff`)

---

## Exit Criteria

This operational flow works end-to-end without external integrations:

```text
Create Supplier
      ↓
Create Fulfillment Location
      ↓
Create Wholesale Product & SKUs
      ↓
Set Inventory Availability
      ↓
Publish Market Offer
      ↓
Create Seller
      ↓
Create Store
      ↓
Browse Supplier Market Offers
      ↓
Import/Link Offer to Store Listing
      ↓
Set Retail Price & Presentation
      ↓
Publish Store Listing
```

---

# PHASE 4 — Native Storefront and Theme Engine

Seller Store storefront themes are a core, high-priority part of the customer-facing retail store experience.

## Storefront Architecture

Use one Next.js storefront application capable of serving many seller stores.

Store resolution:

```text
Incoming Request
      ↓
Host / Domain
      ↓
Store Resolver
      ↓
store_id
      ↓
market_id
      ↓
theme
      ↓
catalog
```

---

# 32. Store Resolution

Support:

```text
seller.platform.com
seller-custom-domain.com
```

The Storefront API must resolve store context using a trusted domain-to-store mapping.

Seller Dashboard APIs may use explicit store IDs.

Public storefront requests should not trust arbitrary user-supplied store IDs when the domain already defines the tenant.

---

# 33. Tenant Isolation

All storefront operations must be scoped to the resolved store.

Examples:

* Catalog queries
* Cart
* Checkout
* Theme
* Store settings
* Pricing
* Availability

Cross-store data leakage is a critical security failure.

Tenant isolation must be covered by integration and security tests.

---

# 34. Storefront Cache Strategy

Cache keys must always include store context.

Example:

```text
store:{store_id}:catalog:{version}:product:{product_id}
```

Avoid wildcard deletion over large Redis keyspaces.

Prefer:

* Versioned namespaces
* Targeted invalidation
* Event-driven invalidation

---

# 35. Theme Engine

Multiple configurable storefront themes are high priority for Seller Stores.

Sellers can:

```text
Install Free Theme
Purchase Premium Theme
```

Entities:

```text
Theme
Theme Version
Theme Type
Theme Price
Theme Asset
Theme Configuration Schema
Theme Installation
Theme License
```

Types:

```text
FREE
PREMIUM
```

---

## Configurable Theme Features

* Logo
* Colors
* Typography
* Header
* Footer
* Hero
* Homepage sections
* Product cards
* Category layouts
* Banners
* Navigation
* Layout settings

All themes must support:

```text
Arabic
English
RTL
LTR
```

---

# 36. Theme Security

Do not initially allow arbitrary JavaScript execution from sellers.

Themes must be:

* Platform-controlled
* Versioned
* Sandboxed by architecture
* Compatible with predefined rendering contracts

Third-party theme development may be introduced only after a safe extension model exists.

---

## 36.1 Theme Architecture & UI/UX Workflow Rules

### Theme Dependency & Logic Rules
1. **Shared Primitives**: Themes may depend on shared components, layouts, and design tokens provided by `@matjerhub/ui-sdk`.
2. **Zero Inter-Theme Dependencies**: Themes must NEVER depend on or import components from other themes. Each theme must remain strictly independent and self-contained.
3. **Decoupled Business Logic**: All business logic, cart calculation, inventory checking, checkout state management, and direct API calls must reside outside theme rendering components in storefront application services.
4. **Token System & Localization**: All themes must consume the `@matjerhub/ui-sdk` design token system and provide 100% Arabic and English localization with native RTL and LTR mirror layout support.

### Mandatory UI/UX Skill Workflow
* **UI/UX Design Skill**: For all upcoming UI design and frontend development tasks across MatjerHub applications (Seller, Supplier, Admin, Platform), developers must use `https://github.com/nextlevelbuilder/ui-ux-pro-max-skill` and initialize project UI contexts using `uipro init --ai antigravity`.
* **Stitch Tooling Boundary**: Stitch MCP tools are restricted strictly to the MatjerHub Platform UI components previously designed with Stitch. Stitch must NOT be used for Seller Storefront Themes unless explicitly requested by the user. Storefront themes must be authored using UI/UX Pro Max + `@matjerhub/ui-sdk` primitives.

---

# PHASE 5 — Cart, Checkout, Orders and Inventory Transactions

The complete checkout and order lifecycle is a high-priority milestone for the Seller Store.

*Strategic Context*: Customer checkout and retail order execution occur strictly within the Seller Store customer journey. Supplier participation in orders involving supplier-backed products occurs through backend dropship order routing, supplier fulfillment state machines, and financial ledger settlements—not by moving retail checkout or customer management into the Supplier Portal.

*Historical Status*: Core domain models, transactional outbox publishing, atomic inventory reservation algorithms, order aggregate state machines, and single-store marketplace checkout attribution (merged in Core PR #70) have completed foundational backend implementation in `matjeroapps/core`. Production end-to-end checkout execution across multiple Seller Storefront themes remains an active operational focus.

## Implement

```text
Customer
Customer Address
Cart
Cart Item
Checkout Session
Order
Order Item
Order Timeline
Order Note
```

Initial order lifecycle:

```text
PENDING
CONFIRMED
PROCESSING
READY_FOR_SHIPPING
SHIPPED
OUT_FOR_DELIVERY
DELIVERED
CANCELLED
RETURNED
```

Use an explicit state machine.

---

# 37. Checkout Transaction

Critical path:

```text
Validate Store
Validate Market
Validate Listings
Validate Prices
Reserve Inventory
Create Order
Create Order Items
Create Outbox Events
Commit
Return Order
```

Do not synchronously call:

* Courier
* External supplier
* External seller store
* Analytics system
* Email provider

inside the main order transaction.

---

# 38. Transactional Outbox

Use a PostgreSQL Transactional Outbox.

Example:

```text
outbox_events

event_id
aggregate_type
aggregate_id
aggregate_version
event_type
schema_version
payload
correlation_id
causation_id
occurred_at
published_at
created_at
```

Every event receives a generated unique:

```text
event_id
```

Do not derive event identity from payload hashes.

---

# 39. Outbox Publishing

Initial implementation:

```text
PostgreSQL
    ↓
Polling Outbox Publisher
    ↓
Event Transport
```

Workers may use:

```sql
FOR UPDATE SKIP LOCKED
```

to safely process batches concurrently.

Do not introduce Debezium/CDC in the MVP unless required.

---

# 40. Event Consumer Inbox

All important event consumers should use an idempotency/inbox mechanism.

Example:

```text
processed_events

consumer_name
event_id
processed_at
```

Unique:

```text
(consumer_name, event_id)
```

Processing flow:

```text
Receive Event
      ↓
Check Inbox
      ↓
Already Processed?
      ├── Yes → Ignore
      └── No
           ↓
      Process Transaction
           ↓
      Record Inbox Entry
           ↓
         Commit
```

---

# 41. Event Ordering

Use:

```text
aggregate_id
aggregate_version
```

for causal ordering.

Do not rely purely on timestamps.

Ordering is a business requirement, not a transport configuration. Where a
workflow requires it, messages affecting the same aggregate may need ordered
processing, typically identified by:

```text
Order events     → order_id
Inventory events → inventory_id / SKU pool
Seller events    → seller_id
```

depending on event semantics. State such a requirement as an ordering constraint
on the owning workflow and satisfy it with aggregate identity plus aggregate
version. Do not express it as a broker partition key.

---

# PHASE 6 — Shipping

Begin with one shipping provider in the first launch market.

Commerce Core owns:

```text
Shipment
Shipment Item
Shipment Status
Tracking
Shipping Cost
COD Amount
Shipment Events
```

Provider-specific logic belongs behind a provider adapter.

Conceptual operations:

```text
CreateShipment
CancelShipment
TrackShipment
CalculateRate
HandleWebhook
```

---

# 42. Shipment State Machine

Model shipping transitions explicitly.

Support future:

* Partial shipments
* Multiple packages
* Split fulfillment locations
* Failed delivery
* Return-to-origin
* Reattempts

Do not assume:

```text
Order = exactly one Shipment
```

---

# PHASE 7 — Payments

Initial methods:

```text
Cash on Delivery
+
One Electronic Payment Provider
```

Implement:

```text
Payment
Payment Attempt
Payment Status
Refund
Payment Provider Reference
```

---

# 43. Payment State Machine

Example:

```text
CREATED
PENDING
AUTHORIZED
CAPTURED
FAILED
CANCELLED
PARTIALLY_REFUNDED
REFUNDED
```

Provider-specific behavior may vary.

Internal states must remain deterministic.

---

# 44. Payment Idempotency

Strict idempotency is required for:

* Payment creation
* Capture
* Refund
* Cancel
* Webhook processing
* Payment retry

Never assume provider APIs execute exactly once.

---

# 45. Webhook Inbox Pattern

All external webhooks should follow:

```text
Receive
   ↓
Verify Signature
   ↓
Persist Envelope
   ↓
Deduplicate
   ↓
Return 2xx Quickly
   ↓
Queue Async Processing
   ↓
Process
```

Example:

```text
webhook_inbox

id
provider
connection_id
provider_event_id
event_type
payload
payload_hash
signature_verified
received_at
status
attempt_count
processed_at
```

Where available, uniqueness should use:

```text
provider
connection_id
provider_event_id
```

---

# 46. Webhook Ordering

Never assume webhook delivery order.

Internal state machines must reject stale transitions.

Example:

If:

```text
Payment = CAPTURED
```

and an old:

```text
AUTHORIZED
```

webhook arrives later, it must not regress the state.

Reconciliation handles events that never arrive.

---

# PHASE 8 — Financial Ledger, Reconciliation and Settlements

This phase is critical.

Financial architecture must be specified before implementation begins.

---

# 47. Double-Entry Ledger

The financial subsystem must use immutable double-entry accounting.

Do not model financial truth only as mutable:

```text
seller_balance
supplier_balance
```

Balances may exist as derived snapshots, but the journal remains authoritative.

---

# 48. Ledger Model

Core concepts:

```text
Ledger Account
Journal Transaction
Ledger Entry
Balance Snapshot
Settlement
Payout
Reconciliation
```

Example:

```text
journal_transactions

id
type
reference_type
reference_id
currency
status
created_at
```

```text
ledger_entries

id
journal_transaction_id
ledger_account_id
direction
amount_minor
currency
created_at
```

For every posted journal transaction:

```text
SUM(DEBIT)
=
SUM(CREDIT)
```

per currency.

---

# 49. Ledger Immutability

Posted entries cannot be edited.

Corrections use:

```text
Reversal
+
Replacement Transaction
```

not direct updates.

---

# 50. Example Accounts

Possible accounts include:

```text
Cash / Payment Clearing

COD Receivable

Seller Payable

Supplier Payable

Courier Payable

Payment Provider Payable

Platform Revenue

Shipping Revenue

Refund Liability

Promotional Expense

Settlement In Transit
```

Actual account design must be finalized before ledger implementation.

---

# 51. Financial Idempotency

Strict idempotency required for:

```text
Journal posting
Payment capture
Refund
Settlement creation
Payout creation
Payout confirmation
COD reconciliation
Courier reconciliation
Payment provider reconciliation
```

---

# 52. Settlement Architecture

Use one financial foundation.

Do not build four independent accounting implementations.

Recommended structure:

```text
Finance
│
├── Ledger
├── Accounts
├── Reconciliation
├── Settlement Engine
├── Payouts
└── Policies
      ├── Seller
      ├── Supplier
      ├── Courier
      └── Payment Provider
```

Different parties may have different:

* Settlement cycles
* Fees
* Hold periods
* Minimum payout
* Payout methods
* Dispute rules

but they share the same ledger foundation.

---

# 53. Payout State Machine

Payout is an external side effect and cannot participate in one ACID transaction with PostgreSQL.

Use a state machine:

```text
PROPOSED
APPROVED
RESERVED
PROCESSING
SUCCEEDED
FAILED
REVERSED
```

Example flow:

```text
Calculate Settlement
      ↓
Reserve Payable Amount
      ↓
Commit
      ↓
Call Payment/Bank Provider
      ↓
Receive Confirmation
      ↓
Post Final Ledger Entries
      ↓
Reconcile
```

---

# PHASE 9 — Integration Foundation

External integrations should begin only after internal contracts for:

```text
Products
Listings
Inventory
Orders
Shipping
Payments
Fulfillment
```

are reasonably stable.

---

# 54. Integration Deployment Principle

Every external integration context is independently deployable.

Examples:

## Supplier

```text
salla-supplier-integration
shopify-supplier-integration
woocommerce-supplier-integration
api-supplier-integration
```

## Seller

```text
salla-seller-integration
shopify-seller-integration
woocommerce-seller-integration
easyorders-seller-integration
api-seller-integration
```

There is no requirement for a central Salla Service or Shopify Service.

---

# 55. Integration Isolation

Example:

```text
salla-supplier-integration
```

and:

```text
salla-seller-integration
```

must be independently:

* Deployable
* Versioned
* Scalable
* Observable
* Recoverable
* Releasable

A Seller-specific change must not require Supplier Integration deployment.

---

# 56. Safe Shared Libraries

Shared libraries may include:

```text
logging
tracing
messaging
HTTP utilities
retry utilities
encryption
database helpers
event contracts
```

A tiny provider SDK may be shared only when genuinely generic.

Do not share:

```text
syncProducts()
syncOrders()
syncInventory()
```

business workflows merely because both contexts use Salla or Shopify.

Some duplication is preferable to release coupling.

---

# 57. Supplier Integration Responsibilities

Example:

```text
salla-supplier-integration
```

may handle:

* Product import
* Variant import
* Price import
* Inventory import
* Product availability
* Order export
* Cancellation export
* Fulfillment import
* Webhooks
* Polling
* Reconciliation
* Mapping
* Retries
* Rate limits
* OAuth
* Token refresh

---

# 58. Seller Integration Responsibilities

Example:

```text
salla-seller-integration
```

may handle:

* Product publishing
* Variant publishing
* Price publishing
* Inventory publishing
* Order import
* Customer import where needed
* Cancellation import
* Return import
* Fulfillment status export
* Webhooks
* Reconciliation
* Mapping
* OAuth
* Retries
* Rate limits

---

# 59. Integration Data Ownership

Integration applications own only integration state.

Examples:

```text
connections
external mappings
sync cursors
webhook inbox
sync jobs
reconciliation status
provider metadata
```

They do not own authoritative:

```text
Products
Orders
Inventory
Payments
Balances
```

Commerce Core remains authoritative.

---

# 60. Integration Reconciliation Framework

Every integration must support three synchronization mechanisms where appropriate:

```text
Webhook / realtime updates
        +
Incremental sync
        +
Reconciliation
```

Do not rely exclusively on webhooks.

---

# 61. Synchronization State

Maintain:

```text
last_successful_sync
sync_cursor
last_reconciled_at
external_version
internal_version
mapping_status
sync_status
last_error
```

---

# 62. External Entity Mapping

Example:

```text
external_entity_mappings

connection_id
entity_type
internal_id
external_id
external_version
mapping_status
last_synced_at
conflict_status
```

Enforce uniqueness appropriate to each provider.

---

# 63. Conflict Resolution

Each Integration must explicitly define:

* Internal source of truth
* External source of truth
* Direction of synchronization
* Manual override behavior
* Conflict resolution
* Deletion behavior

Example:

Seller Store:

```text
Platform Inventory
→ External Store

External Order
→ Platform
```

Do not use generic last-write-wins blindly.

---

# 64. Integration Reconciliation

Reconciliation frequency should be provider and data-type specific.

Examples:

```text
Inventory → frequent
Orders → frequent
Fulfillment → frequent
Products → slower
Historical data → background
```

Do not force one global daily full synchronization strategy.

---

# 65. Integration Backpressure

Large synchronization tasks must use:

* Queues
* Batching
* Rate controls
* Debouncing
* Retry windows

Example:

```text
Inventory 10 → 9 → 8 → 7
```

should ideally result in:

```text
external update = 7
```

rather than four redundant API calls when safe.

---

# PHASE 10 — First Supplier Integration

Implement one Supplier Integration end-to-end.

*Historical Status*: Core supplier connectors, sync jobs DB schemas, and internal capability APIs were merged in PR #62 and PR #63.

For an Egypt-first MVP, WooCommerce may be a reasonable first candidate.

Capabilities:

```text
Connect
Authenticate
Import Product (via 10-Stage Product Import Lifecycle)
Import Variants
Import Inventory
Import Pricing
Map External Entities
Reconcile
Push Order
Pull Fulfillment
Handle Webhooks
```

All product and catalog imports must execute through the 10-Stage Product Import Lifecycle:
`Import → Validate → Map → Review → Create/Update Product → Variants/SKUs → Media → Pricing → Inventory → Publish`.

---

# PHASE 11 — First Seller Integration

Implement the matching or most commercially relevant Seller integration.

Capabilities:

```text
Connect
Authenticate
Publish Product (via 10-Stage Product Import Lifecycle)
Publish Variants
Publish Price
Publish Inventory
Import Orders
Handle Cancellations
Push Status
Reconcile
Handle Webhooks
```

---

# PHASE 12 — Public Integration API

After internal integration contracts stabilize, expose a Public API for:

* Custom supplier systems
* Custom seller stores
* Agencies
* Enterprise merchants

*Historical Status*: Core Public Integration API foundation, API keys, webhook dispatcher, rate limiting, and operation idempotency were merged in Core PR #64 and PR #65.

Capabilities:

```text
Products
Inventory
Orders
Fulfillment
Webhooks
```

Require:

* Authentication
* Authorization
* Rate limiting
* Idempotency
* Versioning
* Auditability

---

# PHASE 13 — Unified Marketplace [DEFERRED - LOWEST PRIORITY]

**Strategic Roadmap Status**: DEFERRED TO LOWEST PRIORITY until P0 Seller Stores and P0 Supplier Portal / Wholesale Commerce Operations ecosystems are substantially complete and operationally validated in production.

*Historical Status*: Foundational marketplace capabilities merged through Core PR #70 (curated discovery collection read models in PR #68, platform service authentication in PR #69, marketplace listing resolution and single-store checkout attribution in PR #70) remain fully supported in `matjeroapps/core`.

However, all NEW consumer marketplace features—including multi-seller catalog discovery UI, cross-store search, merchandising, AI/ML ranking algorithms, seller recommendations, and consumer marketplace storefront extensions—are explicitly deferred to the lowest priority.

When consumer marketplace capabilities are eventually resumed, development will build upon the existing curated collection boundaries:

```text
Trending
Best Sellers
Unique Products
New Products
Offers
Fast Delivery
```

Marketplace-generated orders may carry additional commission recorded in the financial ledger.

---

# PHASE 14 — Reputation

Introduce:

## Seller Score

Signals:

```text
Delivered Orders
Cancellation Rate
Return Rate
Ratings
Fulfillment Performance
Sales
Response SLA
```

## Supplier Score

```text
Inventory Accuracy
Fulfillment Speed
Order Accuracy
Return Rate
Product Quality
Stock Reliability
```

## Product Score

```text
Sales Velocity
Conversion
Delivered Rate
Return Rate
Margin
Rating
Saturation
Trend
```

---

# PHASE 15 — Product Intelligence

Initially use deterministic analytics.

Examples:

```text
Best Products
Trending Products
High Margin
Low Return
Rising Products
Saturated Products
Best Suppliers
Best Sellers
```

Introduce ML/AI only after enough historical data exists.

---

# PHASE 16 — Protected Product Access

Introduce seller tiers after platform activity justifies them.

Example:

```text
Starter
Growth
Pro
Elite
```

May control:

* Exclusive products
* Early access
* Reserved supply
* Fees
* Marketplace priority
* Faster settlements

Rules should be transparent and performance-based.

---

# PHASE 17 — Premium Themes and Theme Marketplace

After native storefront success:

```text
Premium Theme Purchase
Theme Licensing
Theme Versioning
Theme Updates
Theme Compatibility
```

Third-party developers may be introduced only after a secure extension model exists.

---

# PHASE 18 — Dropship-to-Microbrand

Advanced phase.

Enable proven sellers to move:

```text
Generic Product
        ↓
Validated Demand
        ↓
Reserved Inventory
        ↓
Custom Packaging
        ↓
Unique Variant
        ↓
Private Label / Microbrand
```

This should not be part of initial MVP scope.

---

# PHASE 19 — Additional Market Expansion

Adding a market should require configuration and provider enablement, not a separate codebase.

Example:

```text
Saudi Arabia
UAE
```

Enable:

* Currency
* Locale
* Shipping providers
* Payment providers
* Marketplace rules
* Integrations

Maintain:

```text
Store Market
=
Supplier Offer Market
```

until cross-border commerce is deliberately designed.

---

# PHASE 20 — Large-Scale Optimization

Scale based on metrics.

---

## API Scaling

Possible future:

```text
admin-api      × 2
supplier-api   × 4
seller-api     × 15
storefront-api × 50
```

---

## Integration Scaling

Example:

```text
salla-seller        × 20
woocommerce-seller  × 8

salla-supplier      × 3
woocommerce-supplier × 2
```

---

# 66. Database Scaling Strategy

Evolution:

```text
PostgreSQL Primary
        ↓
Connection Pooling
        ↓
Read Replicas
        ↓
Partition Selected Tables
        ↓
Separate Databases only when justified
```

Avoid premature:

```text
Sharding
Database-per-service
Distributed SQL
```

---

# 67. Candidate High-Volume Tables

Possible future partitioning candidates:

```text
inventory_movements
order_events
payment_events
shipment_events
webhook_inbox
audit_logs
ledger_entries
```

Only partition when observed size/query behavior justifies it.

---

# 68. Storefront Performance Architecture

Typical path:

```text
Customer
   ↓
CDN
   ↓
Next.js
   ↓
Storefront API
   ↓
Redis
   ↓
PostgreSQL
```

Most browse traffic should avoid PostgreSQL when cached data remains valid.

---

# 69. Object Storage

Product and storefront media should follow:

```text
Dashboard
    ↓
Presigned Upload
    ↓
Object Storage
    ↓
CDN
    ↓
Customer
```

Do not store large binary media in PostgreSQL.

Do not proxy normal media delivery through Go APIs.

---

# 70. Security Architecture

Must include:

* ZITADEL
* MFA support
* Role enforcement
* Object-level authorization
* Tenant isolation
* Input validation
* Rate limits
* Secure headers
* CORS
* CSRF protection where applicable
* Webhook signatures
* Replay protection
* Audit logs
* Secret encryption
* Credential rotation
* Public API security

Integration credentials must never be stored as plaintext.

---

# 71. Webhook Security

For each provider:

```text
Verify Signature
Verify Timestamp where supported
Reject invalid events
Prevent replay
Store raw envelope
Deduplicate
Audit failures
```

Webhook endpoints should perform minimal synchronous work.

---

# 72. Failure Handling

## PostgreSQL unavailable

Transactional operations fail safely.

Do not accept:

* Orders
* Payments
* Inventory reservations
* Financial postings

without authoritative persistence.

---

## Redis unavailable

System should degrade where possible.

Examples:

* Cache misses fall back to PostgreSQL.
* Some rate limits may degrade according to security policy.
* Transactional state remains safe.

---

## RabbitMQ unavailable

Do not lose asynchronous work.

Transactional operations requiring later jobs should use persisted outbox/job records where appropriate.

Core transactions continue using PostgreSQL, and outgoing events remain
recoverable through the Outbox once the broker returns.

---

## External Supplier unavailable

* Do not lose internal orders.
* Queue synchronization.
* Retry.
* Reconcile later.
* Surface degraded integration health.

---

## Seller Store unavailable

* Platform order state remains authoritative.
* External updates queue.
* Retry later.
* Reconcile.

---

## Payment Provider unavailable

Electronic payment checkout may temporarily fail or disable that payment method.

COD may remain available if appropriate.

---

## Shipping Provider unavailable

Order can remain accepted where business rules permit, with shipment creation retried asynchronously.

---

# 73. Testing Strategy

## Unit Tests

Cover:

* Market isolation
* Inventory reservation
* Reservation expiry
* Order transitions
* Payment transitions
* Ledger balancing
* Settlement policies
* Pricing
* Permission rules

---

## Integration Tests

Cover:

```text
PostgreSQL
Redis
RabbitMQ
ZITADEL boundaries
External integration adapters
```

---

## Concurrency Tests

Mandatory for inventory.

Test:

```text
100+ concurrent reservation attempts
against the same inventory pool
```

Verify:

```text
No overselling
No negative availability
Correct reservation count
```

---

## Contract Tests

Especially important for independent integrations.

Verify that:

```text
Commerce Contracts
↔
Integration Deployments
```

remain compatible.

---

## Financial Tests

Must verify:

```text
Debit = Credit
No duplicated posting
Idempotent refund
Idempotent settlement
Payout retry
Reversal correctness
Reconciliation correctness
```

---

## End-to-End

Critical journey:

```text
Supplier Product
      ↓
Inventory
      ↓
Seller Listing
      ↓
Storefront
      ↓
Customer Checkout
      ↓
Inventory Reservation
      ↓
Order
      ↓
Payment/COD
      ↓
Shipment
      ↓
Delivery
      ↓
Ledger Entries
      ↓
Seller/Supplier Settlement
```

---

# 74. Definition of Done

A phase is complete only when:

* Scope is implemented.
* Unit tests pass.
* Integration tests pass.
* Required concurrency tests pass.
* Required financial tests pass.
* Security controls are applied.
* Arabic is tested.
* English is tested.
* RTL is tested.
* LTR is tested.
* Authorization is tested.
* Database migrations are tested.
* API contracts are documented.
* Generated OpenAPI/Swagger specs are updated, validated, and kept in sync with the code whenever endpoints, schemas, auth, pagination, or filtering change.
* Events/contracts are documented.
* Observability exists.
* Failure behavior is tested.
* Deployment succeeds.
* No unresolved blocker exists for dependent phases.

---

# 75. Documentation Structure

Recommended:

```text
docs/
├── architecture/
├── decisions/
├── implementation/
├── integrations/
├── api/
├── finance/
└── backlog/
```

---

## Architecture Decisions

Examples:

```text
ADR-001-postgresql-source-of-truth.md

ADR-002-market-isolation.md

ADR-003-money-representation.md

ADR-004-fulfillment-location.md

ADR-005-inventory-reservation.md

ADR-006-transactional-outbox.md

ADR-007-consumer-inbox.md

ADR-008-double-entry-ledger.md

ADR-009-integration-deployment-model.md

ADR-010-storefront-tenant-resolution.md

ADR-011-theme-security.md

ADR-018-rabbitmq-asynchronous-messaging-backbone.md
```

---

# 76. Commercially Usable MVP

The MVP should remain smaller than the complete roadmap.

## Mandatory MVP

```text
P0 Engineering Foundation

P1 Identity, Localization (Arabic/English, RTL/LTR) & Markets

P2 Commerce Core
   Suppliers, Sellers, Stores, Catalog, Offers, Listings, Inventory

P3 P0 Seller Stores & P0 Supplier Portal / Wholesale Ecosystem Completeness
   Operational Storefront & Seller Retail Store Management
   Operational Supplier Wholesale Management, Catalog & Market Offers

P4 Staged Product Import Lifecycle
   Import -> Validate -> Map -> Review -> Create/Update Product -> Variants/SKUs -> Media -> Pricing -> Inventory -> Publish

P5 Storefront Theme Ecosystem
   Multiple Configurable Storefront Themes
   UI SDK Integration & Strict RTL/LTR Support

P6 Complete Checkout, Orders & Inventory Lifecycle
   Atomic Inventory Reservations
   COD & Electronic Payment Processing
   Shipment State Machine & Fulfillment Workflow

P7 Double-Entry Financial Ledger & Settlements

P8 External Integration Foundation
   Supplier & Seller Connectors

P9 Public Integration API
```

---

# 77. Optional MVP Features

May be added if launch requirements justify them:

```text
Basic analytics
Premium Theme
Additional payment provider
Additional shipping provider
Second external integration
Curated marketplace discovery read models (foundational APIs from PR #68-#70 supported; further development deferred)
```

---

# 78. Deliberately Postponed Features

Do not block MVP on:

```text
Unified Marketplace discovery, search, merchandising, ranking algorithms, recommendations, and cross-store aggregation
AI product recommendations
ML ranking
Protected catalog
Seller tiers
Theme developer marketplace
Microbrand
Cross-border sourcing
Warehouse ownership
Advanced search engine
Database sharding
Kubernetes
Service mesh
```

---

# 79. Post-MVP Roadmap

```text
Public Integration API

Additional Supplier Integrations

Additional Seller Integrations

Reviews and Reputation

Product Intelligence

Protected Product Access

Premium Theme Marketplace

Microbrand

Saudi Expansion

UAE Expansion

Advanced Analytics

Search Infrastructure

Large-Scale Optimization

Unified Marketplace (Consumer Discovery, Search, Merchandising, Ranking, Cross-Store) — Deferred to lowest priority until Seller and Supplier ecosystems are substantially complete
```

---

# 80. Final Architecture

```text
                         ZITADEL
                            │
      ┌─────────────────────┼─────────────────────┐
      │                     │                     │
  Admin Web            Seller Web           Supplier Web
      │                     │                     │
  Admin API             Seller API           Supplier API
      │                     │                     │
      └─────────────────────┼─────────────────────┘
                            │
                      COMMERCE CORE
                            │
               ┌────────────┼────────────┐
               │            │            │
          PostgreSQL      Redis      Transactional
                                     Outbox
                                          │
                                          ▼
                                      RabbitMQ
                                Asynchronous Backbone
                                          │
                ┌─────────────────────────┴────────┐
                │                                  │
        Supplier Integrations              Seller Integrations
                │                                  │
      ┌─────────┼─────────┐              ┌─────────┼─────────┐
      │         │         │              │         │         │
    Salla     Shopify     Woo          Salla     Shopify     Woo
   Supplier   Supplier  Supplier       Seller     Seller     Seller
```

Storefront path:

```text
Customer
   │
   ▼
  CDN
   │
   ▼
Next.js Storefront
   │
   ▼
Storefront API
   │
   ▼
Commerce Core
```

Media:

```text
Seller/Supplier Dashboard
          │
          ▼
   Presigned Upload
          │
          ▼
    Object Storage
          │
          ▼
         CDN
```

---

# 81. Final Architectural Rules

## Rule 1

PostgreSQL is the transactional source of truth.

## Rule 2

Redis is never authoritative for business or financial state.

## Rule 3

The Commerce Core owns business rules.

## Rule 4

Admin, Seller, Supplier, and Storefront APIs are independent actor-facing boundaries.

## Rule 5

Integration deployments are independently deployable and independently scalable.

## Rule 6

Supplier and Seller integrations for the same external platform remain independent.

## Rule 7

Inventory reservation is strongly consistent.

## Rule 8

External synchronization is eventually consistent.

## Rule 9

Every inventory pool belongs to a Fulfillment Location.

## Rule 10

Orders preserve immutable commercial snapshots.

## Rule 11

All financial truth is derived from an immutable double-entry ledger.

## Rule 12

External payouts use state machines and reconciliation, not distributed transactions.

## Rule 13

Transactional Outbox prevents database/event loss.

## Rule 14

Consumers are idempotent using event identity.

## Rule 15

Webhooks use Inbox + Deduplication + Async Processing + Reconciliation.

## Rule 16

Market isolation is enforced in both the domain layer and PostgreSQL constraints.

## Rule 17

Monetary values never use floating-point arithmetic.

## Rule 18

Arabic and English are first-class languages from the beginning.

## Rule 19

Each Seller Store belongs to exactly one Market.

## Rule 20

Cross-border sourcing remains intentionally unsupported initially.

## Rule 21

Themes are versioned and controlled; arbitrary seller JavaScript is not allowed initially.

## Rule 22

RabbitMQ is the sole asynchronous messaging backbone; synchronous inter-service capability calls use HTTP/JSON.

## Rule 23

Scaling decisions must be based on measured operational evidence.

## Rule 24

UI design and implementation across MatjerHub applications must use https://github.com/nextlevelbuilder/ui-ux-pro-max-skill and initialize with `uipro init --ai antigravity`. Shared UI primitives must originate from `@matjerhub/ui-sdk`. Stitch is restricted to MatjerHub Platform UI previously designed with Stitch, and must NOT be used for Seller Storefront Themes unless explicitly requested. Storefront themes may depend on `@matjerhub/ui-sdk` shared components and design tokens, but must NEVER depend on other themes. Business logic must reside outside theme rendering components. Full RTL/LTR and multi-language support remain mandatory across all interfaces.

## Rule 25

All changes across MatjerHub repositories must follow the mandatory delivery workflow: (1) Review prior implementation report, PR diff, and architecture decisions. (2) Fetch latest origin/main and branch directly from origin/main (never from orchestrator or stale feature branches). (3) Write an implementation report in `docs/implementation/<feature>-report.md`. (4) Execute full local test, lint, typecheck, and build commands before pushing. (5) Create a GitHub PR against main. (6) Audit the report against the actual PR diff. (7) Keep blockers and deferred work explicit. (8) Never auto-merge PRs.

---

# 82. Architecture Goal

The final platform is designed to be:

**Multilingual**

**Multi-market**

**Themeable**

**Integration-ready**

**Financially auditable**

**Inventory-safe**

**Event-driven**

**Horizontally scalable**

**Operationally resilient**

while still allowing the first production version to remain operationally lean.
