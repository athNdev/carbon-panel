# DEP-3 / DEP-4: Snapshot Integrity Verification, Concurrency Transition Guard & Retention Policy

**Status:** Completed & Verified  
**Priority:** High  
**Category:** Reliability & Data Safety  
**Components:** `internal/snapshot/snapshot.go`, `internal/config/config.go`, `internal/scheduler/scheduler.go`

## Problem Description
1. **Unchecked Snapshot Extraction**: Pre-update snapshots were extracted over existing server data without verifying ZIP archive integrity or checksums. If an archive was corrupted or truncated on disk, `Rollback` would begin extraction, leaving server files in a partially extracted, corrupt state.
2. **Missing Lifecycle Concurrency Guard**: Unlike backup restore and power operations, snapshot `Rollback` did not acquire `docker.TryBeginServerTransition(server.ID)`. This allowed concurrent operations (e.g. reconciler drift recovery, user power actions, scheduled tasks) to race with an in-progress rollback.
3. **No Snapshot Retention Policy (DEP-3)**: Pre-update snapshots had no age-based retention expiration and were only pruned count-wise immediately after a snapshot creation event. Servers without frequent updates or orphaned snapshots accumulated indefinitely, risking disk exhaustion.
4. **Missing Checksum Sidecar & Cleanup**: Snapshots lacked `.sha256` sidecars, preventing tamper and bitrot detection. Furthermore, snapshot deletions did not clean up sidecars if present.

## Implementation Details
1. **Archive Integrity Verification**:
   - `CreatePreUpdateSnapshot`: Tests zip integrity using `files.VerifyZipArchive(archivePath)` immediately following creation. Computes SHA-256 and writes `${archivePath}.sha256` alongside the archive.
   - `Rollback`: Verifies SHA-256 checksum against `${archivePath}.sha256` (if present) and tests zip integrity via `files.VerifyZipArchive` before container stoppage or extraction.
2. **Lifecycle Concurrency Lock**:
   - `Rollback` guards execution with `docker.TryBeginServerTransition(server.ID)` and releases it on exit via `defer docker.EndServerTransition(server.ID)`.
3. **Flexible Retention Policy**:
   - Added `MaxSnapshots` (default: 5) and `SnapshotRetentionDays` (default: 0 = disabled) to `StorageConfig` in `internal/config/config.go`.
   - Implemented `PruneSnapshots(ctx context.Context, serverID string)` in `internal/snapshot/snapshot.go` supporting both count and age retention while guaranteeing at least one newest snapshot is always preserved.
   - Implemented `PruneAllSnapshots(ctx context.Context)` and integrated it into the daily background maintenance cycle of `internal/scheduler/scheduler.go`.
   - `DeleteSnapshot` and `PruneSnapshots` clean up both the archive `.zip` and `.sha256` sidecar.
4. **Verification & Tests**:
   - Added `internal/snapshot/integrity_retention_test.go` with tests:
     - `TestSnapshot_IntegrityVerificationAndChecksum`: verifies archive corruption and hash mismatch abort rollback without touching files, and verifies sidecar cleanup on delete.
     - `TestSnapshot_Rollback_LifecycleConcurrencyGuard`: verifies rollback blocks when transition lock is held.
     - `TestSnapshot_RetentionByAgeAndPreserveLatest`: verifies age-based pruning and latest snapshot protection.
     - `TestSnapshot_PruneAllSnapshots`: verifies multi-server pruning sweeps.
   - Added validation tests in `internal/config/config_test.go` ensuring negative values for snapshot retention configurations are rejected.
