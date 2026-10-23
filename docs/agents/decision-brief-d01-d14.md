# Decision brief — D01 and D03–D14

> **Outcome, 2026-10-23.** The four business decisions were answered by the
> product owner and are now recorded as accepted in the Cloud decision register
> (`docs/plans/hosted-minecraft-instance-catalog.md` §11) and in
> `docs/adr/0010-managed-hosting-commercial-policy.md`. D11 and D12 were
> delegated with concrete direction (industry standard; Australian entity with
> Stripe) and fixed to specific values there so they cannot drift.
>
> A new accepted direction, **D15**, was added the same day: storage-heavy
> instance profiles with thin-provisioned disk, so deep sleep and freeze can
> carry many servers on small RAM/vCPU while disk remains the scarce dimension.
>
> Still open: **D14** (billing anchor) has an implementation default but no
> product answer, and the eight engineering defaults below are not product
> acceptance.

Scope: these are the only open product decisions blocking MINE-231 beyond the
nonpayable development checkout. C01 (one fixed Carbon product, modular
deployment) and D02 (dedicated and shared hosting in the first release) are
already accepted and are not reopened here.

Each row states the proposed answer, what it gates, and whether it is an
engineering default you can wave through or a business choice only you can make.

| ID | Proposed answer | Gates | Kind |
| --- | --- | --- | --- |
| **D01** | One merchant per deployment, using that deployment's own payment account. No marketplace, no multi-party collection or payouts in v1. | MINE-238, MINE-239 | Business |
| **D03** | Java-first. Ship a named, tested runtime and version set. Modpacks only after they are qualified. No Bedrock promise until UDP/runtime validation exists. | MINE-232 | Engineering |
| **D04** | Three benchmark families, presented as discrete dependent choices rather than arbitrary sliders. | MINE-232–235 | Engineering |
| **D05** | Monthly upfront recurring price per instance. BYON quota plans stay a separate product. | MINE-234, 238, 239 | Business |
| **D06** | Versioned deployment configuration, validated and published atomically. No runtime admin editing in v1. | MINE-234, 235 | Engineering |
| **D07** | Short capacity hold during checkout, with an explicit delivery deadline and a defined compensation path. | MINE-237, 239, 242 | Business |
| **D08** | Sell a throughput cap only where it is actually enforced. Disclose transfer allowance separately. | MINE-233, 235, 240 | Engineering |
| **D09** | Preserve the price revision accepted for the current term; define renewal policy and notice explicitly. | MINE-234, 239 | Business |
| **D10** | Live data is location-pinned. No automatic cross-region failover in v1. | MINE-235, 241, 242 | Engineering |
| **D11** | Backup, export and retention durations, plus payment grace periods, chosen before launch. | MINE-239–242 | Business (needs numbers) |
| **D12** | First launch country, currency, operator jurisdiction and processor eligibility settled before live commerce. | MINE-238, 244 | Business (needs numbers) |
| **D13** | Measure create-to-ready, restore and game availability before publishing any SLO. | MINE-232, 242, 244 | Engineering |
| **D14** | Billing anchor: charge from checkout with compensation, or from actual ready time. | MINE-237, 238, 239 | Business |

## What actually requires your judgment

Four decisions cannot be defaulted, because they create financial or legal
obligations rather than technical ones:

1. **D01** — who holds customer money. Single merchant is by far the simplest and
   is what the current design assumes; a marketplace is a separate product.
2. **D05** — the pricing model. Monthly upfront is assumed everywhere today.
3. **D11** — retention and grace durations. The numbers in the planning docs are
   illustrative, not policy, and must not be shipped as customer promises.
4. **D12** — jurisdiction, currency and processor eligibility.

**D14** is a business choice but a small one; either answer is implementable.

The remaining eight (D03, D04, D06, D07, D08, D09, D10, D13) are engineering
defaults that match what is already built or planned. If you accept the table as
written, they can be recorded as accepted and unblocked immediately.

## Recommended order

1. Accept D01, D05, D14 — unblocks the order and payment schema.
2. Choose D11 and D12 — unblocks customer-facing terms.
3. Record D03, D04, D06, D08, D09, D10, D13 as engineering defaults.
4. D07 last, since it depends on D14 and delivery timing.

## What happens after acceptance

MINE-238 and MINE-239 move out of Backlog. Until then the code keeps the
nonpayable development checkout only: it can create orders, but no real money
moves and no entitlement is granted from a payment fact. That is the current,
deliberate state — not a gap.
