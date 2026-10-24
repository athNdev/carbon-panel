---
type: Architecture
title: Module system
description: Modules, hooks, packwiz and indexers deliver server content and modpacks.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/module
  - internal/packwiz
  - internal/indexers
  - internal/blueprint
  - internal/module/hooks.go
sources:
  - title: Module delivery checksum issue
    resource: ../../docs/issues/003-MOD-4-modpack-delivery-checksum.md
  - title: Indexer error surfacing issue
    resource: ../../docs/issues/004-MOD-2-surface-indexer-errors.md
---

# Shape

Modules are server-side content units. Hooks run inside the server's runtime;
packwiz and the indexers resolve CurseForge and Modrinth content; blueprints
describe a server's desired configuration.

# Rules

- Every module operation must use the Docker client resolved for the target
  server. Do not use the manager's client directly.
- Surface indexer and delivery failures; do not turn a partial download into a
  successful install.
- Verify checksums before declaring a modpack delivered.
- A hook failure must be attributable to the module and the server.

# Known gap

`internal/module/hooks.go:execInModule` still uses the manager's client
directly. It is tracked with the other node-resolution gaps in
[failure modes](../failures/remote-node-fallback.md).

# Read next

- [Node resolution](node-resolution.md)
- [Known failure modes](../failures/remote-node-fallback.md)
