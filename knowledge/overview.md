---
type: Overview
title: Carbon Panel
description: Single-tenant, self-hosted Minecraft server controller with a Go daemon, SvelteKit console, Docker isolation, SQLite and a TCP proxy.
status: stable
owners: [oss-runtime]
code_refs:
  - cmd/carbon-panel
  - internal
  - web/carbon-panel
sources:
  - title: README
    resource: ../../README.md
  - title: Design specification
    resource: ../../DESIGN.md
---

# What it is

Carbon Panel is a self-hosted controller for Minecraft servers and proxies. One
Go daemon owns the API, the web console, the scheduler, a TCP proxy and a
SQLite database. Game servers run as isolated Docker containers; the controller
may dial remote Docker endpoints as well as the local socket.

# Boundaries

- Single operator, single controller. Multi-tenancy is Carbon Cloud, not this
  repository.
- Persistent user data (worlds, databases, backups) is never disposable.
- The API is proto-first (`proto/carbonpanel/v1`); the frontend and CLI consume
  the same contracts.

# Read next

- [Repository map](repo-map.md)
- [Runtime model](architecture/runtime.md)
- [Data safety](invariants/data-safety.md)
