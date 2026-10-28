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

Scheduled backup archives, snapshot create/restore, and scheduled modpack
sync use the mapped world root. Mod installation and packwiz bake write the
mods folder on that same root. Deleting a server still removes the stored
data path.

Panel database snapshots and the offline restore command are specified in
[ADR 0007](../../docs/adr/0007-panel-sqlite-snapshots.md) and
`docs/runbook-panel-db-restore.md`.

# Read next

- [Data safety](../invariants/data-safety.md)
- [Known failure modes](../failures/known-gaps.md)
