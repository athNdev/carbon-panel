---
type: Failure Mode
title: Known gaps
description: Cross-cutting gaps that are not defects to rush, with the live tracker as the authority.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/db/server_operation.go
  - internal/worldstore
  - cmd/carbon-storage
  - internal/docker/freezer.go
  - internal/module/hooks.go
sources:
  - title: Reliability plan
    resource: ../../docs/plans/reliability-hardening.md
---

# Current gaps

- `internal/db/server_operation.go` is schema-only: the journal type exists and
  migrates, but nothing creates or advances an operation yet.
- `internal/worldstore` and `cmd/carbon-storage` have no production caller; the
  binary is not built by any Dockerfile or release target.
- `internal/docker/freezer.go` still targets the local client unconditionally.
- `internal/module/hooks.go:execInModule` uses the manager's client directly.
- Remote file, Minecraft and backup archive paths still read the controller
  filesystem; only restore refuses. This is tracked as the remote archive path gap.

# How to use this

This concept is a map, not a backlog. The live tracker and
the reliability plan is authoritative for status. If a gap is fixed, update
this file in the same change.

# Read next

- [Remote node fallback](remote-node-fallback.md)
- [Persistence](../architecture/persistence.md)
