# ADR 0005 — Lifecycle transitions are owned leases (OSS)

Date: 2026-10-24
Status: Accepted
Issue: MINE-305

## Context

Mutating a server (create, start, stop, update, delete, back up, hibernate,
reconcile) must not race another mutation of the same server. The panel enforced
this with a process-wide map keyed by server ID. Acquiring returned a `bool`;
releasing took only the server ID.

Because those were separate operations, releasing was not tied to acquiring.
Any code path that reached a release could clear a claim held by a different
operation, and releasing twice was not a no-op. Every caller also had to invent
its own rejection, status write and rollback, which is why the lifecycle rules
were duplicated across handlers, the reconciler, the scheduler and the sleep
managers.

## Decision

A transition claim is a `*docker.Transition` handle:

- `TryBeginServerTransition(serverID)` returns the handle, or
  `ErrTransitionInFlight` / `ErrTransitionServerID`.
- `Transition.Release()` is idempotent and unregisters the claim only while the
  handle still owns it, so a stale handle cannot evict a later owner.
- Holding the handle is the only way to release it; there is no release-by-ID.
- A handle may be handed to a goroutine that outlives the RPC that created it,
  which makes long-running creates an explicit ownership transfer instead of a
  boolean flag plus a deferred release.

The handle owns exclusivity only. Daemon resolution, status-write ordering and
rollback are still the caller's responsibility until the lifecycle module in
MINE-305 lands.

## Consequences

- Releasing a transition you did not acquire is no longer expressible.
- Callers branch on typed errors instead of interpreting `bool`.
- The in-flight maps in the deep-sleep and hibernation managers store the handle,
  so they release the claim they actually took.
- Claims remain process-local; this does not coordinate multiple controllers.
