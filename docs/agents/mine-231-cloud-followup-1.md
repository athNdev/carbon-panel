# Follow-up — reconcile and continue the managed Cloud Epic

Resume your existing Cloud Epic work. Missing tracker access does not invalidate all prior implementation; retain valid work, compare it with the live specification and finish every feasible gap. This is a continuation, not a fresh project or a second feature branch.

## Preserve the current work

- Actual working checkout: **`C:\research_learning_new\carbon-cloud-managed`**.
- Continue **`codex/cloud-managed-minecraft`**, **[PR #29](https://github.com/athNdev/carbon-cloud/pull/29)** targeting main.
- Pushed head observed: `dd448cc9547f3260c61f09a564867e5d16e30642`, base `d1aa2412410ebe57d095da604a880a43cfd29571`. Backend, console and proto CI were green for that pushed head.
- **21 files had additional uncommitted changes** in agent runtime/files/backup/locks, services, commerce/catalog and generated protobufs. Preserve and review them; the pushed head's green CI does not validate that dirty tree.
- The original `C:\research_learning_new\carbon-cloud` checkout has unrelated pre-existing edits **and newer policy/glossary documents not present in your working checkout**. Carry the relevant documentation deliberately; do not reset/clean, blanket-stage or overwrite that checkout.
- The supervisor inspected current status and scope documents, not a full Cloud PR correctness review. Do not interpret this prompt as acceptance of the code.

## Plane access is repaired — use this connection

Use the native **plane_homelab** MCP tools for the self-hosted workspace **homelab**, project **CarbonPanel / MINE**, project UUID `51da0c85-fe79-4427-9783-617dfcb2aaf1`. The server is already registered for this Windows user, supplies its own credential and workspace, and has passed actual MCP reads, comments, issue updates, state changes and directional dependency creation. Temporary verification records were removed.

The hosted `mcp__codex_apps__plane` connector is a separate OAuth connection. Its reauthentication error is not a failure of plane_homelab. Do not spend more turns retrying that connector or ask me to reauthenticate it. Start with `workitem(action="retrieve_by_identifier", workitem_identifier="MINE-…")` through plane_homelab, then retrieve the full child descriptions, comments, states and relations. Follow pagination; do not rely on names or the old kickoff alone. Resolve state/issue UUIDs before writes, read back changes, and check existing relations before adding them.

The local relation adapter supports **list and create only**, preserving blocked_by/blocking direction. Relation removal and custom-relation definitions are not exposed by this instance's public API; record an incorrect link for supervisor correction instead of probing unsupported endpoints or editing the database. Unsupported features, HTTP429, actual local401 and network failures must be reported precisely. Honor Retry-After and check whether an uncertain mutation committed before retrying.

If the native tools have not loaded in your existing chat, refresh tool discovery/connection before declaring Plane unavailable. Registration can be inspected with `codex mcp get plane_homelab`; that alone does not establish live access. Local connection/runbook: `C:\Users\Daybreak8458\Documents\Codex\mcp-servers\plane-homelab\README.md`. Keep credentials and this machine's connection configuration outside either repository. Do not print the token or copy it into source, logs, PR text or subagent prompts.

## Re-read all live requirements and reconcile coverage

Read **MINE-231** (UUID `54f937c6-b5b7-49e3-a3b2-035bf6823555`), **MINE-232–248**, implementation child **MINE-272**, every comment and native dependency. There were **18 children** when refreshed. A local snapshot is `C:\Users\Daybreak8458\Documents\Codex\2026-10-23\carbon-epic-kickoffs\cloud-plane-snapshot.md`; it predates the latest accepted-terminology comment, so read live **MINE-245** comments.

MINE-272 and your implementation checkpoint already show some direct Plane access. Verify actual criterion coverage rather than assuming you never had access. MINE-272 is a legitimate bounded implementation record; it **does not replace or narrow MINE-231 or MINE-232–248**. If the phrase “authorized implementation subset” suggests the kickoff authorized only that slice, correct it: the kickoff authorized all sufficiently specified feasible Epic work, while retaining genuine policy and qualification gates.

Read the six original plan/research documents plus `docs/plans/managed-minecraft-implementation.md`, current ADRs, AGENTS.md/CI, and the latest accepted glossary. Create an acceptance-by-acceptance matrix linking original requirement, implementation, exact evidence, gap/dependency and status. Design, unit-tested mechanics, offline simulation, provider qualification, authenticated visual journeys and actual Minecraft delivery are different evidence levels. Backfill live comments and dependencies; do not declare broad design criteria complete because the development subset passes.

## Accepted decisions you must incorporate now

**C01 — accepted:** Carbon Cloud is a fully managed Carbon product with modular, general deployment configuration. **No rebranding, white-label mode, or replacement product name/logo settings.** Provider adapters, VM/fleet inventory, regions, resource/price and payment integration configuration remain operator-configurable. C01 does not decide who owns merchant funds.

**D02 — accepted:** the first managed release supports **both dedicated and shared hosting**. Dedicated-first/shared-later is an obsolete recommendation, not current accepted scope. Shared nodes may serve different customer organizations using enforced, tenant-scoped allocations. Preserve existing BYON/multi-workload contracts.

**Core terminology — explicitly accepted by the user and now recorded on MINE-245:**
- **Carbon instance / hosting instance:** stable purchased hosting service/resource contract.
- **Minecraft server:** its game, world, configuration, player access and connection identity.
- **Worker node:** registered execution host.
- **Worker VM:** virtual machine backing that host when virtualized.
- **Dedicated/shared:** allocation mode. Stable service identity survives VM/container replacement or migration. Dedicated VM does not imply dedicated physical host/core.

Use the other glossary entries consistently: node agent is software, workload is an execution unit, allocation is an exclusive worker claim or bounded resource share, fleet/pool describe operator execution capacity, and background job worker differs from worker node.

**D01 and D03–D14 remain pending.** Do not treat merchant/funds flow, processor, monthly billing, public prices/profiles, edition qualification, bandwidth terms, compensation, repricing, region failover, retention/deletion, tax/jurisdiction or SLO examples as accepted policy. No actual charge, paid provisioning or public contractual promises. Develop mechanics that are independent of these choices; isolate genuine decision-dependent acceptance with exact questions/options.

Your managed checkout currently lacks `CONTEXT.md`; its catalog still recommends dedicated-only initially, and its implementation checkpoint incorrectly says all D01–D14 are proposals. Reconcile the original checkout's newer glossary/catalog/contracts/blueprint, the live comments and your implementation documentation. Preserve research history while clearly marking superseded recommendations. Correct the same drift in MINE-272's wording/comments and PR. Include accepted decisions in every subagent brief.

## Work to continue beyond the development checkout

Do not stop after nonpayable catalog/order fixtures. Work through all feasible original children:
- **232–234:** benchmark-backed workload suitability (no invented player guarantees), versioned configurable profiles, dependent RAM/vCPU/network ranges, exact provider feasibility, geography, server-priced integer-money quotes, immutable accepted price/terms and safe config publication. Distinguish vCPU count from CPU quality and bandwidth caps from included transfer/guaranteed throughput.
- **235–237:** provider capability/mapping seam, existing VM/fleet inventory and separately qualified provisioning adapters, finite capacity, region resolution, resource enforcement and durable provisioning/reconciliation with reservations, fencing and compensation. Terraform/OpenTofu is an adapter mechanism, not a universal feature promise.
- **238–239:** narrow payment ports plus development fake and test-only processor boundaries; authenticated raw-body verification, account/environment/object binding, durable deduplication/ledger, out-of-order/late/unknown events and entitlement rules. A browser redirect, fake payment or VM creation cannot establish entitlement/game readiness. Processor choice still needs policy/research acceptance and actual sandbox qualification.
- **240/243:** explicit operator infrastructure versus tenant allocation ownership for cross-org shared workers. Existing `db.Node` embeds TenantBase/OrgID and ADR0003 assumes org-owned hosts. Design/migrate this deliberately; **do not remove organization checks to enable sharing**. Cover RPCs, queues, agent commands, streams, backups/artifacts and cross-tenant denials; retain BYON compatibility and least privilege.
- **241/246:** persistent node-correct worlds, coordinated writers, checksum/staging/atomic recovery, migrations/export/restore with named contents and interrupted recovery; authenticated enrollment, real Minecraft readiness and the intended external player endpoint. Preserve service identity across replacement.
- **242/244/247/248:** truthful readiness/freshness and operations, release/migration/rollback, server-centered workspace, **P01–P30 OSS parity**, modpacks/content/Studio portability, and real customer/operator journeys. Infrastructure/HCL/secrets belong to operators. Keep sign-out/org-switch recovery reachable on access failure, stable scroll/tab geometry, keyboard/reduced-motion and actionable progress/errors.

ADR0009 requires Cloud to build independently as `github.com/athNdev/carbon-cloud`. Your checkpoint records released OSS **v1.0.38**; verify actual pins and compatibility. No sibling checkout dependency, OSS internal imports, local replace/go.work release dependency or assumptions about unmerged OSS PR225. Record exact API/version blockers and continue independent work.

Complete/review the existing uncommitted safety patches before claiming a new checkpoint. Add meaningful regression, PostgreSQL/concurrency/migration, real caller and recovery evidence. Required wrappers must propagate failures and routine Compose shutdown must preserve persistent volumes. Keep uncertain provider outcomes recoverable and saved-plan approval bound to the exact plan.

## Delegation, verification and handoff

You may use bounded subagents. Keep your current chosen owner model/reasoning setting. For ordinary work explicitly request **Luna Medium** when supported; raise Luna reasoning for difficult subproblems. Do not assume a default full-history fork changes the model. Inspect actual reported settings. Give every subagent the exact checkout/commit, live issue criteria, accepted decisions/glossary, required contracts, owned files, evidence expectations, neighboring work and integration boundary. Use a stronger model only for a clearly justified bounded review.

You are authorized to add/run necessary tests and visually inspect using test accounts. Run affected Go build/vet/tests/races, PostgreSQL integration, console check/tests/build, reproducible proto generation, relevant Terraform/Helm/Compose/release qualification and authenticated browser journeys. Use disposable owned infrastructure; do not alter unrelated deployments or user worlds. Never bypass CAPTCHA or identity-provider protections. Offline plans, mocked agents, synthetic slots and a local Stripe signature test are not real provider/payment/gameplay qualification. Record unperformed checks precisely and keep their acceptance Backlog.

For every incomplete, ambiguous or externally blocked criterion, comment with implemented code, evidence, missing requirement, exact blocker/decision and next action. Difficulty alone is not ambiguity. Keep partial/ambiguous/unverified items Backlog, preserve broader acceptance and keep the Epic open. Continue independent work instead of using pending commercial decisions to defer unrelated reliability, parity, authorization or UI work.

Keep one feature branch/PR, stage explicit paths and commit/push incrementally. Iterate CI until all required checks are green on the **latest pushed SHA**, including your resumed fixes; do not weaken or omit checks. Update Plane/PR/matrix together. Handoff PR/head/CI links, implemented and verified IDs, remaining decisions/qualification gaps, migrations/rollback, world safety, public OSS version dependencies and visual/journey evidence. **Do not merge, enable auto-merge, push main, release, charge or deploy.** Leave the result ready for supervising review.

