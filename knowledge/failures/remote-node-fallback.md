---
type: Failure Mode
title: Remote node fallback
description: Operations that cannot resolve a remote node must error instead of using the controller's Docker socket.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/docker
  - internal/module/hooks.go
  - internal/docker/freezer.go
  - internal/snapshot
sources:
  - title: Fail-closed node resolution change
    resource: https://github.com/athNdev/carbon-panel/pull/225
---

# Symptom

A module, snapshot, sleep or file operation succeeds against the wrong host, or
silently operates on the controller's local Docker socket.

# What was fixed

Module, snapshot and deep-sleep helpers now return an error when the target node
cannot be resolved, and a module records the server's node ID.

# Remaining exposure

- `internal/docker/freezer.go` still targets the local client unconditionally.
- `internal/module/hooks.go:execInModule` still uses the manager's client.
- Scheduled modpack sync still copies into the stored data path. Snapshot
  create and restore still archive that stored path. Deleting a server still
  removes that stored path. Creating a server, mod installation, packwiz
  bake, and scheduled backup archives use the node's mapped storage root.

# Detection

Add a test with a server whose node cannot be resolved. The operation must
return an error and must not touch the local socket.

# Read next

- [Node resolution](../architecture/node-resolution.md)
- [Known gaps](known-gaps.md)
