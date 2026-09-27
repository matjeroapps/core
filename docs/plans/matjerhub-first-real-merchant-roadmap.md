# MatjerHub — First Real Merchant Roadmap

**Status:** Final planning baseline — 2026-09-27  
**Scope:** planning and verification design only; no application implementation

## 1. Executive goal

Make MatjerHub ready for one controlled real supplier and one controlled real
seller in one market, using the complete journey:

`Supplier product -> offer -> Seller import -> Seller Store -> theme -> listing -> publish -> Storefront -> cart/Buy Now -> checkout -> guest order -> Seller operations -> manual fulfillment`

The shortest safe path is milestone-driven, not phase-number-driven. The
Unified Marketplace remains the lowest-priority platform area until this journey
is live and repeatable.

## 2. Repository baseline and audit evidence

`origin/main` is the implementation authority. Historical feature branches and
implementation reports are evidence only and must be reconciled against merged
code before each implementation.

| Repository | Full `origin/main` SHA | Latest merged change | Working checkout caveat |
|---|---|---|---|
| `core` | `54b017b2d3a221ece8fe6ead323074e5807e4be5` | PR #74 — seller/supplier scope clarification | clean `main`, plus this untracked plan |
| `seller` | `2d36528b4aac79f69ef93bcb0ede36730d48c7cc` | PR #36 — Seller API hardening | clean `main` |
| `supplier` | `6339f4875e7497fcda8cc0c917b31bb6f98bf005` | PR #14 — supplier store operations post-merge audit | checkout is on a documentation branch; use `origin/main` |
| `platform` | `7dba33619537f415806344e6066af90122a0ed90` | PR #11 — independent platform CI | checkout is on `codex/independent-platform-ci`; use `origin/main` |
| `platform-infra` | `4b4b0701b098499c7d5759e3e97bc9ca34a09095` | PR #5 — platform service-auth infrastructure | checkout has an uncommitted Compose edit |

Audit actions completed: `git fetch origin` in all five repositories; review of
master plan, ADRs, merged logs, implementation reports, OpenAPI documents,
route registration, frontend route trees, tests, themes, import/checkout code,
Compose profiles, and Makefiles. `platform-infra` Compose configuration parses
successfully. `make up-infra` was attempted and could not start because the
Docker daemon was unavailable; therefore no live service capability is marked
verified here. A full `make up-all` journey is a mandatory M0/M7 gate below.

## 3. Current-state matrix

The columns deliberately distinguish code existence from networked proof.

| Capability | Implemented | Unit/contract tested | Integration tested | Live local infrastructure verified | First-merchant ready |
|---|---|---|---|---|---|
| Core identity, tenant isolation, service auth | Yes | Yes | Partial | No complete five-repo proof | Conditional |
| Core catalog, listings, SKU, media, inventory | Yes | Yes | Yes in Core | Partial | Conditional |
| Core cart, atomic checkout, guest order access | Yes | Yes | Yes | Partial | Conditional |
| Core order lifecycle, outbox, RabbitMQ | Yes | Yes | Partial | No restart/retry proof across stack | Conditional |
| Supplier SSO/profile/markets/locations | Yes | Yes | Contract-level | No live Core-backed portal proof | No |
| Supplier product, EN/AR, category, SKU, offer, MOQ | Yes | Yes | Contract/component | No live media/catalog journey | No |
| Supplier inventory and availability | Yes | Yes | Contract/component | No live cross-service proof | No |
| Seller SSO/store/settings/domain | Yes | Yes | Partial | No complete onboarding proof | No |
| Seller supplier-offer discovery/import | API and UI exist | Yes | Core/Seller contracts | No Supplier -> Seller E2E | No |
| Seller price/presentation/publish/order operations | Yes | Yes | Partial | No complete merchant rehearsal | Conditional |
| Storefront catalog, cart, Buy Now, checkout, orders | Yes | Yes | Partial | No complete browser journey | Conditional |
| Theme registry, Default + Boutique, tokens, preview | Yes | Yes | Partial | Runtime mock proof only | Conditional |
| Arabic/RTL/responsive/error states | Yes at foundation level | Component tests | Partial | No full real-data browser pass | Conditional |
| PostgreSQL/Redis/RabbitMQ/MinIO/Zitadel | Compose services exist | Config checks | Partial | `config` only; boot is M0 | No |
| Workers, seed/reset, backup/restore, observability | Partial | Partial | No | No | No |
| Unified Marketplace | Foundational read models/auth/attribution | Yes | Marketplace-specific | Not critical-path proof | Deferred |

