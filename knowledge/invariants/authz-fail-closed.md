---
type: Invariant
title: Fail-closed authorization
description: Unmapped RPCs and resource scopes are denied; a usable RPC needs an explicit permission mapping and a negative test.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/auth
  - internal/rbac
  - internal/rpc
sources:
  - title: Contributor guide
    resource: ../../AGENTS.md
verified: { by: agent:mimo-v2.6-pro, at: 2026-10-24 }
review_after: 2027-01-23
---

# Rule

Authorization fails closed. A new RPC, resource scope or administrative action
must be mapped to a permission and covered by at least one negative test before
it is usable. Two bounded authorization paths are allowed and must stay tested:
subuser grants for server-scoped procedures, and legacy download sessions
without a server ID, which resolve against the global scope.

# Why

An unmapped endpoint that defaults to allow is a privilege escalation. The
controller exposes server lifecycle, files, backups and user administration.

# How to check

- Find the permission mapping for the RPC; if there is none, the change is
  incomplete.
- Add a test for an actor without the permission and assert denial.
- Do not rely on the UI hiding an action. The API is the boundary.

# Review

Verified `2026-10-24` by `agent:mimo-v2.6-pro` against the code:

- `internal/rpc/server.go` denies a procedure with no `rbac.ProcedurePermissions`
  entry and denies the request when the RBAC enforcer is unavailable.
- `internal/rbac/mapping_scope_test.go` covers the mapping, and
  `internal/rpc/server_test.go` covers denial.

Human attestation is still pending.

# Read next

- [Add an RPC](../workflows/add-rpc.md)
- [RPC contract](../architecture/rpc-contract.md)
