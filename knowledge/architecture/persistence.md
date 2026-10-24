---
type: Architecture
title: Persistence
description: SQLite is the controller's single-writer store, with embedded migrations and durable user data.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/db
  - internal/snapshot
  - internal/s3
sources:
  - title: SQLite durability ADR
    resource: ../../docs/adr/0003-sqlite-durability.md
---

# Shape

The controller uses SQLite through GORM. The database file is the controller's
state; server worlds and backups live outside it and are persistent user data.
Migrations are embedded and run at startup.

Worlds, snapshots and remote archives are handled by `internal/snapshot` and
`internal/s3`. The database holds metadata about them, never the world data
itself.

# Rules

- Migrations are forward-only. Do not drop or rewrite user data in a migration
  without an explicit, separately named destructive operation.
- A new table or column needs a test that covers a fresh database and an
  upgrade from the previous schema.
- Do not hold a write transaction while calling Docker or the network.

# Read next

- [Database migration workflow](../workflows/db-migration.md)
- [Data safety](../invariants/data-safety.md)