Test inventory observed: Core 102 Go test files; Seller 24 Go and 47 frontend
test files; Supplier 11 Go and 10 frontend test files; Platform 26 frontend
test files. These counts are not cross-service acceptance evidence.

## 4. Completed capabilities — do not reimplement

- Core owns domain models, migrations, authorization, tenant isolation, money,
  inventory reservation, atomic checkout, order state transitions, outbox, and
  the internal OpenAPI contract.
- Seller reaches Core through authenticated HTTP and already has store catalog
  authoring, variants/SKU primitives, S3 presign/complete, locations,
  inventory, listing readiness/publish, Seller order views/transitions, and the
  Storefront API proxy.
- Supplier already exposes authenticated profile, markets, locations, products,
  categories, offers, inventory snapshots/movements, sync-job records, and the
  secondary affiliated retail capability.
- Direct supplier-offer discovery/import exists in Seller API/UI. It is a
  contract foundation, not yet a live first-merchant proof.
- Storefront already has host resolution, catalog/category/product/search,
  cart, Buy Now, checkout finalize, guest order retrieval/cancel, localization,
  RTL direction, responsive layouts, error boundaries, and two registered themes.
- `platform-infra` already defines profiles and services for PostgreSQL, Redis,
  RabbitMQ, MinIO, Zitadel, Core, workers, Seller, Supplier, Admin, and Platform.
- ADR-017 repository independence and service-auth boundaries are established.

## 5. Remaining gaps and corrections to the prior roadmap

1. The former P0 items were too broad. They are split into M0–M8 executable
   increments with explicit cross-repository verification.
2. Supplier UI/API was described as complete from component evidence; it now has
   a live Core/MinIO/SSO gate before readiness is claimed.
3. Seller import was described as present but lacked explicit update,
   unavailable, archived-product, conflict, and audit semantics.
4. Theme work now includes install, preview, switch, rollback, published-store
   behavior, token isolation, both locales, mobile, and real-data runtime proof.
5. Checkout/order work now explicitly includes COD/payment-pending policy,
   cancellation and reservation release, timeline/correlation IDs, outbox,
   restart/retry behavior, and Seller visibility.
6. `make config` is not equivalent to live infrastructure verification. M0 and
   M7 require actual `platform-infra` services and a browser journey.
7. Bulk import, external payments/shipping, connectors, analytics, promotions,
   returns, customer IAM expansion, domain automation, and Marketplace UX are
   moved out of the critical path.

## 6. Milestone architecture

```text
M0 Baseline & infrastructure reproducibility
  -> M1 Supplier production-ready vertical slice
  -> M2 Supplier -> Seller product import
  -> M3 Seller Store setup + theme workflow
  -> M4 Storefront + multiple themes
  -> M5 Cart / checkout / guest orders
  -> M6 Seller order operations + manual fulfillment
  -> M7 Full first-merchant rehearsal
  -> M8 Controlled first live merchant
  -> P1 credible MVP expansion -> P2 improvements -> P3 deferred platform
```

Cross-cutting ownership: Core owns business invariants and migrations; Supplier
and Seller own their BFF/API and UI; Seller owns the native Storefront runtime;
platform-infra owns orchestration and reproducibility; Platform is not a blocker
unless the selected launch requires its public shell.

## 7. Detailed P0 milestone backlog

Every item below is a separate future branch/PR/report. Every row explicitly
states API, DB, UI, security, cross-repository, verification, blocking status,
required PRs, and report requirements.

### M0 — Baseline and infrastructure reproducibility

#### M0-01 — Fresh-stack baseline and release fixture

- **Repository:** `platform-infra` plus `core`, `seller`, `supplier` verification.
- **Business objective:** Reproduce the actual networked journey without SQL or
  curl-only setup.
