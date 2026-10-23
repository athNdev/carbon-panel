# Cloud agent follow-up 2 — implement real fleet allocation and fulfillment

Resume **MINE-231** on your existing feature branch. The supervisor verified backend/race, console and proto CI at `a85136bb8f21c026d331a377694490533156697d`. Your reconciliation and synthetic shared allocation tests are useful. The scoped review found no confirmed defect in the inspected runtime paths, but this is not full security or production acceptance.

The next handoff must advance the actual managed service. Synthetic slot accounting, a development checkout and accurate documentation do not fulfill accepted shared hosting or Minecraft delivery. Do not restart; retain your tested foundations and connect them to real operator infrastructure.

## Checkout and specification

- Working checkout: **`C:\research_learning_new\carbon-cloud-managed`**.
- Branch: **codex/cloud-managed-minecraft**; draft **[PR #29](https://github.com/athNdev/carbon-cloud/pull/29)**, base main.
- Reviewed base: `d1aa2412410ebe57d095da604a880a43cfd29571`; reviewed head: `a85136bb8f21c026d331a377694490533156697d`. It was clean; refresh before editing.
- Preserve the separate original `C:\research_learning_new\carbon-cloud` checkout and unrelated work.
- Epic MINE-231 UUID `54f937c6-b5b7-49e3-a3b2-035bf6823555`; original children MINE-232–248 plus implementation child MINE-272. Refresh live children and comments.
- Read the criterion-level matrix, implementation checkpoint, six design/research documents, CONTEXT.md, current ADRs, migrations, CI and operator runbooks. Preserve original broad acceptance. MINE-272 remains a bounded implementation record, not a replacement for MINE-231.

## Working rules for this continuation

- Continue the existing feature branch and PR. Inspect status, worktrees, remotes and the latest remote head first. Preserve unrelated edits and user data. Stage explicit paths/hunks and make incremental attributable commits. Do not create a replacement Epic branch or PR.
- Use **plane_homelab** native MCP tools for workspace **homelab**, project **CarbonPanel / MINE**, project UUID `51da0c85-fe79-4427-9783-617dfcb2aaf1`. Refresh descriptions, comments, acceptance, states and native dependencies; follow pagination and read back writes. The separate hosted Plane OAuth connector is not this connection. Do not retry its reauthentication flow as a repair.
- The local MCP connection is already configured with its credential; never print or copy the credential into source, prompts or PR text. Local troubleshooting instructions are at `C:\Users\Daybreak8458\Documents\Codex\mcp-servers\plane-homelab\README.md`. Check actual native tool access before reporting an outage; configuration presence alone is not live verification.
- Relations support list/create, not removal. On the last live read **MINE-269 and MINE-271 had only a valid relates_to link, with no blocking edge between them**. Re-read before changing anything; do not delete that link or keep reporting an unverified dependency conflict.
- You are explicitly authorized to use bounded subagents. Keep the owner's chosen model/reasoning setting. For ordinary subagent work, explicitly select **GPT 6 Luna / Medium** when supported; use higher Luna reasoning for harder slices. With the current collaboration API, explicit model/reasoning overrides require `fork_turns="none"`; a full-history fork inherits the parent's settings and cannot be combined with those overrides. Confirm the actual available model identifiers/schema and returned settings. Do not silently launch expensive inherited-model copies.
- Give each subagent a self-contained brief: exact repository/worktree and pinned base/head; issue acceptance; accepted decisions and terminology; source paths and relevant neighboring behavior; owned files; interface contracts and integration order; test/evidence expectations; safety constraints and a concise handoff. Prefer separate files or read-only reviews to overlapping edits. The owner integrates and reviews every result.
- You are authorized to add/run the regression, integration, fault and visual checks necessary for this work. Iterate narrowly, then run affected full checks. Use disposable resources with explicit ownership and cleanup manifests. Preserve worlds, databases, unrelated containers and existing deployments.
- Technical architecture and implementation decisions within accepted scope are your responsibility: choose, document and test them. Do not relabel ordinary engineering work as a missing product-policy decision. For a real blocker, record the exact failing command/capability or unresolved decision, affected criterion and next action, then continue independent work.
- Keep evidence levels separate: implementation present, regression-tested, real runtime qualified, visual/journey verified, and unverified. Keep partial/ambiguous/unable-to-complete acceptance in Backlog with a substantive comment; do not mark Done merely because CI is green. Use the actual workflow for active implementation and keep the parent Epic open. Preserve broader criteria and history.
- Push all resumed changes to the same PR and iterate until required CI is green on the **latest pushed SHA**. Do not weaken checks or report skips as passes. Keep Plane, PR and acceptance matrix consistent.
- **Do not merge, enable auto-merge, push directly to main, publish a release, deploy to live instances, charge money, or create paid provider resources.** Supervising review owns those actions.

## Accepted scope and policies

**C01:** fully managed Carbon Cloud with the fixed Carbon identity; no white-label/rebranding controls. Operator configuration of providers, inventory, region/resource mappings, prices and payment integrations stays modular and portable.

**D02:** **dedicated AND shared hosting in the first managed release**. Shared allocations can belong to different customer organizations. This is accepted scope, not a pending decision or later-release option.

Accepted core vocabulary:
- Carbon/hosting instance = stable purchased service/resource contract.
- Minecraft server = game/world/configuration/access/connection identity.
- Worker node = registered execution host; worker VM = the VM backing it when virtualized.
- Dedicated/shared describes the allocation; identity survives VM/container replacement. A dedicated VM does not imply dedicated physical CPU/host.
- Keep node agent, workload, allocation, fleet/pool, control plane and background job worker distinct.

**D01 and D03–D14 remain pending** unless a newer explicit owner decision is found in Plane. Do not select merchant/funds flow, public price/profile promises, payment cadence, retention/deletion deadlines, tax markets, compensation, bandwidth promises or SLOs by implication. These do not block implementing operator fleet ownership, tenant allocations, durable execution, resource enforcement or nonpayable real-runtime qualification. Choose and document engineering details under the accepted direction.

## 1. Patch the affected dependency and make the check repeatable

Cloud resolves **github.com/klauspost/compress v1.18.0** through the released OSS dependency graph. The advisory lists **v1.18.7** as patched:
https://github.com/klauspost/compress/security/advisories/GHSA-259r-337f-4rfw

Upgrade to v1.18.7 or a newer compatible patched version after checking upstream metadata. Keep the override as a normal module requirement if needed; no local replace or sibling checkout dependency, no blanket package upgrade. Verify the selected module graph and checksums and run relevant regressions. On the supervisor's dependency inspection neither current Cloud binary imported compress; that is limited reachability evidence, not permission to ignore an affected resolved module or a claim about every build mode.

Check for an existing Plane advisory item and avoid duplication. Record the exact selected version and reachability scope. Add an appropriate pinned, failure-propagating vulnerability check to Cloud CI if absent; distinguish module alerts from callable vulnerable symbols and document any legitimate exclusions. Do not suppress the known advisory to obtain green CI.

## 2. Implement operator fleet ownership and real dedicated/shared allocation

Prioritize **MINE-235/236/240/243**, implementing the accepted D02 consequences.

Today db.Node and agent queries are organization-owned; the shared fixture only reserves fixture=true rows. Introduce an explicit model for **operator-owned execution infrastructure and tenant-owned allocations/workloads**, preserving existing BYON ownership and agent transport. Choose the appropriate table/type boundaries and document the ADR and migration trade-off.

Required behavior:
- Existing operator-managed VM inventory can register authenticated execution nodes with finite capacity, location, capabilities, ownership/generation and supported resource controls. Never automatically adopt arbitrary tenant nodes or unrelated VMs.
- A dedicated allocation exclusively claims its eligible worker node; shared allocations reserve bounded vectors on eligible shared nodes. Dedicated and shared claims must not overlap incompatibly. Validate compatibility of actual mappings, geography and enforceable dimensions.
- Claims and releases are transactional and durable under concurrent requests, retries, expiration, cancellation, drain, node loss and stale generations. A timed-out command is not evidence that resources are absent or safe to reuse.
- Route each customer's operations through allocation/server identity to the permitted agent/workload. Normal managed customers must not choose arbitrary provider/node IDs.
- Add tenant/org/resource authorization at RPCs, background jobs, dispatch, acknowledgments, streams, file/backup artifacts and long-lived commands. Do not remove org filters as a shortcut. Test cross-tenant forgery, stale mappings and revoked allocations.
- Preserve existing tenant-owned BYON nodes/workloads and multiplicity through migration and rollback/recovery. Test old-data upgrades and ownership invariants. Avoid destructive down migrations or automatic reparenting of existing resources.
- Keep synthetic fixtures explicitly separate. Passing synthetic contention tests must never publish real capacity.

Use a disposable real worker to demonstrate allocations for at least two customer organizations, and a separate dedicated case. The required milestone is real allocation-to-agent execution with isolation, not a new schema and fake slot tests alone.

## 3. Wire durable managed fulfillment to a real Minecraft workload

Prioritize **MINE-237/246** and the relevant runtime/data prerequisites. Build a durable reconciler/worker connecting accepted development order → reservation → node selection/adoption → authenticated enrollment/readiness → workload creation → game readiness → intended player endpoint.

Maintain separate payment, entitlement, infrastructure, game and data state machines. A nonpayable development fulfillment path may execute a test workload only through an explicitly enabled nonproduction authorization policy; fake payment alone must never activate production entitlement or capacity. Keep production development-mode rejection intact.

Persist operation identity, stages, command/inbox/outbox records, deadlines, generation fencing, retry/unknown outcomes and repair/compensation ownership. Browser closure and process restart must not interrupt durable progress. Query/reconcile uncertain effects before creating another VM, allocation, container or port claim. Exercise duplicate requests, late acknowledgments, crash-after-dispatch, restart, cancellation, node loss and re-enrollment.

For this milestone, use operator-managed existing disposable VM inventory to prove actual delivery without paid provider creation or unsettled merchant policy. A configurable adapter/fake can exercise other provisioning faults; qualify its real counterpart separately. Do not call an offline Terraform plan provider lifecycle qualification.

## 4. Enforce allocations and qualify the customer journey

Carry the exact accepted resource vector to the node agent/runtime. Enforce memory/CPU, storage and supported networking controls with measured effective configuration and contention tests; reject or hide dimensions a backend cannot enforce. A configured number or database reservation is not enforcement. Account for supporting workloads and node overhead.

Demonstrate two organizations on one disposable shared worker without access to each other's commands, processes, files, archives or capacity; also demonstrate exclusive dedicated allocation. Verify networking and management endpoints, including protection of RCON, agent and provider secrets.

Complete an authenticated visual journey with test accounts: configure geography/tuple/mode → create/recover a nonpayable order → provisioning progress/errors → actually delivered server workspace → console/files → backup/restore. Verify denied/stale-org recovery, sign-out, small-screen/keyboard/reduced-motion behavior and stable scrolling/tab layout. Observe the rendered UI; component tests and screenshots of a mock do not establish the real journey. Do not bypass CAPTCHA/identity-provider safeguards.

Prove actual Minecraft client join, named world modification, checksum-backed backup, stopped coordinated restore and rejoin with expected contents. Test replacement/failure recovery. Off-node backup loss/recovery and broad P01–P30 parity remain separate open acceptance until actually implemented and qualified.

## Independence and progress tracking

Keep ADR0009: independent `github.com/athNdev/carbon-cloud`, released public OSS packages only. The previous checkpoint used OSS v1.0.38; verify pins and compatibility. Do not consume unmerged OSS PR225 through internal imports, local replace/go.work or a sibling checkout. If a necessary public API is missing, specify the exact contract/version blocker and continue independent parts.

Split the above into reviewable technical checklists/implementation children under the existing Epic where useful, after checking duplicates. Link original criteria and real dependencies. Do not rewrite broad design acceptance to match smaller code. Continue feasible reliability, parity and UI work once the milestones function; pending commercial decisions are not a blanket stop.

## Finish

Run affected Go build/vet/full tests and meaningful races with real PostgreSQL contention/migration coverage; console check/tests/build; pinned proto lint/reproducibility; and changed Terraform/Compose/Helm/artifact/release checks. Preserve failure propagation and volume-safe shutdown. Add independent reviews of tenant authorization, allocation concurrency, durable recovery, migrations and world safety before handoff.

Deliver the same PR with exact new SHA/latest green CI, dependency disposition, criteria implemented, real fleet/agent/isolation and player-join evidence, visual journey results, cleanup manifest, migrations/forward recovery and rollback instructions, and precise remaining policy/external gates. Keep the PR draft while its necessary review/qualification remains outstanding. A refreshed matrix or another synthetic-only slice is not completion of this continuation.

