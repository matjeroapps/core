# ADR-021: Merchant Integration Ownership and Unified Hub Boundary

## Status

Accepted as the Integration Track I0 architecture baseline. Runtime migration,
repository rename, repository archive, and deletion are not authorized by this
ADR.

Partially supersedes
[ADR-009: Integration Deployment Model](ADR-009-integration-deployment-model.md)
only where ADR-009 mandates separate Seller and Supplier integration
repositories/applications. ADR-009's independent deployment, scaling, release
safety, observability, recovery, and failure-isolation requirements remain
active through independently deployed workers.

## Context

Core currently persists integration connections using `actor_type` and
`actor_id`, with separate Supplier catalog and Seller channel job concepts. The
workspace also contains a README-only `seller-hub` and a `supplier-hub` Go
foundation whose provider fetch paths, persistence, CLI, and execution are
demonstration-oriented rather than connected production workers.

ADR-020 makes Merchant the canonical business and authorization tenant. The
approved Merchant Integration Hub design requires provider infrastructure to be
shared without combining Supply and Retail authority or failure domains. This
decision establishes the target boundary before additive schema or runtime work
begins.

## Decisions

### 1. Merchant owns every integration connection

- An integration connection belongs to exactly one Merchant.
- Seller and Supplier are not integration ownership types.
- A Merchant may own multiple external connections from the same or different
  providers.
- The same verified external account cannot be connected more than once to the
  same Merchant, including under a different connection type.
- Provider-specific global installation exclusivity is enforced when verified
  provider evidence says the installation cannot belong to multiple Merchants.

### 2. External identity is verified provider evidence

`external_account_id` comes from a successful OAuth exchange or authenticated
provider API response. A display label, domain, email, company name, provider
name, actor ID, or settings value is never sufficient identity evidence.

Connection creation remains non-active until the provider identity and granted
scopes are verified. Unverifiable current connections are quarantined during
migration; they are not guessed or silently activated.

### 3. Every connection has exactly one commercial type

The only connection types are:

- `SUPPLY_SOURCE`
- `RETAIL_CHANNEL`

Type becomes immutable after the connection's first successful
synchronization. A change of purpose requires pausing or retiring the old
connection, resolving/draining its work, reviewing mappings, and creating or
reauthorizing a valid replacement. Connecting the same external account again
cannot bypass this rule.

### 4. Connection binding follows type

A `RETAIL_CHANNEL` is bound to exactly one Store belonging to the same Merchant.
One Store may have multiple Retail Channel connections. One connection may not
publish for or import orders into multiple Stores.

A `SUPPLY_SOURCE` targets the Merchant's Supply capability. Its external
locations map explicitly to Core fulfillment locations. It has no Retail Store
binding.

### 5. Synchronization direction is explicit

Generic bidirectional synchronization is prohibited.

For `SUPPLY_SOURCE`:

- catalog enters MatjerHub Staging from the provider;
- wholesale price and supplier inventory flow from provider to MatjerHub;
- supplier fulfillment requests flow from MatjerHub to provider;
- fulfillment and tracking state flow from provider to MatjerHub;
- consumer Retail orders are never imported;
- Retail prices are never published.

For `RETAIL_CHANNEL`:

- Retail Listings, selling prices, and sellable availability flow from
  MatjerHub to provider;
- customer orders and cancellations flow from provider to MatjerHub;
- fulfillment and tracking flow from MatjerHub to provider;
- refund status flows from provider to MatjerHub reconciliation;
- provider products are never imported as Supplier Offers.

Every attempt uses origin identity, provider event identity, entity identity,
external version or content digest, direction, idempotency key, correlation ID,
and causation ID. Origin changes are not echoed. Duplicates do not duplicate
commerce or financial state. Authority/version conflicts enter review instead
of overwriting newer facts.

### 6. Core remains authoritative

Core owns:

- connection ownership, type, lifecycle, binding, scopes, health, and durable
  records;
- external entity mappings, synchronization job intent, cursors, inbox records,
  review cases, and migration crosswalks;
- catalog/offer/listing/inventory mutations;
- wallet and inventory reservations;
- customer orders, supplier fulfillment, refunds/reversals, ledger, and all
  financial truth.

Externally relevant Core mutations continue to use the transactional outbox.
Consumers assume at-least-once delivery and use durable inbox/idempotency.

### 7. Integration Hub owns provider execution

Integration Hub owns:

- OAuth/provider protocol implementation and external account verification;
- external requests, pagination, provider rate limits, and transport telemetry;
- inbound webhook signature verification and provider event parsing;
- bounded retry and dead-letter execution;
- Supply and Retail orchestration against versioned Core capabilities;
- reconciliation execution and provider-side comparison evidence.

