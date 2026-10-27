# OSS agent follow-up 2 — implement remote data safety and the real fault harness

Resume implementation of **MINE-249**. Your tracker reconciliation and Windows/test hardening are useful. The supervisor found **no confirmed introduced defects in the inspected incremental changes**, and verified nine green CI checks at `db19d699b8f170aae55406546013dc500bbb1cfb`. This is a scoped review of a partial implementation, not acceptance of the Epic.

The next handoff must contain substantive runtime and integration work. Updating the matrix, adding refusal paths, or restating the remaining Backlog is not completion of this continuation. Preserve the valid patches; do not restart from zero.

## Checkout and specification

- Repository: `C:\research_learning_new\carbonpanel`.
- Branch: **codex/oss-docker-resilience**; **[PR #225](https://github.com/athNdev/carbon-panel/pull/225)**, base main.
- Reviewed full base: `911b3aa77bcefcb90787a1395159ae0e64a4903f`; most recent reviewed head: `db19d699b8f170aae55406546013dc500bbb1cfb`. Refresh before editing.
- Epic MINE-249 UUID: `130ad67e-20c0-4a02-be60-69941426c367`. At review it had 23 children: MINE-250–271 plus MINE-224.
- Read `docs/plans/oss-live-acceptance-matrix.md` (73 criteria), the runtime reliability plan, feature completeness matrix, Docker failure-semantics research, AGENTS.md and current CI/ADRs. Live Plane criteria remain authoritative.

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

## 1. Patch the known affected dependency without unrelated upgrades

The supervisor verified OSS resolves **github.com/klauspost/compress v1.18.6**, which falls in the advisory's affected range. Cloud separately resolves v1.18.0; its agent owns its upgrade. Upstream identifies **v1.18.7** as patched:
https://github.com/klauspost/compress/security/advisories/GHSA-259r-337f-4rfw

Upgrade this module to v1.18.7 or a newer compatible patched release after checking current upstream metadata. Keep the change narrow; do not run a blanket upgrade or introduce a local replace. Inspect go.mod/go.sum and the selected module graph. Document that the advisory concerns untrusted S2 dictionaries passed through NewDict and Dict.Encode; affected module presence does not itself prove that attack path is reachable. OSS's binary dependency graph includes s2, so inspect actual callers and report reachability honestly. Run relevant archive/compression regressions and vulnerability analysis, then full affected checks. Check Plane for an existing advisory item before creating one; link exact version/evidence to the relevant release-reliability work.

## 2. Build MINE-224's reusable disposable harness now

Start this alongside the runtime work, not after the entire Epic. Provide a documented, reproducible entry point for:
- Protocol-faithful Docker fault injection: timeout/connection loss before and after a daemon mutation, ambiguous responses, partial image-pull errors and daemon reconnect.
- Two isolated owned Docker endpoints with genuinely separate filesystem roots; prove node B operations never touch A. Declare any unsupported platform/tool prerequisites explicitly.
- Fixture manifests recording run ID, installation/node identity, owned containers/images/volumes/paths and exact cleanup scope. No global prune, shared-volume deletion or changes to unrelated workloads.
- Assertions about container IDs, operation phases, world/file contents and hashes, persisted intent and recoverability after process interruption.

Normal unit tests should remain nonprivileged and portable. Put genuine infrastructure qualification behind an explicit integration command. Missing prerequisites must produce an honest result rather than a green success message that ran nothing.

## 3. Implement the node-addressed storage seam and migrate complete caller paths

Prioritize **MINE-255, MINE-258 and their MINE-250/252 prerequisites**. The remote restore guard is a correct interim refusal, not remote storage support.

Design a small storage interface whose operations carry the server, selected node/daemon identity, ownership/generation and authorized root. Provide a real local implementation and a real supported remote implementation. Choose the transport from the existing architecture and document its guarantees; do not assume a remote DataPath is a path on the controller.

Migrate a complete remote vertical slice: list/read, upload/download, backup creation and restore. Cover RPC/background entry points, authorization, progress/cancellation and errors. Identify remaining file/config/content/archive/metrics callers; route them through this seam or return explicit unsupported capability results until migrated. Do not silently fall back to controller I/O. Verify effective bind/volume identity and constrain operations to the owned world root; reject traversal, symlink escape, wrong-node artifacts and stale ownership.

For backup and restore:
- Coordinate every writer and relevant lifecycle path, including running/paused/starting/unknown state, uploads, commands, tasks and content operations.
- Flush/stop according to the operation contract; retain intended stopped/running state. Fail closed when quiescence or ownership cannot be established.
- Validate archive/checksum/bounds, extract into staging on the owning storage, verify named contents, and replace deliberately rather than overlaying a live directory.
- Persist enough operation intent, stage/generation and retained identities for **MINE-252** restart recovery. Keep the previous world until successful cutover and finalization; provide deterministic recovery for extraction, rename, DB-save and process-crash failures. Reconcile uncertain effects before retrying.
- Prove restore removes obsolete files from the replaced world and preserves the old world on failure. Do not claim atomicity/durability from a successful rename alone.

Use the harness to prove local and remote backup/restore, including real named world data and hashes. A fake Docker HTTP response plus archive existence is not sufficient.

## 4. Continue lifecycle safety and real journeys

After the storage slice, continue **MINE-252/253/263**: preflight the replacement image/config/mount/resources before removing a working container, retain a recoverable known-good identity/config, and persist/fence cutover so image/auth/disk/create/start/DB failures and ambiguous responses cannot lose the server. Reuse the same operation coordinator rather than introducing unrelated locks per caller.

Qualify one local and one remote create → start → actual Minecraft client join → modify world → backup → restore → rejoin journey with named contents and cleanup evidence. A running container, ready API, MOTD or server-list ping alone is not player-join evidence. Add wrong-node, cross-permission and interrupted-operation negative cases. Use isolated test runtime settings without weakening production authentication or ingress.

Continue other feasible Epic criteria once these milestones work. Do not treat the above as a replacement for the original Epic's remaining scope.

## MINE-271 and Windows behavior

The screenshot established **Windows Firewall consent for services.test.exe**, not UAC elevation. Your injected loopback service probe and proxy fixture changes are preserved; production listener behavior must stay correct. Do not revert to calling this an unexplained elevation request.

Verify repeated uncached narrow and standard native Windows runs through the ordinary unelevated agent launch path. Capture the desktop/dialog behavior if an available tool can genuinely observe it; process exit success alone is not visual confirmation. If UI observation is unavailable, state that capability limit precisely and retain the missing acceptance. Never disable firewall/UAC, blanket-allow temporary executables, or weaken production Minecraft/wake ingress.

## Finish

Run current repository checks for every affected surface, including real integration/journey checks described above; record exact commands, versions, red-before/green-after where possible and limits. Review data safety, permissions and recovery independently. Update existing child comments with the new runtime evidence instead of posting repetitive generic suite-pass comments.

Handoff: same PR, exact new SHA and latest CI; selected dependency version/advisory disposition; implemented criteria; harness invocation and cleanup proof; local/remote world and join evidence; migrations/rollback/recovery instructions; remaining exact IDs/decisions/external qualifications. Keep partial items honest. Do not call the Epic or this implementation milestone finished merely because the acceptance table is current.

