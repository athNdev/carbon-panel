# Kickoff prompt — Managed Minecraft Cloud service

You own implementation of Carbon Cloud's managed Minecraft service Epic. Work autonomously through every sufficiently specified, feasible part and leave one substantial, verified feature PR for supervising review. The user now authorizes implementation: this instruction supersedes earlier design-only statements in the planning documents. It does **not** turn proposed commercial policies or illustrative configuration into accepted decisions.

**Accepted product direction (C01, 2026-10-23):** build a fully managed Carbon Cloud platform with modular, general deployment configuration and the fixed Carbon product identity. Do not add an option to rebrand, substitute product names/logos, or enable white-label operation. Keep provider adapters, VM/fleet inventory, regions, resource/price configuration and payment integration settings portable across deployments. This clarification does not accept merchant ownership, marketplace funds flow or processor selection. Read the latest decision register and Plane comments before treating any policy as settled, and include C01 in relevant subagent briefs.

## Repository, Epic and inputs

**Accepted D02 (2026-10-23):** the first managed release must support both dedicated and shared hosting instances. Read `CONTEXT.md` for the working glossary and include relevant definitions in every subagent brief. Dedicated means an exclusive worker VM/node allocation; shared means separate enforced allocations that may belong to different customer organizations. Do not equate hosting instance, Minecraft server, workload, node, agent, allocation and VM. Current `db.Node` embeds org ownership: shared workers need an explicit operator-infrastructure/tenant-allocation model and scoped authorization, not removal of org checks. Amend affected contracts and ADRs with compatibility/migration coverage; existing BYON ownership remains explicit. D01 and D03–D14 remain pending until individually accepted.

- Repository: `C:\research_learning_new\carbon-cloud`.
- Plane workspace: `http://192.168.0.119/homelab/`; project `CarbonPanel`, identifier `MINE` (both repositories currently use this project).
- Epic: **MINE-231 — [Epic] Managed Minecraft Cloud: instance catalog, providers, payments & OSS parity**.
- Epic link: `http://192.168.0.119/homelab/browse/MINE-231/`.
- Expected feature branch: `codex/cloud-managed-minecraft`; PR base: `main`.
- Current children: **MINE-232–248**. Refresh their full descriptions, comments, criteria, states and native dependencies before starting. These children still have design-oriented titles: preserve their history, distinguish design evidence from runtime implementation, and add linked implementation checklists/children within this Epic where necessary. Check for duplicates first.

Read all applicable repository instructions, README, Makefile, CI workflows and accepted ADRs 0003–0009. Read these documents in full and inspect the referenced source:

1. `docs/plans/hosted-minecraft-instance-catalog.md` — primary architecture, work packages and unresolved D01–D14 decision register.
2. `docs/plans/hosted-instance-contracts.md` — proposed interfaces, illustrative configuration, state/sequence diagrams and A01–A28 fault scenarios.
3. `docs/plans/managed-minecraft-service-blueprint.md` — complete customer/operator journeys and managed-service scope.
4. `docs/plans/managed-minecraft-feature-parity.md` — 30 source-backed OSS parity rows P01–P30.
5. `docs/research/hosted-instance-catalog-and-payments.md`.
6. `docs/research/hosted-instance-catalog-reliability-addendum.md`.

Research and design inputs date from 2026-10-22; source observations were not passing tests, live failures or accepted public performance promises. Recheck the implementation, recent merged changes and current official provider/payment documentation before relying on them.

## Branch ownership and delegation

