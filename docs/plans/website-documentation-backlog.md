<!-- SPDX-License-Identifier: MPL-2.0 -->
# Website documentation backlog: Jira creation handoff

This is a creation plan, not a record of created Jira issues. The user asked for
another agent to create the work items. No Jira issues were created for this plan.

## Product goal

Help a new self-hosting user understand Carbon Panel, install it, bring friends
into a first world, and use the features that distinguish it: Modpack Studio,
datapacks, staged updates, native pause, freeze, deep sleep and wake-on-join.

The website now publishes the existing repository docs under `/carbonpanel/docs/`.
The source manifest pins application content and examples to
`d37e61a26147c76a2e5cf6b2b81a067ec3c16c90`, labelled Development snapshot.
This is an initial publication, not acceptance of every existing guide.
Several current pages are sparse or contain older deployment/version guidance.

## Instructions for the creating agent

1. Confirm the Jira site and Carbon Panel project. Inspect issue types, priority
   names, epic/parent fields and supported link types instead of inventing IDs.
2. Search for existing documentation work by title and subject. Reuse suitable
   issues, preserving unrelated descriptions, and record the mapping below.
3. Create EPIC-DOCS first, then DOC-01 through DOC-14. These are local aliases;
   replace them with returned Jira keys in links and descriptions. Do not treat
   the aliases as existing issues.
4. Make every DOC item a child of the epic. Use Story for user journeys and Task
   for audit/validation work if the project supports them.
5. Apply labels `website`, `documentation`, `carbon-panel`. Add feature labels
   where listed. Map P1/P2 to the project's actual priority scheme.
6. Create the blocking links exactly as listed below, then the related links.
   Verify parentage, descriptions, statuses and link directions by reading back.
7. Leave all new work in Backlog/To Do, unassigned unless the user names an owner.
   Do not start implementation, assign sprints or set release dates from this plan.

Descriptions should contain the problem, target pages, scope, acceptance criteria,
verification and source references below. Include a link to this plan and
`docs/website-publishing.md`. Do not copy private infrastructure or workstation
details into Jira.

## Proposed navigation

| Section | Pages and user questions |
| --- | --- |
| Start here | What Carbon Panel is; supported environments; choose an install method; first admin; first server; invite your first player |
| Use the app | Navigation/status glossary; server controls; Modpack Studio; mods and dependencies; datapacks; staged updates; idle modes; console and files |
| Operate | Hostnames/ports; nodes; backups and recovery; users/roles/OIDC; upgrades and migrations |
| Reference | Version pin; configuration/environment; server settings; API/authentication; supported server types and feature prerequisites |
| Get help | Symptom-led troubleshooting; safe diagnostic collection; known limits; community/support |

Keep tasks in user language. Explain prerequisites before UI clicks, show expected
results after each consequential action, and cross-link related workflows.

## Epic

**EPIC-DOCS — Make Carbon Panel understandable from first visit to daily operation**

Type: Epic. Priority: P1. Outcome: a visitor can choose a deployment, launch a world,
use the four distinguishing feature workflows, recover from common failures, and
identify which app version a guide describes.

Done when DOC-01–DOC-14 meet their criteria and DOC-14 records the final journey
review. Excludes redesigning application controls, inventing unsupported features,
claiming formal operational acceptance, or automatically publishing a release.

## Work items

### DOC-01 — Audit the source docs and establish the user journey map

Type: Task. Priority: P1. Labels: `content-audit`.

Problem: existing docs were written at different points in the app's development.
Publishing them does not establish that every instruction is current.

Scope: inventory every published page, compare commands/options against the pinned
commit, record missing tasks and misleading statements, and finalize the navigation
above. Audit installation prerequisites, source builds, Proxmox helper provenance,
release downloads, API authentication, backup semantics and feature claims first.

Acceptance:
- A page-by-page matrix records audience, user task, source evidence, version,
  verified steps, gaps and owning backlog item.
- Unsupported external installers/downloads have explicit applicable versions or
  are removed; no unverified path is labelled recommended.
- Each page has one primary task, prerequisites and useful next links.
- Development-only and released behavior remain distinct.