- **Dependencies:** none; start from each fresh `origin/main`.
- **API impact:** verify health/readiness and existing internal/actor routes;
  no new business endpoint unless a proven readiness gap requires it.
- **DB/migration impact:** verify migrations, deterministic seed, reset, backup,
  and restore; no schema change by default.
- **UI/UX impact:** document service URLs and test accounts; no UI feature.
- **Security impact:** fail closed on missing tokens/OIDC; keep secrets in env,
  never browser bundles or committed files.
- **Cross-repository impact:** boot PostgreSQL, Redis, RabbitMQ, MinIO, Zitadel,
  Core, workers, Seller, Supplier, and relevant Storefront services together.
- **Verification:** `make config`; `make up-infra`; `make up-core`; `make up-workers`;
  `make up-seller`; `make up-supplier`; then `make up-all`; health/readiness,
  migration, seed/reset, media bucket, queue, and network checks.
- **Definition of Done:** a clean checkout reaches all required health endpoints,
  creates the seeded fixture, resets it, restores a backup, and records any
  environment limitation instead of claiming PASS.
- **Blocking:** YES. **Required PRs:** one `platform-infra` PR, plus only
  narrowly required Core/Seller/Supplier PRs. **Report:** `platform-infra/docs/implementation/m0-fresh-stack-rehearsal-report.md`.

#### M0-02 — Baseline reconciliation and contract inventory

- **Repository:** all five repositories.
- **Business objective:** prevent implementation from following stale reports or
  feature branches.
- **Dependencies:** M0-01 baseline.
- **API/DB/UI impact:** inventory only; record route/OpenAPI/schema ownership and
  existing UI paths, with no application behavior change.
- **Security impact:** record caller/service-token boundaries and trust headers.
- **Cross-repository impact:** map each Supplier -> Core -> Seller -> Storefront
  call and its error contract.
- **Verification:** compare merged PRs, current routes, OpenAPI, tests, and
  reports; run documentation path audit.
- **Definition of Done:** full SHAs, owners, known limitations, and evidence
  links are recorded in this roadmap and the M0 report.
- **Blocking:** YES. **Required PRs:** documentation PR per repository only if
  baseline facts need correction. **Report:** repository-relative M0 report.

### M1 — Supplier production-ready vertical slice

#### M1-01 — Live Supplier catalog publication

- **Repository:** `supplier` + `core`.
- **Business objective:** let a supplier independently publish sellable supply.
- **Dependencies:** M0-01; existing supplier/core product and offer contracts.
- **API impact:** close only verified gaps in product, category, variant/SKU,
  media, offer, location, inventory, availability, and publish contracts; update
  OpenAPI and drift tests if contracts change.
- **DB/migration impact:** prefer existing schema; add migration only if current
  lineage/readiness cannot represent the required state.
- **UI/UX impact:** guided workspace for SSO -> product -> EN/AR -> category ->
  variant/SKU -> media -> offer -> wholesale price/MOQ -> location/inventory ->
  publish. Any new UI uses `uipro init --ai antigravity`; Stitch is not required
  for Storefront/Supplier work.
- **Security impact:** supplier subject resolves through Core; no caller-supplied
  supplier identity; media authorization and payload limits remain enforced.
- **Cross-repository impact:** real Supplier BFF -> Core HTTP -> PostgreSQL/MinIO.
- **Verification:** unit/contract, Core integration, live Compose with SSO and
  MinIO, browser smoke in English and Arabic/RTL.
- **Definition of Done:** a supplier creates the complete product/offer/location/
  inventory flow with no manual DB/API intervention, publishes it, and the
  eligible Seller can see exactly the expected offer facts.
- **Blocking:** YES. **Required PRs:** `core` and `supplier` PRs as needed.
  **Report:** `docs/implementation/m1-supplier-vertical-slice-report.md` in
  the owning repository (or coordinated reports if both change).

#### M1-02 — Supplier fact/presentation boundary audit

- **Repository:** `core`, `supplier`, `seller`.
- **Business objective:** ensure supplier-owned facts never overwrite Seller
  retail presentation.
- **Dependencies:** M1-01.
- **API impact:** verify DTOs distinguish supplier product/offer facts from
  seller listing price, presentation, and publish status.