1. Inspect status, branch, diff, remotes and recent commits before editing. At preparation time the checkout was on `codex/cloud-reliability-hardening`, with pre-existing modifications to Compose/Docker configuration, billing files and `internal/cloud/node/service.go`, plus untracked Docker ignore files, a live-SaaS checklist and the six design/research documents. Preserve these changes; do not blanket-stage, reset, clean or overwrite other work.
2. Fetch and inspect `origin/main`; create or reuse this Epic's single feature branch in a suitable checkout or managed worktree. Deliberately carry relevant uncommitted planning documents into that branch if moving checkouts. Account for unmerged prerequisites explicitly rather than assuming everything in the current directory is already on main or belongs to you.
3. Keep all implementation for this Epic on this one branch and one PR, with incremental, attributable commits referencing Plane IDs. Stage explicit paths/hunks after review. No force-push to shared branches.
4. You are explicitly authorized to **use subagents** for bounded provider, billing, runtime, console, tests, research and independent review tasks. Assign separate files/modules and agree on contracts before parallel edits. You own integration, verification, scope and tracker honesty.
5. Own only Cloud. Another agent owns OSS MINE-249 in `C:\research_learning_new\carbonpanel`. You may inspect its source/contracts; record shared requirements as Plane dependencies. Do not edit or merge the OSS agent's branch.

## Subagent model routing and context handoff

You are explicitly authorized to select the **model and reasoning effort separately for each subagent** according to its task. Use the available subagent tools; this permission applies at supported reasoning levels including Medium and does not require Ultra. Respect the models, effort values, concurrency limits and permissions actually exposed by your client. Do not change global or user model settings to obtain a different subagent configuration.

| Delegated work | Preferred model and effort |
| --- | --- |
| Focused exploration, straightforward implementation, bounded regression coverage and routine CI diagnosis/fixes | `gpt-6-luna`, `medium` |
| A bounded implementation task with substantial edge cases or several interacting code paths | `gpt-6-luna`, `high` |
| Architecture/contracts, financial correctness, provisioning and crash recovery, capacity races, tenant isolation, data migrations, backup/restore safety, or independent review of these areas | `gpt-6.1-sol`, `high` |

Route a task to stronger reasoning when its assumptions, interactions or failure modes exceed its original bounded scope. Keep expensive assignments focused and avoid redundant exploration or uncontrolled nested delegation. If a preferred configuration is unavailable, record the actual configuration and resulting review/verification gap; do not present a different-model run as though the preferred review occurred. This routing does not authorize a subagent to accept unresolved commercial policies or weaken acceptance criteria.

**Give every subagent a self-contained task brief before spawning it.** In the toolset used to prepare this kickoff, a full-history fork inherits the parent's model/effort; a model/effort override requires a fresh-context spawn (`fork_turns="none"`). Inspect your actual tool contract and use its supported equivalent. For a fresh-context agent, assume it has none of the conversation, repository exploration, Plane decisions or test output you have seen. Even for an inherited-context agent, state its exact scope and current constraints rather than relying on earlier chat.

Include these fields in each brief, with concrete values and relevant excerpts instead of a transcript dump:

1. **Goal and acceptance:** the specific Epic/Plane item IDs and links, the complete acceptance criteria for this assignment, the expected behavior and explicit non-goals. Explain whether the agent is implementing, investigating, testing or independently reviewing.
2. **Working context:** absolute repository/worktree path, feature branch, relevant base/current SHA or diff, pre-existing changes to preserve, and exact files/modules it may edit. Identify shared interfaces, other agents' file ownership and dependencies. An independent reviewer needs the precise patch/commit range to review.
3. **Domain and evidence:** the relevant architecture/ADR/plan sections and source entry points, a concise summary of established facts, actual observed failures/reproduction steps and important excerpts. Distinguish implemented behavior, hypotheses, illustrative fixtures and unresolved D01–D14 policies. Provide file paths the agent can access; do not rely on links or files that exist only in your context.
4. **Invariants and authority:** applicable repository instructions; tenant/server authorization, durable operation/payment semantics, world/data preservation, independent OSS/Cloud release contracts and any task-specific constraints. Include the one-branch/one-PR rule, no merge/auto-merge/live deployment/live charges, and the requirement that ambiguous/partial/unverified acceptance remains Backlog. Do not assume the child knows the parent kickoff.
5. **Verification:** the relevant test commands and required caller/integration seam, disposable fixtures and environment/tool availability, browser or provider/payment checks where needed, and the exact evidence needed to establish success. Identify known failures and checks already run, including their results and SHA; do not count a parent result against a later child patch without rerunning affected checks.
6. **Deliverable and coordination:** request a concise result with changed files/diff, acceptance coverage, commands and outcomes, evidence references, remaining risks, ambiguities and blockers. State whether it may edit or is read-only, when you will wait for its result, and which independent work you will continue meanwhile. Keep credentials out of the brief; provide only approved secret references/access methods.

