---
type: Architecture
title: Testing seams
description: How failure and recovery behavior is tested without a live Minecraft server or a reachable Docker daemon.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/docker
  - internal/db
  - .github/workflows/ci.yml
  - Makefile
sources:
  - title: Contributor guide
    resource: ../../AGENTS.md
---

# Shape

Tests are ordinary Go package tests. Docker-facing code sits behind seams, for
example `ContainerFreezer` in `internal/docker`, so a test can substitute a
deterministic failure instead of a daemon. `internal/db` tests run against a
temporary test store rather than the controller's database file.

CI runs the suite, the race detector, and a Windows compile pass that builds
every package without executing the Windows binaries.

# Use it when

- A change touches recovery, retries, timeouts or partial failures.
- A Docker operation can fail after a side effect.
- A test needs a deterministic error instead of a real daemon.

# Rules

- Test the failure path at the closest integration seam, not only the happy
  path.
- Do not require a live Minecraft server for a unit-level failure test.
- If a failure cannot be reproduced in a test, say so in the change.

# Read next

- [Verification](verification.md)
- [Scheduler](../architecture/scheduler.md)
