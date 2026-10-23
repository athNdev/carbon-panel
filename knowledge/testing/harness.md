---
type: Architecture
title: Testing and fault harness
description: The integration seams used to test failure behavior without a live Minecraft server.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/testharness
  - cmd/carbon-fault-harness
  - cmd/carbon-storage
  - docs/testing/oss-fault-harness.md
sources:
  - title: OSS fault harness
    resource: ../../docs/testing/oss-fault-harness.md
---

# Shape

`internal/testharness` provides reusable test seams. `cmd/carbon-fault-harness`
injects Docker and storage failures; `cmd/carbon-storage` exercises the storage
utility. The harness is not a production binary.

# Use it when

- A change touches recovery, retries, timeouts or partial failures.
- A Docker operation can fail after a side effect.
- A test needs a deterministic error instead of a real daemon.

# Rules

- Test the failure path at the closest integration seam, not only the happy
  path.
- Do not require a live Minecraft server for a unit-level failure test.
- If the harness cannot reproduce the failure, say so in the change.

# Read next

- [Verification](verification.md)
- [Scheduler](../architecture/scheduler.md)
