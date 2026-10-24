---
type: Overview
title: Repository map
description: Top-level directories and the subsystem each one owns.
status: stable
owners: [oss-runtime]
code_refs: [cmd, internal, pkg, proto, web, docs, docker, scripts, oidc]
sources:
  - title: Design specification
    resource: ../../DESIGN.md
---

# Layout

| Path | Owns |
| --- | --- |
| `cmd/carbon-panel` | Daemon entrypoint, lifecycle, embedded frontend |
| `cmd/carbon-fault-harness` | Fault-injection harness for tests |
| `cmd/carbon-storage` | Storage utility; not yet wired into production |
| `cmd/geyser`, `cmd/status` | Auxiliary binaries |
| `internal` | Controllers and subsystems (see the architecture concepts) |
| `pkg` | Public shared packages (`mcstatus`, `routing`) also used by Cloud |
| `proto/carbonpanel/v1` | Connect-RPC contracts |
| `web/carbon-panel` | SvelteKit 2 / Svelte 5 console |
| `docs` | ADRs, plans, research, runbooks and issue write-ups |
| `docker`, `scripts`, `oidc` | Deployment assets, developer scripts, OIDC examples |

# Rule

Do not add a top-level directory without a concept that names its owner and
boundary. If a directory exists only for one subsystem, document that subsystem
under `architecture/` instead.

# Read next

- [Runtime model](architecture/runtime.md)
- [RPC contract](architecture/rpc-contract.md)
- [Testing and verification](testing/verification.md)
