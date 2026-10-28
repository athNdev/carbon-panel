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
- When a chain stops at a step, that execution names the step and says the
  remaining steps were not run.
- A console command runs once. A configured retry is not repeated, because
  the command may already have been delivered.
- A scheduled backup holds the server lifecycle transition until the archive
  finishes. Restore and wake wait, and a backup refuses to start while that
  transition is already held.
- A scheduled backup writes a journal row before it pauses world saves or
  creates the archive. Each run uses an empty request id, so the row belongs
  to that run. A crash leaves the row active until startup recovery observes
  the server. Recovery says the archive was not finalized and that the backup
  can be run again. The backup does not start when that server already has an
  active journal row.
- A task execution left in the running state across a panel restart is marked
  failed. The recorded reason says to run the task again after the server is
  idle.
- A scheduled stop and a live modpack file sync take that same transition.
  The sync releases it before a restart claims it again.
- Validate cron expressions before persisting them.

# How to check

Use the scheduler and task tests; for recovery behavior, use the fault harness
described in [testing](../testing/harness.md).

# Read next

- [Runtime model](runtime.md)
- [Testing harness](../testing/harness.md)
