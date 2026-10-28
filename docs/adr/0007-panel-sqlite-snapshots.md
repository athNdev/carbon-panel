# ADR 0007 — Daily panel SQLite snapshots

Date: 2026-10-28
Status: Accepted
Issue: MINE-260
Supersedes: ADR 0003

## Context

ADR 0003 chose Litestream-style continuous replication of the controller
SQLite file and described an RPO of seconds. That replication was not what
shipped. The controller takes a daily `VACUUM INTO` snapshot, checks
`PRAGMA integrity_check` before replacing the previous file, and restores
only through an explicit offline command.

Continuous replication would promise a recovery point the OSS build does not
measure or ship. Operators need the published mode to match the code.

## Decision

The OSS controller durability mode is a **daily consistent snapshot**, not
continuous replication.

- `database.panel_backup: daily` writes `panel-YYYYMMDD.sqlite` with
  `VACUUM INTO` while the database stays open.
- A failed integrity check deletes only the partial file and leaves the
  previous snapshot in place.
- Retention pruning never removes the only remaining snapshot when `keep`
  is at least 1, and it never deletes a snapshot that failed its check.
- Restore is explicit: `carbon-panel -restore-panel-backup <snapshot>` with
  the panel stopped. The previous database is kept at
  `<database.path>.pre-restore`, and stale `-wal` / `-shm` files are removed
  so they cannot replay over the restored file.
- There is no automatic restore and no claim of multi-controller failover.
- Worlds and backup archives are outside this database. This decision does
  not change their storage.

Startup still runs forward-only migrations. A new table is added by
AutoMigrate on both a fresh database and an existing one.

## Consequences

- Recovery point is the last successful daily snapshot, not seconds.
- The restore drill is the round trip in `internal/db/panel_restore_test.go`:
  snapshot, change the live file, restore, and read the snapshotted rows back.
- ADR 0003's continuous-replication objective is withdrawn.

## Restore drill

1. Stop the panel.
2. Run `carbon-panel -config config.yaml -restore-panel-backup <snapshot>`.
3. Confirm the command reports success and `<database.path>.pre-restore` exists.
4. Start the panel and confirm the server list matches the snapshot.
