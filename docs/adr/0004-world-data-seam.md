# ADR 0004 — World data flows through one seam (worldstore)

Date: 2026-10-27
Status: Accepted
Issue: OSS-261 (MINE-302)

## Context

World data had three uncoordinated paths: the scheduler wrote backup zips with
one code, `RestoreBackup` extracted archives over the live data tree with no
atomicity (a crash mid-extract leaves a half-restored world), and the
`carbon-storage` CLI used `internal/worldstore`, a purpose-built store with
identity checks, a manifest format and a journaled atomic swap. The
qualification work (atomic backup/restore, migration, resumable deletion)
cannot be proven once per path.

## Decision

**Consolidate snapshot, backup and restore of world data on
`internal/worldstore` as the single seam** (owner-approved 2026-10-27).

Shipped in this change:

- **Default scheduled backups** run through `worldstore.BackupTo`: a
  deterministic manifest archive (`.worldstore-manifest.json`, per-file
  SHA-256, whole-archive SHA-256) covering the **full server state tree**
  minus the configured excludes, streamed to disk.
- **Restores of manifest'd archives** run through `worldstore.RestoreFrom`:
  content verified against the manifest, then one journaled, atomic swap
  (stage → rename) shared with the CLI, with startup `Recover()` for
  interrupted swaps.
- Both sides construct identity through one helper
  (`worldstore.ServerIdentity`) so ownership checks cannot drift.

## Consequences

- **Restore semantics change**: restoring a default backup replaces the
  complete server state tree (worlds *and* configs) instead of overlaying
  only world files. Excluded paths (logs, caches) are simply not part of the
  restored state. This is the honest meaning of "restore to the backup".
- Default backup archives now include configs and other non-world state
  (excludes still applied) — slightly larger artifacts, complete restores.
- **Custom-path backups** (`config.Paths` set) keep the legacy zip pipeline
  and legacy extraction restore; migrating them to the seam is tracked under
  OSS-235. Their restores remain non-atomic.
- The streaming variants use generous fixed bounds (4 GiB/file, 64 GiB total)
  instead of the in-memory API's CLI-sized caps; every extracted byte is still
  hash-verified against the manifest.
- Non-goal: cross-installation restore policy (archives do not yet embed
  identity); tracked under the data-safety area of the epic.
