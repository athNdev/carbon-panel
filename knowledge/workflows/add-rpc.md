---
type: Workflow
title: Add an RPC
description: The order of changes for a new API method, from proto to permission mapping and console client.
status: stable
owners: [oss-runtime]
code_refs:
  - proto/carbonpanel/v1
  - internal/rpc
  - web/carbon-panel/src/lib/proto
---

# Steps

1. Add the method and messages to the relevant `.proto` file in
   `proto/carbonpanel/v1`.
2. Run `make proto` to regenerate Go and TypeScript code.
3. Implement the handler in `internal/rpc/handlers` and the service in
   `internal/rpc/services`.
4. Map the method to a permission in the RBAC layer. A new method is denied by
   default until this exists.
5. Add a positive test and a negative authorization test.
6. Consume the generated TypeScript client in the console. Do not add a
   parallel DTO or an ad-hoc fetch.
7. Run `go test ./...`, `go vet ./...`, and the frontend checks from
   [testing](../testing/verification.md).

# Stop conditions

- The proto change is breaking and there is no compatibility plan.
- The method exposes server files, backups, console output or user
  administration without a permission mapping.
- Only the UI hides the action; the API accepts it.

# Read next

- [RPC contract](../architecture/rpc-contract.md)
- [Fail-closed authorization](../invariants/authz-fail-closed.md)
