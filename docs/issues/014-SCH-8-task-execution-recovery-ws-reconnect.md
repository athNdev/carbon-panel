# Issue 014: SCH-8 - Task Execution Restart Recovery and WebSocket Console Resilience

## Context & Problem
Two related reliability and operational observability gaps were identified across backend task management and the frontend server console:

1. **Orphaned Task Executions on Backend Restart**:
   - When a task execution starts, an entry is written to SQLite with `status = 'running'`.
   - If the daemon crashes, restarts, or the host reboots while an execution is in progress, the database record was permanently left in the `running` state with `ended_at = nil`.
   - Operators saw tasks appearing to run indefinitely. Furthermore, calling `CancelExecution` returned an error (`"execution not found or already finished"`) because the in-memory goroutine was gone, preventing operators from ever clearing or resolving the orphaned execution.

2. **WebSocket Console Reconnection Exhaustion & Log Freezing (P2-10)**:
   - In `web/carbon-panel/src/lib/stores/websocket.svelte.ts`, `scheduleReconnect()` gave up after 5 exponential attempts (~31s) and marked the state as permanently disconnected with `'Unable to connect. Please refresh the page.'`.
   - If the server took longer than 35s to restart, or under network flapping, the console became permanently dead until the user reloaded the page, losing scroll and terminal state.
   - Additionally, while disconnected, log entries stopped arriving completely because no fallback polling occurred for live logs.

## Solution

### Backend
1. **Startup Recovery of Orphaned Executions**:
   - Added `FailOrphanedTaskExecutions(ctx context.Context, reason string) (int64, error)` to `internal/db/store.go`.
   - On `Scheduler.Start()`, queries all executions in `status = 'running'` and transitions them to `status = 'failed'` with `error = "interrupted by panel restart"` and `ended_at = time.Now()`.
2. **Orphan Cancellation Support**:
   - In `Scheduler.CancelExecution(executionID string)`, if the execution is not found in the active memory map (`runningExecutions`), the scheduler checks the store. If the record exists in `status = 'running'`, it cleanly updates it to `status = 'cancelled'` with `error = "execution cancelled"` and sets `ended_at = time.Now()`.
3. **Unit Tests**:
   - Added `TestStore_FailOrphanedTaskExecutions` in `internal/db/executions_test.go`.
   - Added `TestScheduler_StartRecoversOrphanedExecutions` and `TestScheduler_CancelExecution_OrphanedInStore` in `internal/scheduler/scheduler_test.go`.

### Frontend
1. **Continuous Background WebSocket Retries & Manual Reconnect**:
   - In `web/carbon-panel/src/lib/stores/websocket.svelte.ts`, updated `scheduleReconnect()` so that after 5 rapid retries, it transitions to a steady 30-second background retry interval rather than abandoning the connection.
   - Added `reconnect(): void` to `WebSocketClient`, resetting attempt counters and triggering an immediate connection attempt.
2. **Accessible Console UI & Fallback Log Polling**:
   - In `web/carbon-panel/src/lib/components/server-console.svelte`, rendered a visible, accessible "Reconnect" button next to the connection indicator when disconnected.
   - Added fallback log polling (`fetchLogs()` every 5 seconds) while the console tab is active and WebSocket is disconnected, ensuring logs continue updating during transient socket interruptions. Fallback polling automatically stops when WebSocket authenticates or the component unmounts.
3. **Automated Verification**:
   - Added tests in `web/carbon-panel/src/lib/utils/a11y-audit.test.ts` verifying reconnect actions, accessibility labels, and fallback polling cleanup.
