---
type: Invariant
title: Staged server updates
description: Staging a config, mod, datapack, or file edit records the open batch and does not restart the server or write the live tree.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/db/staged_config.go
  - internal/db/staged_content.go
  - internal/rpc/services/stage_content.go
sources:
  - title: Staged update research
    resource: ../../docs/research/staged-update-application.md
verified: { by: agent:cursor, at: 2026-10-28 }
review_after: 2027-01-28
---

# Rule

A server has one open update batch. Staging adds to that batch. It must not
call Docker, change server status, write the live `ServerConfig` row, or write
the mods directory, the datapacks directory, or the world.

File bytes for a staged install or replace go through node storage at
`.carbon-panel/staged/<id>/` inside the server data root. A delete records the
live path and removes nothing. Discard deletes those staged bytes. A remote
node whose storage is unreachable refuses the stage and gets no
controller-local copy.

Client-only mod jars and archives that are not datapacks are rejected before
any staged byte is kept. Applying the batch on a clock time or after idle time
is a separate step from recording it.
