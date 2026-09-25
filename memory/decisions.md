<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


# memory/decisions.md — decision log (append-only)

Never edit or delete rows. Supersede with a new row (new id, status `supersedes <id>`).

| id | date | status | decision | why | links |
|----|------------|--------|----------|-----|-------|
| D-001 | 2026-09-16 | active | Tier 0 file-native context + qmd chosen over mem0/Zep/pgvector-now | Zero infra, no UI, grep fallback keeps recall working without a daemon | docs/context-layer.md |
| D-002 | 2026-09-16 | active | Plane work items are product truth; Pages API 404s on this instance so ADRs live in docs/adr/ | Avoids a second roadmap copy; markdown links Plane IDs instead of duplicating them | docs/adr/ |
| D-003 | 2026-09-16 | active | Single-writer active-VM rule for memory writes | Prevents concurrent sessions clobbering memory/ logs and decisions | .agents/skills/memory-ops/SKILL.md |
| D-004 | 2026-09-16 | active | Self-heal only when a container is *missing*; an existing-but-exited container is reported, never auto-restarted | Auto-restarting fights an operator `/stop` and risks crash loops; recreation covers the Watchtower/force-recreate loss the epic targets | internal/reconciler/reconciler.go |
| D-005 | 2026-09-16 | active | Reconciler emits SERVER_START/SERVER_STOP only for transitions the RPC service did not already announce | Avoids duplicate module auto-start and double event-hook dispatch on panel-initiated start/stop | internal/reconciler/reconciler.go |
| D-006 | 2026-09-19 | active | 0-swap (MemorySwap == Memory) retained as Minecraft container default; off-heap safety handled by EnsureMemoryHeadroom | Minecraft JVM GC suffers severe latency spikes/watchdog crashes on swap; host cgroups on LXC/unprivileged fail if swap is forced | MINE-128, internal/docker/client.go |
| D-007 | 2026-09-25 | active | Docker SDK is moby/moby/{client,api}; netip.Addr at all network boundaries; narrow proxy.DockerInspector replaces CommonAPIClient | Deprecated frozen module + vuln excuses; struct Port/Addr types force boundary parsing | docs/adr/0001-moby-sdk-migration.md, MINE-134 |
| D-008 | 2026-09-25 | active | SvelteKit navigation via resolve(); generic href props stay plain with scoped disables; lint green enforced in CI | Subpath-base safety vs typed-resolve limits; rule catches regressions | docs/adr/0002-sveltekit-resolve-navigation.md, MINE-154 |
