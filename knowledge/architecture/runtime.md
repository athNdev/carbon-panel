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
The Docker event supervisor refreshes its node set while it runs, including
each node's endpoint generation. A changed generation replaces that node's
watcher. A node whose client cannot
be resolved is stale and does not stop the others. A full event buffer drops
the event instead of stalling that node's stream. The periodic sweep reads
the container again, so a dropped or reordered event does not leave status,
the proxy route, or log subscriptions stuck.

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
