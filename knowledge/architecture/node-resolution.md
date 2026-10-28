---
type: Architecture
title: Node resolution
description: Every operation must resolve the Docker client for the target node and fail closed when it cannot.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/docker
  - internal/module
  - internal/snapshot
  - internal/docker/freezer.go
  - internal/metrics
  - internal/nodestorage
sources:
  - title: Fail-closed node resolution change
    resource: https://github.com/athNdev/carbon-panel/pull/225
---

# Rule

A server belongs to a node. An operation that targets a remote node must obtain
that node's Docker client or return an error. It must never fall back to the
controller's local Docker socket.

# Why

A silent fallback can run a module, snapshot or sleep operation against the
wrong host. That is a data-safety bug, not a degraded mode.

# Current state

- Module, snapshot and deep-sleep helpers were made fail-closed and record the
  server's node ID.
- `internal/docker/freezer.go` still targets the local client unconditionally.
- `internal/module/hooks.go:execInModule` still uses the manager's client.
- Disk usage measures the node's mapped world root. A remote node without
  reachable storage is left unmeasured.
- File read, update, delete, and rename resolve that mapped root before
  checking the path. A controller copy of the data path is not consulted.
  The latest-log upload uses that same root.
- Creating a server writes the world directory on that mapped root and copies
  a manual modpack archive there. A remote node with no reachable storage is
  refused, so no controller-local world directory is created.
- Scheduled backup archives resolve the mapped root before reading the world.
- Mod installation and packwiz bake write the mods folder on that mapped
  root. A remote node with no reachable storage is refused, and a controller
  copy is not treated as the installed set.
- Snapshot create and restore archive that mapped root. A remote node with
  no reachable storage is refused before a controller copy is archived.
- Scheduled modpack sync clones and copies onto that mapped root. A remote
  node with no reachable storage is refused before a controller directory
  is created.
- Deleting a server removes the world on the node's mapped storage root
  before the panel row. A remote node with no reachable storage is refused,
  and a controller-local directory of the same name is left in place.

# How to check

For any new operation, follow the path from the server record to the Docker
client. Add a test where the node cannot be resolved and assert an error.

# Read next

- [Failure modes](../failures/remote-node-fallback.md)
- [Module system](module-system.md)
