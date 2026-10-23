# Kickoff prompt — OSS Docker resilience and feature completeness

You own implementation of Carbon Panel's OSS reliability Epic. Work autonomously through every sufficiently specified, feasible item, verify the real behavior, and leave one reviewable feature PR. This is implementation work, not another planning-only audit. The supervising agent will review your work and decide whether to merge.

## Repository, Epic and inputs

- Repository: `C:\research_learning_new\carbonpanel`.
- Plane workspace: `http://192.168.0.119/homelab/`; project `CarbonPanel`, identifier `MINE`.
- Epic: **MINE-249 — [Epic][OSS] Docker resilience, data safety and feature completeness**.
- Epic link: `http://192.168.0.119/homelab/browse/MINE-249/`.
- Expected feature branch: `codex/oss-docker-resilience`; PR base: `main`.
- Scope currently contains **MINE-250–270 plus existing MINE-224**. Refresh the Epic, all descriptions, comments, acceptance criteria, states and native dependency links before starting; the live tracker may have changed.

Read the repository's applicable `AGENTS.md`, README, build configuration, CI workflows and ADRs. Then read these handoff documents in full, using their source references to inspect the actual callers:

1. `docs/plans/oss-runtime-reliability-and-feature-completeness.md` — work packages N01–N21, invariants, implementation order, 60 fault scenarios D01–D60 and acceptance criteria.
2. `docs/plans/oss-feature-completeness-matrix.md` — 32 feature families O01–O32 and seven complete journeys J01–J07.
3. `docs/research/docker-failure-semantics-and-recovery.md` — official Docker/itzg research and evidence limits; refresh official sources when changing version-sensitive behavior.
4. `docs/plans/reliability-hardening.md` and relevant accepted ADRs, particularly ADR 0003 on SQLite durability.

These are source-backed planning findings, not proof that incidents were reproduced or tests passed. The snapshot was HEAD `4cd35359616ffc58602349a61868f763e27f1fb2` plus inspected working files. Recheck the current implementation and preserve fixes that have landed since.

## Start safely and establish the branch

1. Inspect status, branch, diff, remotes and recent commits before editing. At preparation time the checkout was on `codex/reliability-cleanup`, with a pre-existing modification to `internal/rpc/services/server.go`, a modified older plan, and untracked Epic planning/research documents. Preserve those changes. Do not blanket-stage, reset, clean or overwrite other work.
2. Fetch and inspect `origin/main`. Create or reuse the single feature branch for this Epic, using a suitable checkout or managed worktree. Account for unmerged prerequisites explicitly. The new planning documents may exist only in the current working directory: deliberately carry the relevant documents into your feature branch if using another checkout; do not assume they are on `main`.
3. Commit only changes you have reviewed and can account for, using explicit paths/hunks. Use incremental commits tied to Plane IDs, while keeping all Epic implementation on this one feature branch and one PR.
4. You are explicitly authorized to **use subagents** for bounded implementation, research, regression coverage and independent review. Assign clear file/module ownership and dependency contracts; prevent concurrent edits to the same area. You remain responsible for integration, verification, Plane accuracy and the final PR.
5. Own only this repository. The Cloud agent owns `C:\research_learning_new\carbon-cloud` and MINE-231. You may inspect its contracts, but record cross-repository needs as Plane dependencies; do not edit, push or merge its branch.

## Plane access and truthful progress

Use the Plane connector if functional. The connector previously needed reauthentication, while direct REST worked. The API base is `http://192.168.0.119/api/v1/workspaces/homelab/projects/51da0c85-fe79-4427-9783-617dfcb2aaf1`; work items use `/work-items/` and an `X-API-Key` header. Obtain the credential through the configured connection or supplied secret/environment (`PLANE_API_KEY`); never put it in source, comments or logs. If access is unavailable, continue independent implementation and maintain an explicit pending tracker update list. Respect pagination and rate limits; reconcile before retrying a create with an unknown outcome.

- Start an item only after understanding its scope and dependencies. Preserve existing issue history and reuse related issues rather than creating duplicates.
- If an item is **ambiguous, blocked, only partially implementable, or cannot be fully verified**, comment honestly and **keep/return it to Backlog**. Do not move it further merely because a helper, mock, UI control or partial patch exists.
- Use this comment structure: `Status: ambiguous / blocked / partial`; `Acceptance criteria affected`; `What was attempted and observed`; `Implemented subset, commit and tests`; `Remaining gap or exact decision/access needed`; `Suggested elaboration/reframing`; `Dependencies and next verification`.
- Make routine engineering choices yourself. Reserve Backlog for actual unresolved scope, missing prerequisites or unmet acceptance, not simply difficult work. Continue other independent items.
- For implemented items, record commits, test evidence, limitations and the PR link. Use the project's review state if available; otherwise retain In Progress with a review-ready comment. Leave final Done/closure to the supervising review. Keep the Epic open while any required child remains incomplete.
- The supervisor will elaborate/reframe the Backlog items for a later retry. Never silently reduce their acceptance criteria or claim the whole Epic is complete.

## Implementation scope and ordering

Follow the live dependency graph rather than issue-number order. Begin MINE-224's isolated integration/fault harness alongside the earliest fixes. Establish node identity, ownership and durable operation coordination before making migration, restore, deletion or automation depend on them.