- **DB/migration impact:** verify foreign keys and ownership; no duplicate
  `source_type` state or unnecessary schema.
- **UI/UX impact:** label wholesale cost/MOQ/availability separately from seller
  price and merchandising fields.
- **Security impact:** public payloads never expose supplier economics; tenant and
  role boundaries are tested.
- **Cross-repository impact:** contract fixtures in Core, Supplier, Seller, and
  Storefront.
- **Verification:** negative authorization tests, payload privacy tests, and
  live inspection of Seller and public Storefront responses.
- **Definition of Done:** changing a supplier fact cannot mutate Seller-owned
  price/presentation, and no supplier-private field reaches customers.
- **Blocking:** YES. **Required PRs:** only where a gap is found. **Report:**
  attach to the M1 implementation report.

### M2 — Supplier -> Seller product import

#### M2-01 — Idempotent eligible-offer import contract

- **Repository:** `core` + `seller` (Supplier is the source contract).
- **Business objective:** turn a published Supplier offer into one stable Seller
  listing without engineer intervention.
- **Dependencies:** M1-01/M1-02; eligible offer visibility.
- **API impact:** define eligibility (market, status, availability, seller
  access), idempotency key/natural key, retry/conflict errors, lineage, and
  audit/correlation IDs; update Seller/Core OpenAPI if needed.
- **DB/migration impact:** reuse existing seller-listing/offer/SKU/media
  relations; add unique constraint or import mapping only if duplicate safety
  cannot be enforced with current data.
- **UI/UX impact:** Seller discovery list, import progress, duplicate/already
  imported state, conflict/error recovery, and no hidden manual API step.
- **Security impact:** seller subject/store scope enforced by Core; one seller
  cannot import into another store; supplier facts remain immutable to Seller.
- **Cross-repository impact:** real Supplier offer -> Core -> Seller API/client ->
  Core listing creation; Storefront consumes resulting listing.
- **Verification:** Core contract/race tests; Seller client/API tests; live Core+
  Supplier+Seller stack; browser import E2E.
- **Definition of Done:** repeated import creates one listing; product/offer/SKU
  lineage and media remain stable; initial seller price is explicit; failures
  are retryable and audited.
- **Blocking:** YES. **Required PRs:** coordinated `core` and `seller` PRs.
  **Report:** `docs/implementation/m2-product-import-report.md`.

#### M2-02 — Import update/unavailable lifecycle

- **Repository:** `core` + `seller`.
- **Business objective:** define safe behavior after import when supplier data
  changes.
- **Dependencies:** M2-01.
- **API impact:** specify supplier price change, inventory change, offer
  unavailable, product archived, retry, and conflict responses; preserve seller
  price/presentation ownership.
- **DB/migration impact:** no schema by default; if state is needed, add a
  minimal migration with backfill/rollback and explicit listing status rules.
- **UI/UX impact:** Seller sees stale/unavailable warnings and can unpublish or
  correct presentation; customers see unavailable state, not a broken product.
- **Security impact:** update workers/events cannot cross store boundaries or
  expose supplier costs.
- **Cross-repository impact:** event or pull contract between Core and Seller;
  Storefront availability must match checkout authority.
- **Verification:** integration race tests plus live update/unavailable E2E,
  checkout rejection after stock/price change, and retry/recovery.
- **Definition of Done:** every transition has deterministic behavior and an
  operator-visible audit/correlation trail.
- **Blocking:** YES for supplier-backed first merchant. **Required PRs:** as
  needed in Core/Seller. **Report:** appended to M2 report.

### M3 — Seller Store setup and theme workflow

#### M3-01 — Guided Seller Store onboarding

- **Repository:** `seller` + `core`.
- **Business objective:** complete SSO -> create Store -> market/currency -> theme
  -> import -> price/presentation -> readiness -> publish.
- **Dependencies:** M0-01, M2-01, existing store/domain/theme APIs.
- **API impact:** close missing store settings, theme installation, preview,
  readiness, and publish contracts only where UI cannot complete the journey.
- **DB/migration impact:** reuse stores, domains, themes, listings, and settings;
  migrations only for an evidenced missing invariant.