Keep child writes within assigned files and coordinate shared interfaces before edits. The Epic owner stages, integrates, commits/pushes, updates Plane states and owns the single PR; subagents return their changes/findings without independently merging, opening competing PRs or transitioning issues. Review each result against its requested scope, inspect the actual diff, run affected integration checks and reconcile contradictions before proceeding. After new decisions, interface changes or failed checks, refresh the relevant brief; do not let children continue against obsolete assumptions. A subagent's completion message is an input to your verification, not proof of Epic acceptance.

## Plane access and honest scope

Use a working Plane connector, or the direct API if the connector still requires reauthentication. Base: `http://192.168.0.119/api/v1/workspaces/homelab/projects/51da0c85-fe79-4427-9783-617dfcb2aaf1`; `/work-items/` uses `X-API-Key`. Obtain credentials from the configured connection or supplied secret/environment (`PLANE_API_KEY`); never write credentials to source, comments or logs. Honor pagination/rate limits and reconcile unknown create outcomes before retrying. If tracker access fails, continue independent implementation and preserve an explicit pending update list.

- If an item is **ambiguous, blocked, only partially implementable or cannot be fully verified**, explain honestly in a Plane comment and **keep/return it to Backlog**. Do not advance it because a skeleton, mock, design, UI tab or partial path exists.
- Comment structure: `Status: ambiguous / blocked / partial`; `Acceptance affected`; `Attempts and observations`; `Implemented subset, commit and tests`; `Exact missing policy/access/dependency`; `Proposed elaboration or reframing`; `Next verification`.
- Make routine technical decisions yourself and continue independent work. Do not use one unresolved commercial choice as a reason to stop the entire Epic. Build genuinely policy-independent seams or explicitly nonproduction development fixtures where useful; label fixtures and prevent their accidental production use.
- For verified implementation, link commits, evidence and PR, then use a review state if available or retain In Progress with a review-ready comment. Leave final Done/closure to the supervising review. An incomplete required child keeps the Epic open.
- The supervisor will review, elaborate/reframe and retry the Backlog items. Never silently weaken acceptance, overwrite unresolved decisions as accepted, or present partial implementation as a complete hosting service.

## Product outcome and work packages

Deliver a service where a customer signs in, chooses a Carbon profile and supported resource/location/runtime combination, sees an authoritative price, uses the configured payment flow, and receives a Minecraft address they can actually join. They must then be able to manage their server and recover/export its world through the practical OSS workflows. VM provisioning alone is not fulfillment.

| Items | Required work |
| --- | --- |
| MINE-232–234 | Reproducible workload benchmarks; dependent profile/resource configurator; versioned operator catalog/pricing, immutable quotes and atomic publication. |
| MINE-235–237 | Qualified provider capabilities and exact mappings; existing-VM inventory and separate Proxmox allocation; durable capacity reservations, provisioning and lifecycle recovery. |
| MINE-238–239 | Merchant/processor and billing-anchor decisions; payment adapter development; durable billing, entitlement, inbox/outbox, compensation and reconciliation. |
| MINE-240–242 | Tenant/RBAC/network/artifact/abuse isolation; worlds/backups/export/retention; installability, fleet operations, capacity economics, support and measured SLO evidence. |
| MINE-243–245 | Persistence/RPC migrations and independent-repository/BYON compatibility; honest validation/release/rollback gates; explicit unresolved product-policy register. |
| MINE-246–248 | Actual Minecraft runtime and joinable endpoints; tenant server workspace covering OSS parity; safe mods/plugins/modpacks/Studio/import/export and portability. |

Start with contract/schema/authorization and compatibility foundations, then deliver a vertical development path through catalog → quote → test payment → reserved capacity → enrolled agent → ready game → world backup/restore/export. Follow live dependencies and add fault coverage alongside each slice. Do not postpone all integration until after isolated modules are written.

## Architecture and commercial boundaries

