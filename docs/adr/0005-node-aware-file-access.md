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
  `ErrUnavailable`** until a remote adapter is wired. There is no local-path
  fallback for remote nodes, ever.

Content RPCs (get/update/delete/rename) route through the accessor in this
change. Archive and upload pipelines keep their own staging/containment and
move onto the seam with the remote adapter.

## Consequences

- A remote-node server now fails file operations *loudly* (503) instead of
  silently corrupting controller-local state.
- Path safety and symlink rules are enforced at one boundary instead of
  scattered per call site; the existing inline checks remain as defense in
  depth.
- The remote adapter (per-node `carbon-storage` style channel over the node's
  Docker daemon) plugs into `ForNode` without further API change; tracked as
  the remainder of OSS-232 together with the archive/upload pipelines.
