---
type: Domain Term
title: Glossary
description: OSS terms whose meaning is narrower than everyday usage.
status: stable
owners: [oss-runtime]
sources:
  - title: Design specification
    resource: ../../DESIGN.md
---

# Terms

**Server** — a managed Minecraft or proxy instance and its world/config, not the
container that currently runs it.

**Node** — a Docker endpoint the controller can reach. It may be the local
socket or a remote daemon.

**Module** — a server-side content unit delivered into a server's runtime.

**Proxy** — the TCP ingress that routes Minecraft handshakes to server
containers. Not the managed Cloud control plane.

**Blueprint** — a description of a server's desired configuration.

**Packwiz** — the modpack format and toolchain used to resolve and deliver mods.

**Snapshot** — a point-in-time copy used for restore. Not a Docker image.

**World** — persistent Minecraft map data. It outlives containers.

**Staged update** — a config diff or content edit on a server's one open batch.
Recording it does not restart the server or write the live tree.

# Boundary

Carbon Cloud has additional terms (hosting instance, allocation, worker node).
They live in the Cloud repository, not here.
