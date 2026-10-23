---
type: Architecture
title: TCP proxy
description: One public TCP port multiplexes Minecraft traffic by inspecting the handshake and routing on hostname.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/proxy
  - pkg/routing
  - web/carbon-panel/src/lib/components/server-routing.svelte
sources:
  - title: Design specification
    resource: ../../DESIGN.md
---

# Shape

The proxy listens on `:25565`, reads the Minecraft handshake packet, extracts
the requested hostname, resolves the route and forwards the connection to the
right container port. SRV records are handled so clients do not need a port.

# Rules

- An unknown or ambiguous host closes the connection. There is no default
  fallback server.
- Route lookup is owned by `pkg/routing`; the proxy does not invent a second
  routing model.
- A route change must be reflected in the console and the proxy without a
  restart where the existing implementation allows it.

# How to check

Exercise the handshake parser and the route registry with tests under
`internal/proxy`; do not validate only by starting a server.

# Read next

- [Runtime model](runtime.md)
- [Repository map](../repo-map.md)