- Keep **hosting instance, node, workload, NodeType and organization quota plan** distinct. Existing nodes can host multiple workloads. Stable server/world identity must survive container and node replacement. Preserve existing BYON and multi-workload behavior; do not attach retroactive managed-instance charges to old resources.
- Operator deployment inventory, provider credentials and capacity policy are separate from customer offers and prices. Operator configuration should support different hosters/providers and exact region/SKU mappings without workstation or cluster assumptions. Customers see only qualified, available locations and valid combinations.
- Candidate Starter/Community/Memory profiles and sample values are hypotheses, not benchmarks or approved tiers. Validate resource tuples jointly; provider SKUs may not support independent CPU/RAM sliders. Distinguish guest capacity, JVM/native overhead, CPU performance class, network cap, included transfer and reserved throughput. Hide unsupported dimensions; never silently change delivered resources or charge.
- Keep Terraform/OpenTofu behind provider adapters with real input forwarding, packaged module resolution, pinned compatible tools and protected state. Separate an **existing VM/fleet** inventory (one VM is a pool of size one) from a **Proxmox VM creator/allocator**. SSH bootstrap by itself is not a safe allocator or capacity reservation system. Qualify one real commercial-provider path plus existing-VM development behavior; do not claim universal provider parity.
- Model order, payment, infrastructure, game, service entitlement and data retention as separate state dimensions. Use durable logical operation IDs, atomic reservations, fencing/CAS, bounded retries, inbox/outbox and explicit compensation. Reconcile uncertain external outcomes rather than blindly repeating side effects; do not promise exactly-once external execution.
- Persist immutable server-priced quotes/contracts with configuration revision, currency/exponent and integer/fixed-decimal money rules. Never accept client-calculated amounts. Preserve accepted contract identity across retries and catalog updates.
- Stripe is the researched first-processor recommendation, not an accepted product decision. Use a narrow provider interface, an explicit offline Fake adapter and Stripe sandbox/test clocks where the decision permits. Missing production credentials must fail clearly, never silently select Fake. No live charges, payouts or paid cloud resources during this work.
- Verify webhook signatures against the raw body; bind events to merchant, environment and object; deduplicate and reconcile out-of-order/late events. A browser redirect never establishes payment or entitlement. Maintain Carbon's durable ledger beyond a processor's idempotency-key retention window.
- D01 and D03–D14 remain proposals unless live Plane comments record acceptance (D02 accepts both dedicated and shared modes): merchant versus marketplace; dedicated versus shared allocation; edition/runtime support; profiles; billing cadence; config governance; capacity holds/delivery compensation; bandwidth semantics; repricing/renewal; region failover; grace/backup/export/retention; jurisdiction/currency/tax eligibility; SLOs; and the payment-period anchor versus actual game readiness. Implement configurable mechanics where genuinely independent. Leave policy-dependent acceptance in Backlog with concrete options; do not silently choose public terms, retention deletion deadlines or financial commitments.
- ADR 0009 requires Cloud to build independently as `github.com/athNdev/carbon-cloud`. Do not import OSS `internal/*`, require a sibling checkout, or commit a local `replace`/`go.work` release dependency. Consume documented public OSS packages at explicit released versions. If a necessary OSS fix exists only in its unmerged PR, record the exact version/API blocker and continue independent Cloud work; do not bypass release compatibility.

## Runtime, safety, parity and UX

