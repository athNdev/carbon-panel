# ADR 0004 — Proxy route policy is a pure plan (OSS)

Date: 2026-10-23
Status: Accepted
Issue: MINE-306

## Context

`Manager.UpdateServerRoute` held the manager lock while reading the proxy
listener and node from the store, then encoded the status and topology rules
inline. `Start` repeated the same route construction, and direct-port knock
handling was interleaved with route mutation. A slow database therefore
serialised unrelated route updates, and the status table could only be tested
through a store and a fake proxy.

## Decision

Route policy is a pure function and the manager is its applier.

- `PlanFor(PlanInput) RoutePlan` decides the route action, backend, hibernated
  and down flags, and the direct-port knock action from already-observed state.
  It performs no I/O and does not mutate its inputs.
- `Manager.planInput` performs store and Docker observation before taking the
  manager lock. Listener and backend failures are carried in the plan rather
  than guessed at.
- `Manager.Apply` executes a plan under the lock, performs no store reads, and
  is the only writer of route state.
- `Start` and `UpdateServerRoute` share the down-route applier; `Start` keeps
  its own initial inventory load.

## Consequences

- Route rules are a reviewable table covered by a status-by-topology test.
- No store I/O happens under the manager lock.
- A missing listener or backend is explicit in the plan and returned to the
  caller; `Apply` never substitutes a route.
- Valkey broadcast failures are logged with route identity instead of being
  silently discarded. A broadcast failure does not roll back a local route.
- A follow-up can move `Start` fully onto `PlanFor` once the startup inventory
  path is covered by the same tests.
