---
type: Invariant
title: Container privileges
description: Container hardening must stay the baseline; privileges are broadened only through a documented, tested operator override.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/docker
  - docker
sources:
  - title: Contributor guide
    resource: ../../AGENTS.md
verified: { by: agent:mimo-v2.6-pro, at: 2026-10-24 }
review_after: 2027-01-23
---

# Rule

Do not add `privileged`, broad capabilities, host mounts or a writable Docker
socket to a game container outside an explicitly documented and tested operator
override. The default hardening must remain the baseline, and any change must
test the effective configuration after overrides are applied.

# Why

Each Minecraft server is an isolation boundary. A container that can reach the
host or the Docker socket can escape that boundary.

# How to check

- Inspect the effective config after overrides, not only the defaults.
- A new mount needs a path and a reason; a new capability needs a named
  requirement.
- Do not copy a hardening exemption from one image to another.

# Review

Verified `2026-10-24` by `agent:mimo-v2.6-pro` against the code:

- `applyContainerHardening` in `internal/docker/client.go` sets `CapDrop ALL`,
  the Minecraft bootstrap capabilities and `no-new-privileges`.
- `ApplyOverrides` applies operator `privileged`, `CapAdd`, `CapDrop`,
  `SecurityOpt` and mount overrides without validation, so this rule is a
  change policy, not a runtime bound.

A validation bound on operator overrides is a known gap and would strengthen
this invariant. Human attestation is still pending.
