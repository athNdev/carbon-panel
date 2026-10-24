---
type: Runbook
title: Deep sleep and wake
description: Where the hibernation behavior is specified and which code path still needs a node resolver.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/docker/freezer.go
  - docs/runbook-sleep-baseline.md
  - docs/deep-sleep-tier-b.md
sources:
  - title: Sleep baseline runbook
    resource: ../../docs/runbook-sleep-baseline.md
  - title: Deep sleep tier B
    resource: ../../docs/deep-sleep-tier-b.md
---

# Use this when

A server should hibernate or wake, or a sleep-related change is being reviewed.

# Procedure

Follow `docs/runbook-sleep-baseline.md` for the baseline and
`docs/deep-sleep-tier-b.md` for the tier-B behavior. Do not infer the state
machine from a single container inspect; read the recorded state.

# Known gap

`internal/docker/freezer.go` still targets the local Docker client
unconditionally. It needs the same fail-closed resolver as the module and
snapshot paths. See [node resolution](../architecture/node-resolution.md).

# Read next

- [Remote node fallback](../failures/remote-node-fallback.md)
- [Data safety](../invariants/data-safety.md)
