---
type: Invariant
title: Fail-closed authorization
description: New RPCs and resource scopes are denied until an explicit permission mapping and negative test exist.
status: draft
owners: [oss-runtime]
code_refs:
  - internal/auth
  - internal/rbac
  - internal/rpc
sources:
  - title: Contributor guide
    resource: ../../AGENTS.md
review_after: 2027-01-23
---

# Rule

Authorization fails closed. A new RPC, resource scope or administrative action
must be mapped to a permission and covered by at least one negative test before
it is usable.

# Why

An unmapped endpoint that defaults to allow is a privilege escalation. The
controller exposes server lifecycle, files, backups and user administration.

# How to check

- Find the permission mapping for the RPC; if there is none, the change is
  incomplete.
- Add a test for an actor without the permission and assert denial.
- Do not rely on the UI hiding an action. The API is the boundary.

# Read next

- [Add an RPC](../workflows/add-rpc.md)
- [RPC contract](../architecture/rpc-contract.md)
