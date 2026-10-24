---
type: Architecture
title: RPC contract
description: Proto definitions are the single API contract for the backend, console and CLI.
status: stable
owners: [oss-runtime]
code_refs:
  - proto/carbonpanel/v1
  - internal/rpc
  - buf.gen.yaml
  - web/carbon-panel/src/lib/proto
sources:
  - title: Design specification
    resource: ../../DESIGN.md
---

# Rule

Every frontend or CLI call goes through a service defined in
`proto/carbonpanel/v1`. Do not add an ad-hoc HTTP endpoint for product data.

# Flow

1. Edit the `.proto` file.
2. Regenerate with `make proto` (Docker + buf).
3. Implement the handler in `internal/rpc/handlers` and the service in
   `internal/rpc/services`.
4. Map the RPC to a permission and add negative authorization tests.
5. Consume the generated TypeScript client from
   `web/carbon-panel/src/lib/proto`.

# Invariants

- New RPCs are denied by default until a permission is mapped.
- Request and response types are shared; do not hand-write a parallel DTO.
- Security headers and middleware live in `internal/rpc`, not in handlers.

# Read next

- [Add an RPC](../workflows/add-rpc.md)
- [Fail-closed authorization](../invariants/authz-fail-closed.md)
