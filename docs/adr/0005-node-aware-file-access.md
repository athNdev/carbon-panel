# ADR 0005 — Node-aware file access

Date: 2026-10-27
Status: Accepted
Issue: OSS-232 (MINE-255)

## Context

File, configuration and content I/O assumed the controller's filesystem:
`FileService` joined `server.DataPath` with caller paths and read/wrote
directly. That is only correct when the server's node is the controller's own
Docker daemon. On a remote node the data lives on another machine, and
controller-local writes silently land in the wrong place; the restore path
already had a hand-written guard against exactly this
(`requireLocalRestoreStorage`), but the rest of the file surface did not.

## Decision

**Route file, configuration and content I/O through a node-aware accessor**
(`internal/nodestorage`):

- `Accessor` is confined to one server data root (`os.Root`), rejects
  absolute and escaping paths, and refuses symlink writes — the file
  service's long-standing safety rules, enforced in one place.
- `ForNode` resolves the owning node: local nodes (unix socket or loopback
  daemon — ADR-adjacent to the `IsLocal` derivation in node creation) get the
  confined local accessor; **remote nodes are refused with a typed
  `ErrUnavailable`** until their storage is reachable. There is no local-path
  fallback for remote nodes, ever.
- **Shared-storage mapping.** A node may declare `storage_root` (node-side
  root of the shared storage that holds its server data directories) and
  `controller_storage_root` (where the controller mounts the same storage;
  defaults to `storage_root` when the paths are identical on both sides).
  `MapDataPath` maps a server data root to the controller-side directory
  `Join(controller_root, Base(data_root))` for such remote nodes; local nodes
  keep the path unchanged. Remote nodes without a storage root keep the typed
  refusal — a controller-local path is never silently mistaken for the node's
  data.
- `EnsureDataRoot` additionally creates the mapped server directory when
  missing (so restore can target a fresh directory) but requires the
  controller-side storage root itself to pre-exist: creating it could hide a
  missing mount and silently store server data on the controller's local
  disk.

Content RPCs (get/update/delete/rename) route through the accessor in this
change. Backup and restore resolve `server.DataPath` through the same mapping
(worldstore seam and the legacy zip/extract pipelines), and
`requireLocalRestoreStorage` now accepts remote nodes on shared storage.
Archive and upload pipelines keep their own staging/containment and move onto
the seam with the remote adapter.

## Consequences

- A remote-node server now fails file operations *loudly* (503) instead of
  silently corrupting controller-local state.
- Remote nodes on shared storage (NFS and friends) serve file I/O,
  backup and restore through the controller's mount of that storage, without
  waiting for a remote adapter. Nodes without reachable storage keep the
  typed refusal.
- The storage-root columns are part of the node's create/update API and
  model. `Store.CreateNode` uses an explicit column whitelist for insert —
  new node columns must be added there or they are silently dropped.
- Path safety and symlink rules are enforced at one boundary instead of
  scattered per call site; the existing inline checks remain as defense in
  depth.
- The remote adapter (per-node `carbon-storage` style channel over the node's
  Docker daemon) plugs into `ForNode` without further API change; tracked as
  the remainder of OSS-232 together with the archive/upload pipelines and the
  compute-plane half of the mapping: Docker bind translation
  (`TranslateToHostPath`) must bind the node-side path
  `Join(storage_root, Base(data_path))` into remote containers.
