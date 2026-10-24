---
type: Invariant
title: Data safety
description: Worlds, databases, backups and snapshots are persistent user data and must survive build, development and cleanup commands.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/snapshot
  - internal/s3
  - internal/worldstore
  - cmd/carbon-storage
sources:
  - title: Contributor guide
    resource: ../../AGENTS.md
  - title: Backup confirmation issue
    resource: ../../docs/issues/013-UI-5-backup-confirmation-dialogs.md
verified: { by: human:athNdev, at: 2026-10-24 }
review_after: 2027-01-23
---

# Rule

A build, development or cleanup command must preserve server worlds, the
controller database, backups and snapshots. A destructive operation must be
explicitly named and separately invoked; it is never a side effect.

# Why

Worlds and databases are the user's data. Re-creating a container is routine;
losing a world is not recoverable by rebuilding the controller.

# How to check

- Look for `remove`, `prune`, `rm`, `clean` or volume deletion in the changed
  path. If it touches a world, backup or database path, stop.
- Backup and restore must be tested; a backup that cannot be restored is not a
  backup.
- Remote archive paths must not silently read the controller filesystem.

# Review

Reviewed `2026-10-24` by `agent:mimo-v2.6-pro` against the code and attested by
`human:athNdev`:

- `make clean` removes only the build binary and states that server data and
  Docker volumes are preserved; `proto-clean` removes generated proto output.
- Snapshot retention prunes only expired snapshots and protects the most recent
  copy (`internal/snapshot/integrity_retention_test.go`).
- Restore coverage exists in `internal/snapshot` and `internal/worldstore`.

The remote archive path gap is tracked in
[known gaps](../failures/known-gaps.md).
