# ADR-020: Canonical Merchant Identity, Membership, and Capabilities

## Status

Accepted

## Context

Core currently represents Retail and Supply businesses through separate `sellers` and `suppliers` tenant roots, separate `seller_members` and `supplier_members`, separate ZITADEL role families, and separate actor-service resolution paths. ADR-019 added a strict one-to-one `supplier_seller_affiliations` relation so a Supplier could create and operate a Seller retail profile without weakening store ownership or sourcing lineage.

That design safely enabled the initial capability but left one business represented by two authorization tenants and two teams. A unified operator experience requires one canonical business identity, one membership boundary, and explicit capabilities without rewriting the commerce aggregates that correctly preserve Seller-owned stores and Supplier-owned offers.

The approved unified Merchant master plan establishes Merchant as that canonical tenant. This ADR defines the target decision and the evolutionary migration rules needed to reach it without guessing identity, breaking current clients, or exposing internal sourcing data.

## Decisions

### 1. Merchant is the canonical tenant

`Merchant` is the canonical business and authorization tenant in Core.

- Every canonical membership and permission is scoped to one Merchant.
- A subject may belong to more than one Merchant; authentication does not select or merge a tenant.
- Descriptive attributes such as email, person or company name, code, phone, address, and domain are never business-identity evidence.
- A shared ZITADEL subject is evidence only that the same principal has separately recorded memberships; it is not evidence that the businesses are the same Merchant.
- Core remains the sole authority for Merchant identity, profile links, memberships, permissions, capabilities, commerce ownership, and tenant isolation.

### 2. Retail and Supply are independent Merchant capabilities

A Merchant may have Retail, Supply, or both capabilities. Each capability has an independent lifecycle:

- `inactive`
- `activating`
- `active`
- `suspended`

Merchant lifecycle is separately `active` or `suspended`. Merchant suspension denies every capability. Capability suspension denies only that capability. Activation is atomic: required profile creation/linkage and readiness checks either all succeed and make the capability active, or all roll back and leave it inactive.

### 3. Seller and Supplier remain evolutionary capability profiles

During migration:

- a Seller profile is the Retail capability profile linked explicitly to one Merchant;
- a Supplier profile is the Supply capability profile linked explicitly to one Merchant;
- one Seller profile or Supplier profile links to at most one Merchant;
- one Merchant has at most one active Seller profile and at most one active Supplier profile;
- explicit Merchant profile linkage replaces `supplier_seller_affiliations` as the canonical identity relation.

The existing profile IDs remain stable and the legacy affiliation remains available as backfill provenance and compatibility evidence until separately approved cleanup.

### 4. Commerce ownership and lineage do not move

Merchant adds a tenant and authorization layer. It does not replace valid aggregate ownership:

- Stores remain owned through `stores.seller_id`.
- Supplier products and offers remain owned through their Supplier profile, including `supplier_offers.supplier_id`.
- Seller listings remain Store-owned and retain explicit `product_id` and `supplier_offer_id` lineage.
- Order-item Supplier snapshots, inventory reservations, shipments, payments, ledger entries, settlements, and allocations remain Core-authoritative.
- No `store.merchant_id` or `store.supplier_id` shortcut replaces the Seller ownership chain during this evolutionary migration.

### 5. Membership is unified and permissions are capability-scoped

Canonical authorization uses one `MerchantMembership` per exact `(merchant_id, identity_subject)` and explicit permission grants scoped to Merchant, Retail, or Supply actions.

Permission families include Merchant profile/capability/team management; Retail profile/team/store/catalog/inventory/order actions; and Supply profile/team/catalog/offer/inventory/order/financial actions. Resource access requires all of:

1. an active Merchant;
2. an active Merchant membership for the exact subject;
3. an active required capability;
4. the required scoped permission;
5. a resource ownership chain that resolves to the same Merchant profile link.

No role or permission may authorize a Store through a Merchant's Supplier profile or authorize a Supplier Offer through its Seller profile.

### 6. Legacy roles are compatibility inputs

Current Seller and Supplier ZITADEL roles remain supported during migration, but they are not canonical Merchant roles and cannot establish Merchant identity.

- Seller roles translate only to evidenced Retail grants.
- Supplier roles translate only to evidenced Supply grants.
- Broad Merchant owner/team permissions are granted by backfill only when the same exact subject is an active owner of every active capability profile.
- Different active owner sets create a reconciliation exception; each owner keeps capability-scoped authority and no owner is arbitrarily elevated.
- Existing Seller and Supplier actor routes remain compatibility adapters through the observation window.

Legacy authorization remains authoritative in legacy and shadow modes. Merchant authorization becomes authoritative only after the cutover gates pass. At that point legacy routes resolve their existing profile ID through the Merchant link and preserve their established request and response contracts.

### 7. Deterministic backfill and quarantine

The migration classification is exhaustive:

- standalone Seller → one Merchant with Retail active and a linked Seller profile;
- standalone Supplier → one Merchant with Supply active and a linked Supplier profile;
- one valid explicit Seller/Supplier affiliation → one Merchant with both capabilities and both profile links;
- invalid, conflicting, or non-deterministic evidence → reconciliation quarantine with no canonical authorization cutover.

A valid `supplier_seller_affiliations` row is the only current evidence allowed to combine one Seller and one Supplier into a Merchant. Email, names, codes, company details, and shared subjects are prohibited matching inputs.

The future backfill is dry-run-first, batched, idempotent, and resumable. It persists a source-to-Merchant crosswalk before canonical row creation, emits immutable audit manifests and bounded-cardinality metrics, and accounts for every source profile exactly once as mapped or quarantined.

Duplicate subjects are consolidated into one Merchant membership using exact subject equality. Grants are the union of source-scoped translations, never a union of broad roles. Mixed statuses, unknown roles, missing owners, and conflicting owners are auditable exceptions and block canonical authorization cutover for the affected Merchant.

### 8. Cutover, rollback, and cleanup are gated

Rollout proceeds through backward-compatible schema, dry-run, backfill, shadow-read, canonical-read, and compatibility-observation gates.

- Before canonical cutover, Seller/Supplier membership and authorization remain authoritative. New Merchant-side rows created by a failed run may be regenerated, while crosswalk and audit history are retained.
- After canonical cutover, rollback switches reads and authorization to the legacy sources, disables Merchant-native writes, and retains Merchant data for reconciliation. It does not delete or reverse-guess canonical records.
- Canonical cutover is prohibited for any Merchant with an open identity or ownership exception.
- Legacy memberships and affiliation identity reads may be removed only by a separately approved cleanup after at least 14 consecutive healthy observation days and verified client retirement.
- Seller/Supplier profiles and commerce foreign keys are not cleanup targets of this ADR.

### 9. Events and observability use existing Core foundations

Merchant domain events use the existing versioned Core event envelope and transactional outbox/inbox delivery. Versioned events cover capability activation/suspension, membership changes, permission changes, profile linkage, and operationally actionable reconciliation exceptions.

Shadow and dual reads compare legacy and Merchant profile resolution and authorization outcomes without changing client responses before cutover. Any confirmed cross-tenant allow, unauthorized Merchant allow, duplicate profile mapping, active capability without a profile, or public sourcing-data leak triggers rollback.

Metric labels remain bounded and do not contain Merchant IDs, profile IDs, subjects, or run IDs. Those identifiers may appear only in access-controlled structured logs and audit artifacts.

### 10. Public storefront privacy remains strict

Merchant is an internal/authenticated actor concept, not a reason to expose sourcing information publicly. Customer-facing contracts must not expose:

- Supplier identity or affiliation;
- OWN/NETWORK or other sourcing classifications;
- `supplier_offer_id` or internal profile links;
- wholesale price, cost, margin, commission, or settlement allocation;
- source availability or internal fulfillment routing.

Public storefront ranking and presentation remain independent of internal Supplier affiliation and Merchant capability state, except that suspended/unpublished Retail resources fail closed.

## ADR-019 relationship

ADR-020 partially supersedes ADR-019 as follows:

- ADR-019 Decision 1 remains correct that Retail and Supply are capabilities, but its Supplier-to-Seller hierarchy is superseded as the canonical business/identity model.
- ADR-019 Decision 3's prohibition on inferred identity remains active. Its use of `supplier_seller_affiliations` as the canonical identity link is superseded; the table becomes compatibility and migration evidence.
- ADR-019 Decision 5's owner-governed, atomic activation invariant remains active. Its exact creation of separate Seller membership and affiliation identity rows is compatibility behavior, superseded for the canonical target by Merchant membership and profile linkage.
- ADR-019 Decision 6 remains historical truth for already provisioned profiles, but separate Seller membership as the target authorization mechanism is superseded by unified Merchant membership and scoped permissions.
- ADR-019 Decisions 2, 4, 7, 8, 9, and 10 remain active.

No historical text is deleted from ADR-019; that ADR contains the explicit supersession marker.

## Consequences

### Positive

- One business identity and team can safely operate one or both capabilities.
- Existing store, offer, listing, order, inventory, and financial lineage remains stable.
- Legacy clients can migrate incrementally across independently deployed repositories.
- Ambiguous business identity becomes visible operational work instead of a silent cross-tenant merge.
- Authorization differences are measurable before Merchant becomes authoritative.

### Costs and constraints

- Compatibility adapters and legacy records must remain throughout backfill, cutover, rollback readiness, and observation.
- Owner conflicts require human resolution before canonical authorization for the affected Merchant.
- Phase 1 must add schema and persistence before any Merchant endpoint or console can be implemented.
- A later route-to-permission policy must cover every current Seller and Supplier action and prove parity through shadow comparison.

## Phase 0 implementation boundary

This ADR is an architecture decision only. Its adoption does not create a production migration, backfill command, runtime endpoint, handler, UI, event publisher, event consumer, or change to live Seller/Supplier behavior.