- **UI/UX impact:** one guided flow with loading, empty, conflict, unauthorized,
  and retry states. Apply `uipro init --ai antigravity` before new UI/UX work.
- **Security impact:** store ownership, subject-derived identity, preview-token
  protection, and no trusting client store IDs for authorization.
- **Cross-repository impact:** Seller BFF -> Core; selected published host reaches
  Seller Storefront.
- **Verification:** Seller unit/contract tests, live Core-backed API tests, and
  browser E2E with no SQL/curl/manual records.
- **Definition of Done:** a new Seller completes setup and opens its storefront
  using only the UI and documented environment.
- **Blocking:** YES. **Required PRs:** `seller` plus `core` only if contract gaps.
  **Report:** `docs/implementation/m3-seller-onboarding-report.md`.

#### M3-02 — Theme lifecycle contract

- **Repository:** `core` + `seller`.
- **Business objective:** make multiple themes safe to install, preview, switch,
  publish, and roll back.
- **Dependencies:** M3-01; existing theme registry, tokens, preview contracts.
- **API impact:** theme catalog/install/preview/publish/switch contracts and
  revision invalidation; no duplicated commerce APIs.
- **DB/migration impact:** verify existing theme version/config persistence;
  migration only for a missing safe-switch invariant.
- **UI/UX impact:** Theme A and Theme B selection, install, preview, publish,
  switch confirmation, rollback/error state, Arabic/RTL and mobile states.
- **Security impact:** validate theme keys/configuration, isolate tokens/assets,
  sign preview tokens, and prevent arbitrary template execution.
- **Cross-repository impact:** Core theme domain -> Seller API -> Storefront
  registry; Platform Stitch is not required unless Platform UI is changed.
- **Verification:** theme unit/contract tests, live published-store switching,
  preview/published parity, and browser E2E in both themes/locales.
- **Definition of Done:** switching themes changes presentation only; catalog,
  cart, checkout, order behavior, tenant, and seller data remain unchanged.
- **Blocking:** YES (multiple themes are high priority). **Required PRs:** as
  needed in Core/Seller. **Report:** `docs/implementation/m3-theme-lifecycle-report.md`.

### M4 — Storefront and themes

#### M4-01 — Live Storefront runtime and media proof

- **Repository:** `seller` (Storefront) + `core`.
- **Business objective:** make the published Seller Store usable by customers.
- **Dependencies:** M3-01/M3-02 and M2 import.
- **API impact:** verify public Storefront catalog, category, product, search,
  cart, Buy Now, checkout, and guest-order contracts; no direct browser/Core
  access.
- **DB/migration impact:** none expected; verify Storefront revision/cache
  invalidation against published changes.
- **UI/UX impact:** real product/media/price/availability; home/category/list/
  detail/search; loading, empty, unavailable, error, mobile, Arabic/RTL.
- **Security impact:** trusted host resolution, payload privacy, bounded upstream
  responses, secure cookies, and no internal service URL leakage.
- **Cross-repository impact:** browser -> Storefront API -> Core, with Seller
  host authority and MinIO media.
- **Verification:** live services and MinIO plus browser E2E for Theme A/B,
  English/Arabic, and mobile viewport.
- **Definition of Done:** imported product renders and is actionable from both
  themes on the real tenant host; no mock data remains on the acceptance path.
- **Blocking:** YES. **Required PRs:** `seller` and only necessary `core`.
  **Report:** `docs/implementation/m4-storefront-runtime-report.md`.

### M5 — Cart, checkout, and guest orders

#### M5-01 — Operational checkout policy and browser flow

- **Repository:** `core` + `seller`.
- **Business objective:** accept one real customer order without external payment.
- **Dependencies:** M4-01; published listing and inventory.
- **API impact:** explicitly support COD and/or `payment_pending`; verify cart,
  checkout session, address/email, finalize, order reference, guest capability,
  retrieval, and cancellation contracts.
- **DB/migration impact:** reuse atomic reservation/order/session schema; no new
  payment schema unless required by the policy.
- **UI/UX impact:** cart and Buy Now isolation, checkout form, localized errors
  for price/stock/expiry, confirmation, order-status page, and cancel behavior.
