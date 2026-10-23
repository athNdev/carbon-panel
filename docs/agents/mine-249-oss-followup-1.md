# Follow-up — finish the OSS resilience Epic after acceptance reconciliation

Continue your existing work. Preserve the useful patches and audit them against the live Epic before deciding what must change. Tracker access problems alone are not grounds to restart from zero.

## Existing branch and independent review

- Repository: `C:\research_learning_new\carbonpanel`.
- Continue **codex/oss-docker-resilience**, **[PR #225](https://github.com/athNdev/carbon-panel/pull/225)** targeting main. Do not create a second Epic PR.
- Supervising review pinned base `911b3aa77bcefcb90787a1395159ae0e64a4903f` and head `096432cf743fdc6184ea567c62197b1b6fa1ce62`. The checkout was clean and all nine current GitHub checks passed at that head. Refresh remote/head/status before editing.
- The read-only Standards review found **no confirmed introduced regression**. The Spec review confirmed substantial **unmet acceptance**, including the three caller paths below. These are remaining Epic requirements, not allegations that every patch is wrong. No new application tests or daemon/journey checks were run by the supervisor.

## Plane access is repaired — use this connection

Use the native **plane_homelab** MCP tools for the self-hosted workspace **homelab**, project **CarbonPanel / MINE**, project UUID `51da0c85-fe79-4427-9783-617dfcb2aaf1`. The server is already registered for this Windows user, supplies its own credential and workspace, and has passed actual MCP reads, comments, issue updates, state changes and directional dependency creation. Temporary verification records were removed.

The hosted `mcp__codex_apps__plane` connector is a separate OAuth connection. Its reauthentication error is not a failure of plane_homelab. Do not spend more turns retrying that connector or ask me to reauthenticate it. Start with `workitem(action="retrieve_by_identifier", workitem_identifier="MINE-…")` through plane_homelab, then retrieve the full child descriptions, comments, states and relations. Follow pagination; do not rely on names or the old kickoff alone. Resolve state/issue UUIDs before writes, read back changes, and check existing relations before adding them.

The local relation adapter supports **list and create only**, preserving blocked_by/blocking direction. Relation removal and custom-relation definitions are not exposed by this instance's public API; record an incorrect link for supervisor correction instead of probing unsupported endpoints or editing the database. Unsupported features, HTTP429, actual local401 and network failures must be reported precisely. Honor Retry-After and check whether an uncertain mutation committed before retrying.

If the native tools have not loaded in your existing chat, refresh tool discovery/connection before declaring Plane unavailable. Registration can be inspected with `codex mcp get plane_homelab`; that alone does not establish live access. Local connection/runbook: `C:\Users\Daybreak8458\Documents\Codex\mcp-servers\plane-homelab\README.md`. Keep credentials and this machine's connection configuration outside either repository. Do not print the token or copy it into source, logs, PR text or subagent prompts.

## Reconcile the complete scope first

Read **MINE-249** (UUID `130ad67e-20c0-4a02-be60-69941426c367`), all children **MINE-250–270**, **MINE-224**, and newly added **MINE-271**. There were **23 children** when refreshed; discover any additions. Read comments, linked historical issues and native dependencies as well as descriptions. A local read-only snapshot is available at `C:\Users\Daybreak8458\Documents\Codex\2026-10-23\carbon-epic-kickoffs\oss-plane-snapshot.md`; live Plane takes precedence.

Build/update an acceptance matrix with one row per criterion: current implementation/source, exact regression or integration evidence, remaining work, dependency/decision, and truthful status. Compare it with:
- `docs/plans/oss-runtime-reliability-and-feature-completeness.md`
- `docs/plans/oss-feature-completeness-matrix.md`
- `docs/research/docker-failure-semantics-and-recovery.md`

Backfill Plane with your actual partial changes for **250, 253, 256, 258, 263**, and caller fixtures for **224**, linking exact commits/tests and stating what they do not prove. Missing runtime acceptance stays Backlog; MINE-224 was still Todo when refreshed and must not imply a completed harness. Keep the Epic open. Preserve valid requirements and history rather than replacing their acceptance with the smaller implemented slice.

## Concrete follow-up from the review

1. **MINE-255/258 — restore must reach the owning storage and replace safely.** At reviewed head, `internal/rpc/services/backup.go:371` still calls local `files.ExtractArchive(archivePath, server.DataPath)` after selecting a remote Docker client only for stop/start. This can touch the controller filesystem while the remote world remains unchanged; local extraction overlays existing files and can partially write on failure. Implement a node-correct storage/restore seam, stage and verify data, coordinate every writer, provide recoverable cutover/rollback, and preserve prior world contents and intended runtime state. Unknown runtime/ownership must prevent mutation. Verify named files, removed stale files, failure mid-extraction/cutover, interrupted recovery and two hosts with isolated filesystems.
2. **MINE-256 — port claims must describe the selected host/address/protocol.** `server.go:1087–1097` checks the selected daemon then unconditionally probes controller-local sockets. `GetNextAvailablePort` at roughly 2705–2720 uses the default daemon/controller scope. Do not treat a controller socket as evidence about a remote host, or a failed daemon query as availability. Unify atomic game/RCON/additional/proxy leases, TCP/UDP/address/node scope, effective bindings and reachable endpoints. The local socket helper is only local evidence.
3. **MINE-252/253 — recreation remains destructive before replacement preflight.** `internal/docker/client.go:1453–1466` removes the old container before CreateContainer and image/create qualification. Unknown-inspect handling is useful but does not supply a reversible known-good config/image/container or crash recovery. Implement preflight and durable operation identity/stages/fencing, retain recovery identity until cutover succeeds, and test image/auth/disk/create/start failure plus ambiguous daemon responses and process restart.
4. **MINE-224 — build the real fault/journey harness alongside these fixes.** Fake/client-level tests are useful regression coverage but do not qualify real Docker behavior, daemon disconnection, separate host storage, or the Minecraft journeys. Use disposable owned infrastructure; never existing user worlds.

Line references refer to the reviewed SHA; relocate by symbol after changes.

## Continue the rest of the feasible Epic

Do not stop at tracker backfill or the five initial patches. Work through: daemon identity/strict routing (250); install ownership and orphan quarantine (251); durable lifecycle operations (252); dynamic watchers/reconnect (254); node-aware file/config/content/archive/metrics I/O (255); verified world migration (257); consistent backup/atomic restore/offsite encryption/retention (258–259); SQLite durability matching ADR0003 and actual recovery (260); bootstrap/mount/UID/rootless/effective override qualification (261); enforced resources including world storage (262); streamed image failures and reproducible digest identity (263); API/node/game readiness and freshness (264); schedule/content/sleep coordination (265); module/sidecar convergence (266); resumable deletion/detach (267); actionable diagnostics/retry evidence (268); portable upgrade/rollback/release qualification (269); and **O01–O32, D01–D60, J01–J07** feature/fault/journey closure (270/224). Reproduce source-observed candidates before calling them confirmed failures.

**MINE-271:** diagnose the repeated Windows UAC/firewall prompts in ordinary tests. Capture the exact prompt and executable first; distinguish actual elevation from firewall consent for newly built Go test binaries. The `internal/proxy/direct_knock.go` wildcard listener and test caller are candidates, not a proven cause. Prefer loopback/injected test listeners while preserving production Minecraft/wake ingress. Do not disable UAC/firewall globally, require routine elevation, or broadly allow temporary executables.

Follow AGENTS.md. Preserve fail-closed authorization, minimal image capabilities, writable persistent data/read-only root behavior, strict node routing, graceful stop/OOM truth, resource headroom and restart/sleep intent. Keep workstation/cluster addresses and generated state out of tracked source. Keep all user data and pre-existing unrelated changes intact.

You may use bounded subagents. Give each its exact checkout/base/head, relevant live criteria and decisions, source paths, contracts, owned files, expected evidence and handoff boundaries. For routine implementation/review use **Luna Medium** explicitly when supported; use Luna High/xhigh for harder work. Inspect reported model/settings rather than assuming inheritance changes models. A stronger model needs a clear bounded reason; do not spawn several expensive full-context copies.

You are authorized to add/run relevant regression, integration, fault, visual and journey checks. Iterate narrowly, then run every affected surface's required checks and CI; Windows portability failures must be diagnosed rather than dismissed as a permanent excuse. Record real environmental blockers precisely, keep their acceptance Backlog, and continue independent work. Difficulty or breadth alone is not ambiguity. Never weaken required checks or claim a skipped external test passed.

Comment on every partial/ambiguous/blocked item with criterion, code/evidence, what is missing, why, and a concrete next action/question. Keep such items Backlog. Do not mark Done on green unit tests alone or close the Epic before supervising acceptance.

Stage explicit paths, commit and push incrementally to the same branch/PR. Iterate CI until every required check is green on the **latest pushed SHA**. Update the PR and Plane to agree about implemented, verified, partial and unverified scope. Handoff PR/head/CI links, acceptance matrix, remaining IDs/blockers and migration/data-preserving rollback instructions. **Do not merge, enable auto-merge, push main, release or deploy.** Continue autonomously until all feasible specified work is finished and only real decisions/external qualifications remain.