- Fulfillment requires authenticated node enrollment, actual workload creation, Minecraft readiness and externally reachable intended endpoints. Terraform `applied`, an API health response, a container running or a stale heartbeat is insufficient.
- Recheck known candidate gaps: incomplete module variable forwarding and checkout-relative lookup; fixed-SKU versus custom-resource mismatches; ignored disk/image/network inputs; backend/frontend plan-approval hash mismatch; sensitive bootstrap/tfvars/state exposure; enrollment readiness; and loopback versus public ingress.
- Keep worlds persistent and node-correct. Coordinate writers during backup/restore/resize/migration/update; verify data and recovery before retiring old resources. Respect accepted geography/residency and retention. No silent cross-region failover or automatic destructive cleanup of uncertain ownership.
- Carry tenant, organization and server authorization into every RPC, background job, artifact, stream and long-lived operation. Keep auth fail-closed. Do not expose provider secrets, raw HCL/state/private keys, arbitrary privileged bootstrap or host-level extensions to tenants.
- Preserve the practical OSS surface: overview/telemetry, console/logs, files/settings, players/access, runtime/content/modpacks/Studio, backups/restores/exports, tasks, proxy/networking, sleep/wake, modules where tenant-safe, activity/API/webhooks and granular permissions. Work through P01–P30; document concrete limitations and equivalent workflows rather than silently dropping features.
- Use server-centered customer navigation. Infrastructure/node/Terraform controls belong to operators or explicit advanced BYON flows. Explain price/downtime/data effects before mutation. Keep sign-out and organization switching available on membership/access errors; test recovery from expired, removed and stale organization state.
- Verify UI visually with real test accounts and browser interaction: configuration dependencies, checkout recovery, provisioning progress, errors, permissions, console/files/backup flows, small screens, keyboard/reduced-motion and stable scrolling/tab geometry. Do not bypass CAPTCHA or identity-provider protections; an unavailable authenticated scenario remains unverified.
- Keep credentials, development addresses, local state, worlds and build artifacts out of tracked source. Update affected configuration/schema, Compose/Docker/Helm/Terraform, API docs and operator runbooks together. Use disposable owned test infrastructure; do not mutate unrelated cluster workloads.

## Verification, CI and PR

You are authorized to add and run the tests needed for this Epic. Read current tool pins and CI. Current checks include:

- Go formatting, `go build ./...`, `go vet ./...`, `go test ./...` (`make cloud-test`); relevant race tests. CI currently races `./internal/cloud/reconcile/`, `./internal/cloud/notify/` and `./internal/cloud/svc/`; expand meaningful coverage for new concurrency.
- From `web/cloud-console`: `bun install --frozen-lockfile`, `bun run check`, `bun run test`, `bun run build`.
- `buf lint` and reproducible generation for changed protobuf contracts.
- Affected Terraform/OpenTofu formatting, initialization/validation and real module qualification; Helm lint/template and Compose configuration/build verification where changed. Offline examples do not establish provider apply/destroy correctness.

The current Makefile's `cloud-ci` can swallow Terraform validation failure, and some targets report SKIP when tools are missing. Check actual command results and prerequisites; a green wrapper message is not evidence. Also, `cloud-compose-down` currently invokes `down -v`: do not use it against persistent data. Address verification/data-preservation wrapper defects within the relevant release-reliability scope rather than relying on them.

Test real caller paths and migrations, duplicate/late/unknown payment outcomes, restart/crash recovery, capacity races, cross-tenant denials, readiness/freshness and named world restore/export contents. Use Fake/offline and sandbox fixtures with clear evidence limits. A fake payment or generic offline Terraform example cannot qualify a real processor/provider. Record external qualifications you cannot perform as Backlog gaps, not successes.

Open one PR early, optionally draft, linking MINE-231 and an item/acceptance checklist. Push incremental fixes, inspect CI logs and iterate until all required checks are green on the **latest PR SHA**. Do not weaken tests, suppress failures or misrepresent skipped checks. Investigate pre-existing failures and document real external blockers. Use independent subagent review for billing invariants, tenancy/auth, durable state/compensation, migrations and data safety; address actionable findings before handoff.

## Finish and handoff

Leave one pushed feature branch and one PR targeting `main`, ready for review for its implemented and verified scope, with remaining work explicitly recorded. **Do not merge, squash-merge, enable auto-merge, push directly to main, publish releases or update live deployments.** The supervising agent owns review, merge and rollout.

Report: PR URL, branch/exact SHA and CI links; implemented IDs and acceptance evidence; payment/provider/fault/visual/journey verification and its limits; remaining Backlog IDs with exact decisions/blockers; public OSS version dependencies; schema/config/data migration and rollback guidance; any pending Plane updates. Do not claim a production-ready managed service while policy, security, provider or data-recovery gates remain unproved. Begin repository/Epic inspection now and proceed into implementation, continuing all independent feasible work.
