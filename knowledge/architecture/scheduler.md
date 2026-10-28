---
type: Architecture
title: Scheduler
description: Cron and lifecycle tasks run in the controller and surface progress over the WebSocket hub.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/scheduler
  - internal/command
  - internal/events
  - internal/ws
sources:
  - title: Cron validation issue
    resource: ../../docs/issues/002-SCH-7-cron-validation.md
  - title: Task recovery issue
    resource: ../../docs/issues/014-SCH-8-task-execution-recovery-ws-reconnect.md
---

# Shape

The scheduler owns cron expressions and lifecycle actions such as start, stop,
restart and backup. Long-running work is a task; task state and logs are
streamed through the WebSocket hub.

# Rules

- A scheduled action that destroys or replaces data must be explicitly
  configured and separately named. It is never implied by a cleanup rule.
- A failed task is recorded and visible; it must not be silently retried into a
  success state.
- On startup, recover or fail tasks that were interrupted by a restart. Do not
  leave them "running" forever.
- A task is not claimed when the running-execution check fails. The due run
  stays due, and a manual trigger returns that error. An unknown check is
  treated as running.
- Cron runs in the controller's local time. A missed slot is not replayed:
  one due check runs the task once and stores the next future time.
- Validate cron expressions before persisting them.

# How to check

Use the scheduler and task tests; for recovery behavior, use the fault harness
described in [testing](../testing/harness.md).

# Read next

- [Runtime model](runtime.md)
- [Testing harness](../testing/harness.md)
