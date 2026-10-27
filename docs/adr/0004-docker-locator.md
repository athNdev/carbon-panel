# ADR 0004 — One node-to-daemon locator (OSS)

Date: 2026-10-24
Status: Accepted
Issue: MINE-303

## Context

Nine modules needed the same answer: for a given `NodeID`, which Docker client and
which container freezer applies? Each re-derived it with a private copy of the same
logic (`isLocalNodeID`, a `ClientSet` lookup, a nil-store guard). The copies drifted:
`docker/freezer.go` had no resolution at all and always used the local daemon, so a
hibernation request for a remote node silently targeted the wrong host.

## Decision

`internal/docker.Locator` is the single seam. `NewLocator(pool, local, store)` builds
it from the client pool, the local client and the node store; `NewExecutorLocator` is
the token-only variant for callers that only send commands. `For(nodeID)` returns the
Docker client, `ExecutorFor(nodeID)` the command executor, `IsLocal(nodeID)` the
locality predicate. Resolution failures are typed: `ErrUnknownNode` when the store has
no such node, `ErrNoClient` when the node exists but its daemon is unreachable.

Callers keep their existing return-shaped shims where a test depends on them
(`snapshot`, `command` translate `ErrNoClient` to "no client, no error").

## Consequences

- Local fallback is defined once, for `""`, `local` and `default`.
- `freezer.go` gains a resolver callback (`SetFreezerResolver`) and hibernates on the
  server's own node instead of always locally.
- `NodeStore` is an interface, so a typed-nil `*db.Store` would otherwise read as
  non-nil; the locator guards with reflection and treats it as absent.
- Adding a node transport (agent, proxy) means changing the locator, not nine modules.
- `ClientPool.GetClient`/`GetClientStrict` stay public; this ADR does not remove them.
