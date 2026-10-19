# SCH-1: Staged Config Rollout Rollback on Container Recreation Failure

**Status:** Completed & Verified  
**Priority:** High  
**Category:** Reliability & Robustness  
**Components:** `internal/db/models.go`, `internal/db/staged_config.go`, `internal/rpc/handlers/staged_config.go`, `internal/scheduler/staged_config.go`, `internal/scheduler/scheduler.go`, `internal/rpc/services/server.go`

## Problem Description
1. **Irreversible Config Application**: When staged configuration changes were applied (either automatically via `on_restart` or scheduled via cron ticks), the live `ServerConfig` was merged with the diff and the record was updated to `applied`.
2. **Missing State Snapshot**: Prior to this change, no snapshot of modified fields was retained in the database record. If the modified config contained invalid values causing Docker container recreation or start failure, or if an operator needed to back out an applied change, there was no recorded state to revert to.
3. **Broken Container Recovery**: When container recreation (`dockerCli.RecreateContainer`) or startup failed after staged config application, the server remained in an `error` state while the database held the broken configuration, preventing subsequent clean restarts and recovery.
4. **No Reversal API**: Operators had no endpoint to roll back an applied staged change without manually re-constructing and staging another inverted diff.

## Implementation Details
1. **Data Model (`internal/db/models.go`)**:
   - Added `StagedStatusRolledBack StagedStatus = "rolled_back"` to the enum.
   - Added `PreviousPayload string` to `StagedConfigChange` to snapshot the previous values of every touched key prior to merging.
   - Added `RolledBackAt *time.Time` to record when the rollback occurred.
2. **Snapshot on Apply (`internal/db/staged_config.go`)**:
   - In `Store.ApplyStagedConfigChange`, before merging the incoming diff into the live configuration, `prevDiff` captures the original values for each key in `diff`.
   - Serialized into `PreviousPayload` transactionally with the merge.
3. **Rollback Implementation (`internal/db/staged_config.go`)**:
   - Added `Store.RollbackStagedConfigChange(ctx context.Context, id string) (*StagedConfigChange, error)`.
   - Uses atomic claim pattern (`status = 'applied' -> 'rolling_back'`) within a database transaction.
   - Restores the original field values from `PreviousPayload` back into the live `ServerConfig` and marks the change as `rolled_back` with `RolledBackAt`.
   - Blocks double-rollback and non-applied records cleanly.
4. **HTTP Rollback Route (`internal/rpc/handlers/staged_config.go`)**:
   - Added `POST /api/v1/staged-config/{serverID}/staged/{id}/rollback` endpoint.
   - Returns 200 OK with the updated change record, or 400 Bad Request if the change is not eligible for rollback.
5. **Auto-Rollback on Recreate/Start Failure**:
   - In `internal/scheduler/scheduler.go` (`recreateForPendingStagedConfig`), if `RecreateContainer` or `StartContainer` fails, newly applied staged config records are automatically rolled back.
   - In `internal/rpc/services/server.go` (`StartServer` and `RestartServer`), newly applied staged records are automatically rolled back if Docker recreation or container startup fails.
6. **Verification & Tests**:
   - `internal/db/staged_config_test.go`: Added `TestStagedConfigChange_Rollback` validating snapshot capture, rollback restoration, status assertions, non-applied rejection, and corrupt/missing payload handling.
   - `internal/scheduler/staged_config_test.go`: Added `TestRecreateForPendingStagedConfigRollbackOnStartFailure` and updated partial recreation failure tests to ensure `store.rolledBack` captures affected staged changes.
   - `internal/rpc/handlers/staged_config_test.go`: Added full HTTP lifecycle tests for stage -> apply -> rollback -> duplicate rollback rejection.
