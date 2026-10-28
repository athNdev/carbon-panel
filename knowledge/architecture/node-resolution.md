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
- A cached Docker client is dropped when that node's host, TLS material, or
  endpoint generation changes. The next lookup builds a client for the current
  endpoint. A client registered without a recorded endpoint is left in place.
- Creating a node starts its endpoint generation at 1. Changing the host or
  TLS material increases that generation. A lifecycle operation records the
  generation and the accepted Docker daemon id it observed.
- The first successful daemon observation stores that daemon id. A later
  observation with a different id is refused, the cached client is dropped,
  and the accepted id stays in place until an operator update replaces it.
- `GetClient` does not serve a named node from the local daemon, including
  when the historical fallback flag is set. Local aliases still use the
  controller client.
- `internal/docker/freezer.go` still targets the local client unconditionally.
- `internal/module/hooks.go:execInModule` still uses the manager's client.
- Remote file, Minecraft and backup archive paths still read the controller
  filesystem; only restore refuses. This is tracked as the remote archive path gap.

# How to check

For any new operation, follow the path from the server record to the Docker
client. Add a test where the node cannot be resolved and assert an error.

# Read next

- [Failure modes](../failures/remote-node-fallback.md)
- [Module system](module-system.md)
