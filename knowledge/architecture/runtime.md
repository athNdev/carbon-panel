---
type: Architecture
title: Runtime model
description: The daemon owns the API, console, proxy, scheduler and SQLite, and starts them from cmd/carbon-panel.
status: stable
owners: [oss-runtime]
code_refs:
  - cmd/carbon-panel
  - internal/rpc
  - internal/proxy
  - internal/db
  - internal/docker
  - internal/scheduler
  - internal/operation
  - internal/reconciler
  - web/carbon-panel
sources:
  - title: Design specification
    resource: ../../DESIGN.md
---

# Shape

`cmd/carbon-panel/main.go` loads configuration, opens the SQLite database,
registers the RPC services, starts the TCP proxy and starts the scheduler,
reconciler and WebSocket hub. The console is a SvelteKit build embedded into the
binary. Interrupted server mutations are journaled and recovered on startup.
The Docker event supervisor refreshes its node set while it runs.

# Listeners

- `:8080` — Connect-RPC, HTTP and the WebSocket hub.
- `:25565` — the public TCP proxy. See [proxy](proxy.md).

# Build boundary

The backend tests expect `web/carbon-panel/build` to exist; CI creates a
placeholder when the frontend is not built. Production builds run
`bash scripts/web-build.sh` first.

# Docker boundary

The controller holds a Docker client for the local socket or for a remote node.
Never assume it is local; see [node resolution](node-resolution.md).

A Minecraft server image is a tag on `itzg/minecraft-server`, or a full
reference to `itzg/minecraft-server` or `itzg/minecraft-bedrock-server`.
Other repositories are rejected. A tag stays a tag in the database so the
image picker still matches it. An image that is already on the daemon is
used without a registry check. Concurrent pulls of the same reference on
one client share one request, and a pull is success only after the daemon
reports that image present. The image ID of the container that last came
up stays recorded when a later pull or recreate fails. A tag change is
written to the activity log with that ID and whether the running
container moved to the new image.

# How to check

```sh
go build ./...
go vet ./...
go test ./...
```

# Read next

- [RPC contract](rpc-contract.md)
- [Persistence](persistence.md)
- [Scheduler](scheduler.md)