Verify with source inspection plus bounded command checks. Track real install
verification separately. Sources: published docs tree, README, Go/Bun versions,
Compose, release workflows, `docs/website-source.json`.

### DOC-02 — Guide a new user from installation to their first connected player

Type: Story. Priority: P1. Labels: `onboarding`.

Problem: an install command does not explain first admin setup, data paths, creating
a world, or how another player reaches it.

Pages: install overview, supported environments, first-start checklist, first server,
first connection. Retain separate Compose/binary/source instructions where verified.

Acceptance:
- Prerequisites cover Docker access, persistent storage, supported host conditions,
  ports and memory without assuming a particular workstation.
- Steps explain first admin access, server type/version selection, resource limits,
  launch readiness, address/port and a player connection.
- Local play and public access are separate paths with clear prerequisites.
- Each stage shows its success signal and a recovery/help link.
- Persistent worlds/databases are identified before update or cleanup commands.

Verify one fresh supported installation and player join; record environment and
commit. A synthetic build or HTTP check alone does not satisfy the journey.

### DOC-03 — Explain app navigation, server states and everyday controls

Type: Story. Priority: P1. Labels: `app-guide`.

Pages: application tour, server details, status glossary, console/files overview.

Acceptance:
- Labelled screenshots from the pinned app orient users to overview, server list,
  server detail, configuration, tasks, packs and settings.
- Running, starting, stopping, stopped, native pause, frozen, deep sleep, waking,
  errors and unavailable-node states are explained with their observable signals.
- Start/stop/restart, save, logs and file actions describe consequences and required
  permissions. A normal restart is never presented as a backup.
- Screenshots include text alternatives and version provenance; conceptual website
  illustrations are not used as evidence of the actual app interface.

Verify against a running pinned app. Sources: app routes, server status components,
server operation runbook and RPC behavior.

### DOC-04 — Teach the complete Modpack Studio workflow

Type: Story. Priority: P1. Labels: `modpack-studio`.

Problem: a headline about modpacks does not teach authoring, compatibility, export
or deployment.

Pages: Studio overview, create/import a pack, find mods and resolve dependencies,
pack versions/loaders, deploy, export/share.

Acceptance:
- Walk through a small pack from creation/import to a running server.
- Explain CurseForge/Modrinth discovery, client/server compatibility, dependency
  resolution, loader/Minecraft versions and supported import/export formats.
- Show validation failures and what the user can change rather than implying every
  requested mod/version can be resolved.
- Explain what deploy changes and when a restart/staged batch is involved.
- Cross-link datapacks, staged updates and backup preparation.

Verify with a small reproducible pack, a deliberate incompatibility, export/import
and a real deployment. Sources: `modpacks/studio` routes, packwiz handlers/services,
modpack delivery/checksum guidance.

### DOC-05 — Add, manage and troubleshoot datapacks

Type: Story. Priority: P1. Labels: `datapacks`.

Pages: datapack concept, browse/install, upload ZIP, remove/update, compatibility.

Acceptance:
- Explain datapacks versus mods/plugins and the Vanilla/no-loader use case.
- Show browsing, installing and ZIP upload with pack.mcmeta expectations.
- Explain world directory placement, pack-format warnings and version compatibility.
- Describe whether an action is immediate or staged for this version, reload/restart
  requirements, permissions and the consequences of removal.
- Include invalid ZIP, incompatible format and failed-upload recovery examples.

Verify one searched install, one upload, one rejected archive, and safe removal on
a disposable world. Sources: server datapacks/browser components and datapack RPCs.

### DOC-06 — Prepare and apply a staged update batch

Type: Story. Priority: P1. Labels: `staged-updates`.

Pages: batch concepts, configuration/content staging, apply windows, discard,
conflicts and failed publication.

Acceptance:
- Explain one open batch per server and the difference between staging and applying.
- Cover supported config/mod/datapack/file/modpack/module changes with a capability
  table based on the pinned code, not assumptions about every UI action.
