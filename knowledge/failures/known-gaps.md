---
type: Failure Mode
title: Known gaps
description: Cross-cutting gaps that are not defects to rush, with the live tracker as the authority.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/module/hooks.go
  - internal/docker/freezer.go
  - cmd/carbon-panel
  - internal/s3
sources:
  - title: Reliability plan
    resource: ../../docs/plans/reliability-hardening.md
---

# Current gaps

- `internal/module/hooks.go:execInModule` runs hook commands through the
  manager's own Docker client (`m.docker.Exec`), so a module hook does not
  follow the node its server runs on.
- `internal/docker/freezer.go` takes a `ContainerFreezer` seam, but
  `cmd/carbon-panel` wires it to the local client unconditionally.
- Deleting a server still removes the stored data path. Creating a server,
  mod installation, packwiz bake, snapshot create/restore, scheduled backup
  archives, and scheduled modpack sync use the node's mapped storage root.

# How to use this

This concept is a map, not a backlog. The live tracker and
the reliability plan is authoritative for status. If a gap is fixed, update
this file in the same change.

# Read next

- [Remote node fallback](remote-node-fallback.md)
- [Persistence](../architecture/persistence.md)
