---
type: Decision
title: Architecture decisions
description: Index of the accepted ADRs and the concept each one governs.
status: stable
owners: [oss-runtime]
code_refs:
  - docs/adr
sources:
  - title: ADR directory
    resource: ../../docs/adr
---

# Accepted decisions

| ADR | Decision | Governs |
| --- | --- | --- |
| [0001](../../docs/adr/0001-moby-sdk-migration.md) | Use the Moby SDK instead of a shelled-out Docker CLI | [Runtime](../architecture/runtime.md), [Node resolution](../architecture/node-resolution.md) |
| [0002](../../docs/adr/0002-sveltekit-resolve-navigation.md) | Use SvelteKit resolve/navigation conventions in the console | [Frontend change](../workflows/frontend-change.md) |
| [0003](../../docs/adr/0003-sqlite-durability.md) | SQLite durability and migration posture | [Persistence](../architecture/persistence.md) |

# Rule

The ADR is authoritative for rationale. This index exists so an agent can find
the decision without reading every file. When an ADR is superseded, update this
table and mark the old concept `deprecated` rather than deleting it.