- Demonstrate next restart, scheduled UTC time, idle window, apply-now and discard.
- State that staging leaves live files/config unchanged; applying to a running
  server includes a restart. Explain frozen/deep-sleep behavior.
- Explain changed-live-value conflicts, journaled recovery, rollback limits and
  how a failed batch remains visible.
- Include a safe backup checkpoint and failure-recovery links.

Verify staging isolation, one due publication, cancellation and a controlled
failure with a disposable server. Sources: `knowledge/invariants/staged-updates.md`,
staging services/scheduler, server configuration screen and staged-update research.

### DOC-07 — Choose native pause, freeze or deep sleep and understand waking

Type: Story. Priority: P1. Labels: `sleep`, `wake-on-join`.

Pages: idle-mode comparison, prerequisites/settings, wake lifecycle, troubleshooting.

Acceptance:
- Compare CPU, retained memory, startup latency and supported versions/platforms.
- Explain native pause separately from cgroup freeze and stopped-JVM deep sleep.
- Document verified UI/operator configuration paths and defaults; do not invent
  toggles or generalize local-node support to every remote deployment.
- Explain watchdog prerequisites, idle/player detection, pings versus real joins,
  wake readiness and cold-start retry/time budgets.
- Clarify that zero JVM memory does not mean zero panel/proxy/host memory.
- Show disabling a mode safely and distinguishing operator stop from deep sleep.

Verify supported modes and a real player wake on a disposable world, including a
slow-start retry. Sources: sleep baseline, tier-B docs, freezer/sleep/idle/proxy code.
Reconcile stale runbook statements with the current implementation before publishing.

### DOC-08 — Explain networking, hostnames and remote nodes

Type: Story. Priority: P2. Labels: `networking`, `nodes`.

Pages: local/LAN/public connection, DNS and proxy routing, Docker nodes, storage paths.

Acceptance:
- Show how one ingress routes a hostname, with prerequisites and protocol limits.
- Separate Java, Bedrock and proxy-forwarding cases where behavior differs.
- Explain node enrollment/connectivity, placement, data paths and fail-closed
  unavailable-node behavior with portable example names.
- Explain TLS exposure guidance and minimum access requirements without broadening
  container privileges or recommending publicly exposed Docker sockets.

Verify local and one supported remote-node path, recording unsupported cases.
Sources: proxy/node configuration, node resolution invariant and Docker client pool.

### DOC-09 — Back up and recover worlds and the panel database

Type: Story. Priority: P1. Labels: `backup`, `recovery`.

Pages: backup strategy, server backups, retention/offsite/encryption, restore,
panel database recovery.

Acceptance:
- Distinguish world/server snapshots from panel SQLite snapshots.
- Explain consistency, RCON/save coordination, retention, remote storage and keys.
- Mark destructive restore steps and prerequisites explicitly, with safe verification
  and a way to preserve existing data before proceeding.
- Cover lost keys, missing/corrupt archives, remote failures and incomplete restore.
- Link the authoritative database restore runbook.

Verify a disposable backup/restore and recovery inspection; keep operational proof
separate from mock storage tests. Sources: backup guide, backup invariants, ADR 0007
and panel database restore runbook.

### DOC-10 — Set up users, roles and single sign-on safely

Type: Story. Priority: P2. Labels: `access-control`, `oidc`.

Pages: first admin, users/invites, role/server permissions, API tokens, OIDC providers.

Acceptance:
- Explain who can perform key actions and how server-scoped access differs from admin.
- Cover token scope/revocation and safe secret handling.
- Audit the four existing OIDC examples against the pinned configuration.
- Document expected denied-access behavior, login diagnostics and recovery access
  without weakening authentication as a troubleshooting shortcut.

Verify allowed and denied actions using non-production accounts; do not publish
tokens or real identity-provider secrets.

### DOC-11 — Upgrade deliberately and maintain version-aligned docs

Type: Story. Priority: P1. Labels: `upgrades`, `versioning`.

Pages: identify version, backup checklist, supported upgrade steps, rollback limits,
release notes and docs version policy.

