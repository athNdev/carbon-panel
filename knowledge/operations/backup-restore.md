---
type: Runbook
title: Backup and restore
description: Where backup storage, snapshots and restore are specified.
status: stable
owners: [oss-runtime]
code_refs:
  - internal/s3
  - internal/snapshot
sources:
  - title: Backup confirmation issue
    resource: ../../docs/issues/013-UI-5-backup-confirmation-dialogs.md
  - title: Snapshot integrity issue
    resource: ../../docs/issues/015-DEP-3-snapshot-integrity-concurrency-retention.md
---

# Use this when

A backup, snapshot, restore or retention change is being made.

# Rules

- A backup is not complete until it can be restored. Test the restore path.
- S3 credentials and any backup keys come from configuration, never from
  tracked source.
- Retention must not delete the only surviving copy of a world.
- A restore must not overwrite a live world without an explicit confirmation.

# Known gap

Remote file, Minecraft and backup archive paths still read the controller
filesystem; only restore refuses. This is tracked as the remote archive path gap.

# Read next

- [Data safety](../invariants/data-safety.md)
- [Known failure modes](../failures/known-gaps.md)
