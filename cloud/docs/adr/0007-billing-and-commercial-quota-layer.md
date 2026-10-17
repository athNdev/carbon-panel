# ADR 0007 — Billing and Commercial Quota Layer

- Status: accepted
- Date: 2026-09-24

## Context

Carbon Cloud provides multi-tenant control plane capabilities managing both bring-your-own (BYON) and cloud-provisioned compute nodes. A robust SaaS commercial model requires:
1. Tiered subscriptions (`free`, `pro`, `team`) with differentiated capacity allowances.
2. Strict quota enforcement at placement time so tenants cannot over-allocate physical or cloud resources beyond their plan limits.
3. Accurate usage metering for node-hours, workload-hours, and RAM/CPU allocations.
4. An invoicing engine that functions reliably in local/airgapped environments with zero external keys (ADR 0006) while seamlessly integrating with payment gateways (such as Stripe) in production.

## Decision

- **Domain Model & Tier Catalog**:
  - Defined in `cloud/internal/cloud/billing`.
  - Three standard plan tiers: `free` (1 node, 3 workloads, 4GB RAM), `pro` (10 nodes, 25 workloads, 64GB RAM), and `team` (unlimited nodes, 256GB RAM).
  - Subscriptions track status (`active`, `past_due`, `canceled`) and renewal dates.
- **Fail-Closed Quota Enforcement**:
  - `billing.QuotaEnforcer` validates usage against subscription limits *before* any node joins, managed node provisions, or workload allocations succeed.
  - Violations return explicit typed errors (`billing.ErrQuotaExceeded`) without leaking other tenants' resource states.
- **Zero-Credential Fallback**:
  - `InvoiceProvider` is defined as a Go interface (`CreateCustomer`, `IssueInvoice`, `CancelSubscription`).
  - When Stripe credentials are absent from `secrets.Provider`, `MockInvoiceProvider` handles lifecycle transitions in-memory with deterministic invoice numbers (`inv_mock_...`), allowing 100% test and CI coverage without network calls or secrets.
- **Usage Metering**:
  - Metering records capture consumption increments with timestamping and org scoping, allowing invoice generation at billing cycle rollover.

## Consequences

- Tenants cannot exceed provisioned capacity limits, protecting compute hosts from noisy-neighbor starvation.
- The control plane boots cleanly in airgapped, local development, and CI environments without requiring payment gateway API keys.
- Production deployments enable Stripe integration simply by populating the corresponding secret in the `secrets.Provider`.