Integration Hub must not access Core PostgreSQL directly, import Core private
packages, or accept database-operation messages. Synchronous capability calls
use versioned HTTP/JSON. Asynchronous commands/events use RabbitMQ under
ADR-018. Cleartext credentials are resolved through a supported vault and are
never stored in Core settings, messages, logs, audit output, or frontend
responses.

### 8. Shared adapters do not combine pipelines

Provider authentication, transport, webhook verification, pagination,
rate-limit, and protocol helpers are shared. Supply, Retail, webhook, and
reconciliation workers remain independently deployable, scalable, observable,
recoverable, and failure-isolated. One repository never implies one process or
one queue.

### 9. Supply imports require review before automation

First imports always enter Staging. Product, variant, SKU, barcode, price,
inventory, currency, and location facts are validated and mapped before
publication. Matching SKU/barcode across Supply Sources creates a duplicate
review case; products are not merged and inventory is not summed automatically.

The Merchant selects the canonical product mapping and authoritative source for
overlapping facts. Later approved price/inventory updates may run
automatically. Structural changes invalidate the approved mapping and return
the record to review.

### 10. External Retail orders fail closed before routing

An external order proceeds only through signed webhook verification, inbox
deduplication, connection/Store resolution, order/Listing mapping, price,
address, shipping, inventory reservation, Merchant wallet coverage/reservation,
Core order/fulfillment creation, and supplier routing—in that order.

Any failed business validation creates a stable Integration Review case and
does not send fulfillment work to a supplier.

External proceeds are collected outside MatjerHub and are not represented as
platform-held cash. Before routing, the Merchant wallet covers supplier
wholesale entitlement, applicable MatjerHub commission, Store-funded shipping
subsidy, and every other approved internal obligation. No automatic credit is
granted; external COD follows the same full-coverage rule.

Provider/payment fees are Merchant expenses and reduce neither supplier
entitlement nor MatjerHub commission. They may be imported for profitability
reporting. Refunds occur externally and trigger reconciliation. Supplier
entitlement reverses only through approved cancellation, return, fault, or
dispute rules; partial refunds reverse only corresponding internal allocations.

### 11. Migration is additive, deterministic, and reversible

Future migration must preserve current IDs through a deterministic crosswalk,
reconcile the current connection/job identifier incompatibility, and migrate or
quarantine connections, mappings, cursors, webhook inbox records, and both job
families. Actor ownership moves to Merchant only through canonical profile
linkage. External identity is backfilled only from verified provider evidence.

Dry run precedes apply. Audit manifests, mutually exclusive counts, bounded
metrics, retry state, reconciliation queries, and pre/post-cutover rollback are
required. Duplicate/ambiguous external accounts and unsupported/incomplete
credentials are quarantined.

### 12. Repository transition and retirement are future gated work

The history-preserving target is to rename `supplier-hub` to
`integration-hub`. The future runbook must preserve Git history, remote
redirects where supported, Go module/import compatibility, and consumer
transition. Supply-specific code remains in the Supply pipeline while shared
provider infrastructure is generalized.

`seller-hub` is archived only after Retail parity, every consumer migration,
legacy-write stop, rollback proof, zero required legacy traffic, and at least 14
consecutive healthy observation days. Obsolete code/tables/routes/deployments/
tests are removed only through separately approved cleanup.

I0 authorizes no repository rename, archive, deletion, production migration,
runtime handler, worker, client, publisher, consumer, or infrastructure change.

## Consequences

### Positive

- Connection identity and authority align with canonical Merchant ownership.
- Shared provider work no longer forces duplicate repositories or protocols.
- Supply and Retail directions, order funding, and review failures are explicit.
- Independent processes preserve operational isolation inside a unified history.
- Migration and retirement require measurable evidence rather than architecture
  intent alone.

### Costs and constraints

- Current actor-owned connections and clients require compatibility behavior.
- Existing identifier and table-name defects must be resolved before job data
  can move safely.
- Current provider packages require production protocol/persistence/broker/vault
  work and provider verification before reuse.
- Legacy repositories, routes, records, and tests remain until later gates pass.

## Related decisions and authority

- [ADR-017](ADR-017-repository-independence-and-runtime-service-boundaries.md)
- [ADR-018](ADR-018-rabbitmq-asynchronous-messaging-backbone.md)
- [ADR-020](ADR-020-canonical-merchant-identity-and-capabilities.md)
- Workspace master plan:
  `docs/plans/2026-10-02-unified-merchant-capabilities-master-plan.md`
- Approved Integration Hub design:
  `docs/plans/2026-10-02-merchant-integration-hub-design.md`