- **Security impact:** server-side price/currency revalidation, atomic inventory
  reservation, duplicate finalize protection, capability-token privacy, and
  guest authorization by resolved host/store.
- **Cross-repository impact:** Storefront -> Seller API -> Core -> PostgreSQL/
  RabbitMQ; response shapes must remain privacy-safe.
- **Verification:** existing concurrency/integration tests plus live browser E2E,
  checkout replay, low inventory, price change, expiry, and unavailable product.
- **Definition of Done:** one finalize creates exactly one order, reserves stock,
  emits the outbox event, returns a reference, and allows safe guest retrieval.
- **Blocking:** YES. **Required PRs:** coordinated Core/Seller only if gaps.
  **Report:** `docs/implementation/m5-checkout-orders-report.md`.

### M6 — Seller order operations and manual fulfillment

#### M6-01 — Seller order lifecycle and fulfillment rehearsal

- **Repository:** `core` + `seller`.
- **Business objective:** make the order operationally actionable after checkout.
- **Dependencies:** M5-01; existing Seller order transitions/shipping foundation.
- **API impact:** verify list/detail, pending -> confirmed -> processing ->
  ready-for-shipping, cancellation, inventory release, timeline, and manual
  shipment/fulfillment note contracts.
- **DB/migration impact:** reuse order timeline, reservations, shipment tables;
  migration only for a proven missing manual-fulfillment invariant.
- **UI/UX impact:** Seller order queue, detail, allowed actions, errors,
  correlation/reference display, and manual fulfillment status.
- **Security impact:** seller store scope, no supplier cost leakage, transition
  authorization, idempotent action behavior.
- **Cross-repository impact:** Core order state -> Seller dashboard; outbox event
  and worker restart/retry must be observable.
- **Verification:** state-machine tests, live Seller dashboard E2E, cancellation
  and reservation-release test, RabbitMQ/outbox restart drill.
- **Definition of Done:** Seller sees and operates the real customer order
  through manual fulfillment without SQL/curl.
- **Blocking:** YES. **Required PRs:** Core/Seller as needed. **Report:**
  `docs/implementation/m6-order-operations-report.md`.

### M7 — Full first-merchant rehearsal

#### M7-01 — Cross-repository acceptance rehearsal

- **Repository:** all five repositories; primary report in `platform-infra`.
- **Business objective:** prove the actual networked journey, not isolated mocks.
- **Dependencies:** M0-01 through M6-01.
- **API/DB/UI/security impact:** no feature change; verify all existing boundaries,
  migrations, logs, and payload privacy under real traffic.
- **Cross-repository impact:** `Supplier -> Core -> Seller -> Storefront -> Core -> Seller`.
- **Verification:** `make up-all` (or documented profile sequence), real
  PostgreSQL/Redis/RabbitMQ/MinIO/Zitadel/Core/Seller/Supplier/workers, browser
  journey repeated for Theme A/B, English, Arabic/RTL, mobile, duplicate import,
  unavailable product, price change, low inventory, and checkout replay.
- **Definition of Done:** second operator reproduces the journey from a clean
  environment; every failure is fixed or documented as an explicit blocker;
  evidence includes request/correlation IDs, logs, screenshots, and timestamps.
- **Blocking:** YES. **Required PRs:** no application PR unless rehearsal finds
  a defect; release evidence PR/report required. **Report:**
  `platform-infra/docs/implementation/m7-first-merchant-rehearsal-report.md`.

### M8 — Controlled first live merchant

#### M8-01 — Limited launch gate

- **Repository:** all; operational owner in `platform-infra`.
- **Business objective:** onboard one real supplier and Seller safely.
- **Dependencies:** signed M7 evidence and release checklist.
- **API/DB/UI impact:** no new feature by default; freeze approved contracts,
  seed/backup, and runbook.
- **Security impact:** production secrets, OIDC redirect policy, host/TLS,
  access review, least privilege, incident contact, rollback owner.
- **Cross-repository impact:** production-like deployment of the same service
  boundaries proven in M7.
- **Verification:** controlled onboarding, first product, first order, manual
  fulfillment, support observation, rollback drill readiness.
