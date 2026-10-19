<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->


# SEC-3 & SCH-5: Audit Logging for Privileged Overrides & Multi-Trigger Task Deduplication Concurrency

**Status:** Completed & Verified  
**Priority:** High  
**Category:** Security, Reliability & Robustness  
**Components:** `internal/activity/activity.go`, `internal/docker/hardening.go`, `internal/rpc/services/server.go`, `internal/db/store.go`, `internal/scheduler/scheduler.go`

## Problem Description

1. **Un-Audited Elevated Privileges (`SEC-3`)**:
   - While elevated RBAC permissions (`ResourceServers/ActionManageDockerPrivileged`) gate the ability to configure dangerous Docker overrides (`Privileged: true`, `CapAdd`, dangerous `SecurityOpt`), granting or modifying these elevated capabilities was not recorded in the immutable activity audit log (`storage.ActivityLog`).
   - Server lifecycle modifications (`server.create`, `server.update`, `server.delete`) were also missing from the audit log, leaving operators without an immutable trail of who provisioned, modified, or destroyed servers and who attached host-level escape capabilities to containers.

2. **Multi-Trigger Task Concurrency Race (`SCH-5`)**:
   - The scheduler previously tracked running tasks using an in-memory map (`s.runningTasks`) guarded during scheduled checks (`checkAndRunDueTasks`).
   - However, manual triggers (`TriggerTask`) checked `s.IsTaskRunning(taskID)` without actually adding the task to `s.runningTasks` or locking during execution, allowing concurrent manual triggers to run the same task multiple times simultaneously.
   - Event triggers (`HandleServerEvent`) fired tasks asynchronously via the event bus without checking or setting `s.runningTasks` at all, resulting in duplicate dispatches if server lifecycle events occurred rapidly while a task was already executing.
   - Neither path consulted the database for active running task executions (`ExecutionStatusRunning`), meaning that if a task execution was in flight across scheduler instances or restarted daemons, new executions were dispatched concurrently.

## Implementation Details

1. **Privileged Override Extraction & Audit Events (`SEC-3`)**:
   - Added event types `EventServerUpdate = "server.update"` and `EventServerPrivilegedOverrides = "server.privileged_overrides"` to `internal/activity/activity.go`.
   - Added `docker.PrivilegedOverrideFields(overrides *v1.DockerOverrides) map[string]any` in `internal/docker/hardening.go` extracting dangerous override keys (`privileged`, `cap_add`, and dangerous `security_opt`).
   - Implemented `auditServerLifecycle(ctx, ip, event, serverID)` and `auditPrivilegedOverrides(ctx, ip, serverID, overrides)` in `internal/rpc/services/server.go`.
   - Integrated lifecycle auditing (`server.create`, `server.update`, `server.delete`) and privileged override auditing across `CreateServer`, `UpdateServer`, and `DeleteServer`.
   - Overrides audit record stores the actor ID, actor username, client IP, server ID, and JSON-encoded properties with action `grant_docker_privileged_overrides` and specific override fields.

2. **Deduplication Concurrency & Running Execution Checking (`SCH-5`)**:
   - Added `Store.HasRunningExecution(ctx context.Context, taskID string) (bool, error)` in `internal/db/store.go` to query whether any active execution for the task is in `ExecutionStatusRunning`.
   - Added `Scheduler.tryClaimTask(ctx context.Context, taskID string) bool` and `Scheduler.releaseTask(taskID string)` in `internal/scheduler/scheduler.go`.
     - Atomically acquires the scheduler lock, checks `s.runningTasks`, checks `s.store.HasRunningExecution`, and registers in-memory execution claim.
   - Updated `checkAndRunDueTasks` to claim and release tasks through `tryClaimTask` / `releaseTask`.
   - Updated `TriggerTask` to claim via `tryClaimTask`, defer `releaseTask`, and return `ErrTaskAlreadyRunning` on conflicting executions.
   - Updated `HandleServerEvent` to claim via `tryClaimTask` before spawning goroutines, preventing duplicate event-triggered executions.
   - Updated `IsTaskRunning(taskID string) bool` to check both the in-memory map and database for active running executions.

3. **Verification & Tests**:
   - `internal/docker/hardening_test.go`: Added `TestPrivilegedOverrideFields` verifying extraction of dangerous flags and omission of benign flags.
   - `internal/rpc/services/server_docker_privilege_test.go`:
     - Added `TestCreateServer_PrivilegedOverrides_AuditLogRecorded` verifying `server.create` and `server.privileged_overrides` activity logs with user and capability details.
     - Added `TestCreateServer_BenignOverrides_NoPrivilegedAuditLog` verifying benign overrides do not emit `server.privileged_overrides`.
     - Added `TestUpdateServer_PrivilegedOverrides_AuditLogRecorded` verifying updates with privileged overrides record audit logs upon persistence.
   - `internal/db/executions_test.go`: Added `TestStore_HasRunningExecution` testing running execution queries across creation and completion.
   - `internal/scheduler/trigger_test.go`:
     - Added `TestTriggerTask_RejectsWhenDatabaseRecordsRunningExecution` verifying conflict detection against DB records.
     - Added `TestHandleServerEvent_DeduplicatesConcurrently` verifying that event triggers skip duplicate execution when a task is already running.
   - All internal test suites verified passing with race detector (`go test -race ./internal/...`).