Acceptance:
- Identify the installed version/commit and select matching guidance.
- Explain application versus Minecraft/modpack updates and migration implications.
- Document supported upgrade artifacts only after verifying their publication.
- Explain the snapshot manifest, deliberate pin advancement and future stable-release
  selection without labelling inherited tags as new releases.
- Test a known upgrade path with preserved worlds and a documented recovery boundary.

Sources: website publishing guide, release workflow, migrations and backup docs.

### DOC-12 — Turn common failures into actionable troubleshooting

Type: Story. Priority: P2. Labels: `troubleshooting`.

Pages: symptom index and focused remedies for startup/access, player connection,
mod resolution, datapacks, staged changes, sleep/wake, backups and nodes.

Acceptance:
- Each entry has symptom, safe checks, expected results, next action and escalation.
- Link errors/status labels to relevant concepts and workflow guides.
- Provide a redaction checklist for logs, tokens, world data and private addresses.
- Document how to report commit/version and minimal reproduction.
- Label known limitations and avoid treating retry as a cure for persistent errors.

Verify with representative controlled failures and a user who did not write the guide.

### DOC-13 — Improve findability and contextual help

Type: Story. Priority: P2. Labels: `navigation`, `search`, `accessibility`.

Scope: grouped sidebar, task landing pages, next steps, search vocabulary and relevant
in-app help links. Application link changes belong to their own reviewed patch.

Acceptance:
- Users can find installation, Studio, datapacks, staging and wake guidance from
  homepage and docs in at most two navigation choices.
- Search finds synonyms such as sleeping/frozen, queued/staged, pack/mod/datapack,
  and public address/hostname.
- Breadcrumbs/previous-next links follow a coherent task order.
- Mobile menu, keyboard search, focus and light/dark contrast are verified.
- Contextual app links preserve deployment base paths and identify docs version.

### DOC-14 — Validate the documentation journeys before calling them complete

Type: Task. Priority: P1. Labels: `docs-acceptance`.

Acceptance:
- A new user completes installation/first player plus the four distinguishing
  feature workflows using the docs without author coaching.
- Record the exact app/docs commit, environment, steps, outcomes and unresolved gaps.
- Broken links, search, mobile layout, keyboard navigation and API reference pass.
- Separate source review, synthetic checks, real runtime journeys and user acceptance.
  Unverified platform combinations remain explicitly unverified.
- Correct issues found, update the content audit, and advance the website source pin
  only after the reviewed source content is committed.

## Relationships and sequence

Every item has parent EPIC-DOCS. Use the actual project's "blocks" link and read
it back to ensure the direction is correct.

| Blocker | Blocks | Reason |
| --- | --- | --- |
| DOC-01 | DOC-02–DOC-13 | Establish source facts, taxonomy and version boundaries first |
| DOC-02 | DOC-03, DOC-08, DOC-09, DOC-10 | Shared installation and first-server prerequisites |
| DOC-03 | DOC-04, DOC-05, DOC-07 | Shared app terms, states and verified screenshot conventions |
| DOC-04, DOC-05 | DOC-06 | Batch examples and links depend on the pack/content workflows |
| DOC-09 | DOC-11 | Upgrade guidance needs a verified backup/recovery prerequisite |
| DOC-04–DOC-11 | DOC-12 | Symptom guidance links to completed feature/operations guides |
| DOC-02–DOC-13 | DOC-14 | Final journey review follows content publication |

Additional "relates to" links: DOC-04 ↔ DOC-05; DOC-06 ↔ DOC-07;
DOC-06 ↔ DOC-09; DOC-08 ↔ DOC-10; DOC-11 ↔ DOC-13.
Avoid adding reciprocal blocking links or cycles.

Suggested delivery:
1. Source audit, onboarding and app orientation.
2. Studio, datapacks, staged updates and idle modes; backups alongside them.
3. Networking/access/upgrades, troubleshooting and findability.
4. User journey validation and reviewed pin advancement.

## Creation receipt for the next agent

After creating/reusing issues, append a mapping of each alias to the real issue URL
and note reused issues, unsupported link/type mappings, and any unresolved choices.
Do not mark implementation or acceptance complete merely because issues exist.
