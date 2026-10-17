# SCH-7: Validate Cron Expressions at Task Creation and Update Time

**Status:** Completed & Verified  
**Priority:** Medium  
**Category:** Scheduler / Reliability  
**Component:** `internal/scheduler/scheduler.go`, `internal/rpc/services/task.go`  

## Problem Description
Previously, invalid cron expressions could be submitted when creating or updating scheduled tasks. The task would be stored in the database, only to silently fail at runtime when the cron parser failed to calculate the next execution time.

## Implementation Details
1. In `internal/scheduler/scheduler.go`:
   - Configured `cron.Parser` with standard fields (`cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow`).
   - Exposed `ValidateCronExpr(expr string) error`.
   - `CalculateNextRun()` validates syntax and returns an error for invalid cron expressions.
2. In `internal/rpc/services/task.go`:
   - `CreateTask()` calls `s.scheduler.CalculateNextRun(task)` which checks `cron.Parse()` and returns `connect.CodeInvalidArgument` on error.
   - `UpdateTask()` explicitly checks `s.scheduler.ValidateCronExpr(task.CronExpr)` for cron-scheduled tasks and returns `connect.CodeInvalidArgument` if invalid.
