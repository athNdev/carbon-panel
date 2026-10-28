---
type: Runbook
title: Server lifecycle journal
description: Interrupted container mutations stay locked until the daemon is observed, and an operator can release that lock without deleting containers.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/operation
  - internal/db/server_operation.go
  - cmd/carbon-panel/operations.go
sources:
  - title: Server operation runbook
    resource: ../../docs/runbook-server-operations.md
---

# Use this when

A server lifecycle action must survive a panel crash, or a server is stuck
behind an operation lock.

# Rules

- Persist the intent before calling Docker. If the intent cannot be stored,
  do not perform the mutation.
- A scheduled backup persists that same intent before it pauses world saves
  or writes an archive. An empty request id makes each run its own row. A
  crash leaves the row active until startup recovery observes the server.
  Recovery of that backup says the archive was not finalized and that the
  backup can be run again, then releases the lock. Recovery of a restore, a
  delete, or a migration records the next action and keeps the lock: confirm
  the world, confirm the container and the world, or confirm which node holds
  the world. Container state does not release those three locks. An operator
  resolves one with `-operations resolve` after that check.
- Deleting a server persists a delete intent before modules, the container,
  the panel row, or the world are removed. A server with no container still
  takes that lock. An active backup, restore, or migration row refuses the
  delete and leaves the world in place.
- A start, stop, restart, recreate, boot auto-start, scheduled start, stop,
  or restart, and a deep-sleep or freeze wake all refuse while one of those
  locks is held. A frozen server with no active lock still auto-starts. A
  deeply asleep server still stays asleep across a panel restart.
- A client `request_id` plus the action kind is idempotent for one server.
  Completing a key does not run the mutation again. A failure needs a new key.
- Do not release a lock when the container state cannot be read. Retry the
  read when the daemon returns.
- `-operations resolve` clears the lock and writes a note. It must not stop,
  remove, or prune containers.
- Finished journal rows may be deleted after 30 days. An active lock may not.

# Read next

- [Persistence](../architecture/persistence.md)
- [Runtime model](../architecture/runtime.md)
