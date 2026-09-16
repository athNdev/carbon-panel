<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


# memory/index.md — persistent-context pointer list

Read this file at session-start. Follow the pointers; don't duplicate them.

- Decisions (active rows only at start): `memory/decisions.md`
- Volatile notes: `memory/scratchpad.md` (cleared at weekly compaction)
- Today's log: `memory/2026-09-16.md` (one file per day, `YYYY-MM-DD.md`)
- Architecture record: `docs/adr/` (durable decisions, `NNNN-title.md`)
- Product truth: Plane project **MINE** (MineServer) — link work-item IDs, never copy them
- Runbook: `docs/context-layer.md`
- Full session recipe: `.agents/skills/memory-ops/SKILL.md`