- **Definition of Done:** merchant completes the acceptance criteria below with
  no engineer-created records; launch is limited to one market/store and has a
  documented stop/rollback condition.
- **Blocking:** milestone itself is the launch decision. **Required PRs:**
  release/evidence PRs only. **Report:** first-live-store checklist/report.

## 8. P1 credible MVP expansion

P1 starts only after M8. Each row is a separate scoped PR and implementation
report; none may silently enter M0–M8.

| ID / repository | Feature and objective | Dependencies; API/DB/UI/security/cross-repo scope | Verification and DoD | Blocks first merchant? |
|---|---|---|---|---|
| P1-01 `supplier` + `core` | Supplier multi-SKU editing breadth so real catalogs can be maintained | Existing product/variant/SKU contracts; schema only for evidenced gaps; Supplier editor; preserve tenant and supplier-fact ownership; Seller import regression | Unit/contract/integration plus live edit/archive/SKU-conflict E2E; report attached to PR | NO for one-SKU pilot |
| P1-02 `seller` + `core` | Richer Seller presentation and catalog management | Existing listing/presentation/price APIs; no supplier-fact mutation; localized editor and readiness; store-scope authorization | API/UI/live storefront regression; report attached to PR | NO |
| P1-03 `seller` + `core` | Theme customization and safer preview/publish controls | Existing registry/tokens/preview/revision contracts; no duplicate commerce API; RTL/mobile/accessibility; preview-token and theme validation | Both themes, rollback, live published-store E2E; report attached to PR | NO, but high priority |
| P1-04 `core` + `seller` | Manual shipment tracking and payment-pending operations | Reuse shipment/order state tables; no gateway required; Seller order UI; transition authorization and audit | State/API/live dashboard tests and restart drill; report attached to PR | NO |
| P1-05 `platform-infra` | Deterministic demo tenant, notifications, and operational observability | Seed/reset/runbook updates; no domain change unless required; operator UX/logs; secrets/correlation IDs | Clean-reset, backup/restore, log/alert rehearsal; report attached to PR | NO |
| P1-06 `core` + `seller` | Custom-domain lifecycle and storefront edge hardening | Existing domain APIs/DNS boundary; migrations only if invariant gap; seller/admin UI; TLS/DNS authorization | DNS-state integration and staging edge smoke; report attached to PR | NO (platform subdomain is MVP) |

## 9. P2 post-first-merchant improvements

Bulk import (the ten-stage Import -> Validate -> Map -> Review -> Create/Update
-> Variants/SKUs -> Media -> Pricing -> Inventory -> Publish lifecycle), external
payment gateways, shipping carriers, returns/refunds automation, promotions,
advanced analytics, customer IAM expansion, connector execution, Salla/Zid/ERP
integrations, image/CDN optimization, and broader market/currency support.

## 10. P3 deferred platform and marketplace work

Unified Marketplace discovery, ranking, recommendations, merchandising,
cross-store consumer UX, reputation/intelligence, protected commerce,
microbrand features, broad aggregation, and advanced domain automation. Existing
marketplace read models, service auth, listing resolution, and checkout
attribution remain supported Core foundations; no new Marketplace UX may
displace M0–M8.

## 11. Dependency graph and ownership matrix

| Dependency | Owner | Consumes | Produces |
|---|---|---|---|
| Identity/tenant/service auth | Core; actor auth in Seller/Supplier | Zitadel subject and service token | authorized actor context |
| Supplier catalog facts | Core + Supplier | supplier subject, market, media, inventory | published supplier offer |
| Offer import | Core + Seller | eligible offer and Seller store | idempotent seller listing lineage |
| Theme lifecycle | Core + Seller | store and theme registry | preview/published theme selection |
| Storefront runtime | Seller | host, published listing/theme | customer catalog/cart/checkout UI |
| Checkout/order | Core + Seller | cart, listing, inventory, address | guest order, outbox event |
| Manual fulfillment | Core + Seller | order state and shipment foundation | operated order lifecycle |
| Rehearsal/orchestration | platform-infra | all service images and env | reproducible evidence |

