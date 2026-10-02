# ADR-009: Integration Deployment Model

## Status

Accepted

Partially superseded by
[ADR-021: Merchant Integration Ownership and Unified Hub Boundary](ADR-021-merchant-integration-ownership-and-hub-boundary.md).

The historical decision below is retained unchanged. ADR-021 supersedes only
the requirement that Seller and Supplier integrations remain separate
repositories or applications. Independent deployment, scaling, release safety,
observability, recovery, and failure isolation remain active requirements. They
move to separately deployable Supply, Retail, webhook, and reconciliation
workers inside the unified Integration Hub. Core authority and the repository
independence/runtime boundary in ADR-017 remain unchanged.

## Decision

Supplier and seller integrations are independently deployable applications, even when they target the same provider. Shared libraries may contain generic infrastructure and small provider utilities, but synchronization workflows remain separate.

## Consequences

There is no mandatory central Salla, Shopify, or WooCommerce runtime service. Release coupling between supplier and seller integration workflows is avoided.