| Items | Required work |
| --- | --- |
| MINE-250–252 | Explicit node/daemon identity; installation-scoped ownership and orphan quarantine; durable lifecycle intent and recovery of uncertain Docker outcomes. |
| MINE-253–256 | Reversible recreation; dynamic event watchers and full reconciliation; node-aware files/config/content/metrics storage; protocol/address/node-aware port and endpoint contracts. |
| MINE-257–260 | Actual verified world migration; consistent atomic local/remote backup and restore; real encrypted/offsite artifact wiring and retention recovery; SQLite durability consistent with the accepted promise. |
| MINE-261–264 | Effective image bootstrap/mount/UID/rootless/override compatibility; enforceable runtime limits; streamed image-pull error decoding and digest identity; separate API/node/game readiness and observation freshness. |
| MINE-265–268 | Durable coordination of schedules/content/sleep; module and sidecar convergence; resumable node-safe delete/detach; actionable diagnostics with safe retries and truthful unknown states. |
| MINE-269–270, MINE-224 | Portable deployment/upgrade/rollback qualification; closure of advertised workflows against O01–O32 and J01–J07; fault and real user-path coverage throughout. |

Protect these specific boundaries:

- A specified remote node that is unavailable must never fall back to the controller's local daemon or command executor. Check scheduler and command paths as well as the strict pool. An endpoint or daemon identity change invalidates old observations and clients.
- Unknown inspect/create/stop outcomes require observation and reconciliation. They do not authorize force removal, duplicate creation or guessed success. Prepare image/config/storage before replacing a working generation; retain recoverable previous state until cutover is verified.
- Database absence and a generic managed label are insufficient authority to delete a container. Distinguish installations, legacy adoption and restored databases; quarantine uncertain ownership.
- Docker bind paths belong to the selected daemon host. Files, configuration, content, backups, snapshots and disk metrics must follow that same node and world identity.
- Migration must transfer and verify actual world data before switching ownership/routes or scheduling source cleanup. Restore must coordinate all writers, use staging and recoverable cutover, and preserve the previous world on failure. Save/flush failures are meaningful failures.
- Completed MINE-192/194 contain quota/encryption helpers whose full runtime integration was not established by the audit. Prove actual callers and effective enforcement/ciphertext/restore; do not reopen the old work indiscriminately or claim helpers alone complete the feature.
- Preserve single-controller/single-writer SQLite. Address ADR 0003's continuous-recovery promise versus the shipped daily snapshot behavior explicitly; do not silently change an accepted durability contract or introduce an unrelated database migration.
- Preserve existing least-privilege itzg bootstrap support: required CHOWN/SETGID/SETUID, NSS handling, readonly root and writable data. Validate the final merged Docker configuration and supported overrides. Do not solve compatibility through blanket privileged mode, writable roots or broad capability grants.
- Preserve bounded API negotiation, construction outside global locks, deterministic identities, graceful stop, actual `OOMKilled` classification, bounded restart/deep-sleep intent, JVM/native memory reserve, log rotation, PID limits and transactional game/RCON port allocation.
- Fail closed on authorization for every new RPC, storage operation, job and stream. Keep data containment, archive bounds, secret redaction and per-server rights across remote adapters.
- Keep Docker, release/build configuration and documentation portable. No workstation paths, cluster addresses, credentials, worlds, local databases or generated build state in tracked source/examples.
- Reuse open MINE-225/226/229 for generic loading/error/retry/layout work rather than duplicating it. Preserve stable server tabs and scroll geometry; inspect UI changes visually, including small screens and reduced motion.

## Verification and CI

You are authorized to add and run the tests necessary for this Epic. Use the narrowest meaningful checks while iterating, then all checks for affected surfaces:

- Backend: `go test ./...` and `go vet ./...`; add appropriate race coverage for changed coordination/concurrency.
- Frontend, from `web/carbon-panel`: `bun install --frozen-lockfile`, `bun run check`, `bun run test`.
- Production frontend: `bash scripts/web-build.sh`.
- Proto: `make proto-lint` when applicable, plus generated-code reproducibility checks required by CI.
- Read current CI and tool versions rather than assuming the commands above are exhaustive.

Add regressions at actual caller/integration seams, including distinct local/remote identities and restart/reconnect/unknown-outcome cases. Use disposable, explicitly owned daemons, containers and storage. Never prune a shared daemon, delete operator data, exhaust a real host's disk or disrupt unrelated workloads for a fault test. Exercise actual game connectivity and named world contents for migration/restore/readiness claims; HTTP health or mocks alone cannot establish those claims. Record unavailable environment-dependent checks honestly and keep affected items in Backlog.

Open the PR early, optionally as a draft. Include an item-by-item scope/acceptance checklist and link MINE-249. Push incremental fixes, inspect CI logs, fix regressions and rerun until every required check is green on the **latest PR SHA**. Do not skip, weaken, suppress or mark failing checks successful to manufacture green. Investigate pre-existing failures and document any real external blocker; do not claim readiness while checks remain red, pending or materially unverified. Perform an independent subagent review of data safety, node targeting, authorization and crash recovery, then address its actionable findings.

## Finish and handoff

Leave one pushed feature branch and one PR targeting `main`, ready for supervising review when its implemented scope is verified. **Do not merge, squash-merge, enable auto-merge, push directly to main, publish a release or update live deployments.** The supervisor owns merge and rollout.

Your final handoff must include: PR URL, branch and exact SHA; implemented Plane IDs with acceptance/test evidence; CI run links; fault/journey coverage and visual evidence; remaining Backlog IDs with precise unresolved questions; migration/compatibility/rollback notes; and any pending Plane updates. Distinguish implementation verification from production acceptance. Start by inspecting the repository and Epic, then proceed through the work; do not stop after producing another plan.