Repository ownership remains: Core owns domain/migrations/invariants; Supplier
owns Supplier API/UI; Seller owns Seller API/UI and native Storefront; Platform
owns its public shell; platform-infra owns Compose/profiles/readiness/runbooks.

## 12. First Real Merchant acceptance criteria

### Supplier

SSO; isolated workspace; product; EN/AR content; category; variant/SKU; real
MinIO media; market offer; wholesale price; MOQ; availability; fulfillment
location; inventory; publish; eligible Seller visibility. Supplier-owned facts
must remain distinct from Seller retail price/presentation.

### Seller Store

SSO; create Store; configure market/currency; select/install/preview/publish
Theme A or B; discover eligible offer; import once idempotently; set Seller
price; configure presentation; pass readiness; publish; open the real Storefront
host. No engineer-created SQL/curl records.

### Storefront and themes

Home/category/list/product/search; real media and availability; cart and Buy
Now; both themes with isolated tokens/registry; safe switch/rollback; Arabic/
English and RTL; mobile; loading/empty/error/unavailable states; no supplier
private data.

### Checkout and orders

COD/payment-pending policy; guest checkout; shipping address/contact; atomic
reservation; price revalidation; duplicate finalize protection; guest capability;
order reference/status; cancellation rules and stock release; timeline and
correlation ID; outbox `order.created`; restart/retry proof; Seller visibility.

## 13. Verification levels and release checklist

For each milestone, use the highest applicable level:

`Unit -> Contract -> Integration -> Live local infrastructure -> Browser E2E -> Operational rehearsal`

Cross-service work is not complete at unit, mock, stub, or component level when
real local services can run. An environment limitation must be recorded as
**not verified**, never PASS.

Release checklist:

- [ ] all five reviewed `origin/main` SHAs are recorded;
- [ ] fresh checkout builds/tests/lints and OpenAPI drift checks pass;
- [ ] `make config`, `make up-infra`, `make up-core`, `make up-workers`,
      `make up-seller`, `make up-supplier`, and `make up-all` are documented;
- [ ] PostgreSQL migrations, seed, reset, backup, and restore pass;
- [ ] Redis, RabbitMQ, MinIO, Zitadel, Core, Seller, Supplier, workers, and
      required Platform services are healthy;
- [ ] real Supplier -> Seller import passes duplicate/update/unavailable cases;
- [ ] both themes pass English/Arabic/RTL/mobile Storefront smoke;
- [ ] checkout creates one order and Seller manually fulfills it;
- [ ] outbox/RabbitMQ survives restart and correlation IDs are traceable;
- [ ] security, secret, payload-privacy, tenant-isolation, and path audits pass;
- [ ] M7 second-operator rehearsal evidence is attached;
- [ ] M8 launch owner, support contact, rollback, and merchant limits are named.

## 14. PR, branch, report, and documentation discipline

Before every future implementation: `git fetch origin`; start from fresh
`origin/main` (never stale local main or a historical feature branch); create a
new feature branch; implement only the scoped item; run the required levels of
verification; create a PR; and add a repository-relative implementation report
under `docs/implementation/`. Reconcile the report against the actual PR diff,
tests, branch/base, and merged baseline before declaring completion.

Before PR, audit documentation for file-URI links, home-directory prefixes,
system web-root prefixes, and other machine-specific paths. Use
repository-relative paths only. New UI/UX
implementation requires `uipro init --ai antigravity`; Stitch MCP is only for
Platform UI work that specifically depends on existing Stitch designs.

## 15. Recommended Next Implementation Task

**M0-01 — Fresh-stack baseline and release fixture in `platform-infra`.**

This is one concrete increment, not “finish Supplier/Seller.” It must boot the
actual PostgreSQL, Redis, RabbitMQ, MinIO, Zitadel, Core, workers, Seller, and
Supplier services with the existing Makefile/profile commands; prove health,
migrations, seed/reset, and service networking; and publish a reproducible
evidence report. It is the optimal next task because every unresolved business
bridge (especially M1 and M2) is otherwise being judged through mocks or
isolated repositories, while the requested milestone requires proof of the real
`Supplier -> Core -> Seller -> Storefront -> Core -> Seller` boundary.
