# Panel database backup and restore

How to back up and restore the Carbon Panel database (`database.path`, the
SQLite file that holds servers, nodes, users and operation state).

## Durability model

What the panel promises, and what it does not:

- The database runs in **WAL mode with `synchronous=NORMAL`**. A hard crash
  (power loss, kernel panic) may lose the most recent writes. This is a
  deliberate durability/throughput trade-off.
- **Daily snapshots** (`database.panel_backup: daily`) are taken with
  `VACUUM INTO`, so each snapshot is a *crash-consistent* copy of a live
  database, integrity-checked before it replaces the previous snapshot. A
  restore therefore bounds data loss to the last snapshot.
- In-flight Docker operations are journaled separately: after any restore or
  crash, startup recovery reconciles interrupted mutations before releasing
  server lifecycle locks.

There is **no automatic restore**. Snapshots are not shipped off the host by
this mechanism (this is not Litestream); use the encrypted offsite backup
system for worlds, and ship panel snapshots with your own tooling if needed.

## Restoring

1. **Stop the panel.** The restore swaps database files and must not race a
   running process.

2. **Restore from a snapshot:**

   ```sh
   carbon-panel -config config.yaml -restore-panel-backup /path/to/backups/panel-YYYYMMDD.sqlite
   ```

   The command:
   - refuses to install a snapshot that fails `PRAGMA integrity_check`;
   - keeps the previous database at `<database.path>.pre-restore`;
   - removes stale `-wal` / `-shm` sidecars left by the previous database
     (a leftover `-wal` would be replayed over the restored file and corrupt
     it on the next start);
   - re-checks the installed file before exiting.

3. **Start the panel** and verify: log in, check the dashboard loads and the
   server list matches the snapshot date.

If the restore command reports that a stale sidecar could not be removed,
delete `<database.path>-wal` and `<database.path>-shm` manually **before**
starting the panel.

### Manual fallback

If the binary cannot run on the host: stop the panel, verify the snapshot
(`sqlite3 snapshot "PRAGMA integrity_check"` → `ok`), move the current
database aside, copy the snapshot to `database.path`, and delete
`database.path-wal` and `database.path-shm`. Never skip the sidecar cleanup.

## Rolling back a mistaken restore

The replaced database is kept at `<database.path>.pre-restore`. Stop the
panel, move it back to `database.path`, and remove any `-wal` / `-shm`
sidecars again.

## Verifying snapshots without restoring

```sh
sqlite3 /path/to/backups/panel-YYYYMMDD.sqlite "PRAGMA integrity_check;"
```

Snapshots older than `database.panel_backup_keep` days are pruned
automatically at the next backup.
