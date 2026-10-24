---
type: Workflow
title: Database migration
description: How to change the SQLite schema without risking existing controller or user data.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/db
sources:
  - title: SQLite durability ADR
    resource: ../../docs/adr/0003-sqlite-durability.md
---

# Steps

1. Add the model or migration under `internal/db` following the existing
   embedded-migration pattern.
2. Cover a fresh database and an upgrade from the previous schema.
3. Back up the database before running a destructive or large migration.
4. Run `go test ./...` and `go vet ./...`.
5. Record any manual recovery step in the migration comment or the relevant
   runbook.

# Rules

- Forward-only. Do not rewrite or drop user data in place.
- Do not change a column type without a copy-and-swap migration.
- Do not hold a transaction open while calling Docker, S3 or the network.
- A schema-only change is not a feature; do not describe it as one.

# Read next

- [Persistence](../architecture/persistence.md)
- [Data safety](../invariants/data-safety.md)
