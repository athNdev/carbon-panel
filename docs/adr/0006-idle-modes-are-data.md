# ADR 0006 — Idle modes are data, not strategies (OSS)

Date: 2026-10-25
Status: Accepted
Issue: MINE-304

## Context

Two idle modes existed as two managers: the cgroup freezer (`HibernationManager`)
and deep sleep (`DeepSleepManager`). Each owned an activity clock, an in-flight
map, a sweep loop and a ticker. The sweeps were the same algorithm; the modes
differed in four values — which column opts a server in, the timeout and its
default, and the container action.

MINE-304 proposed collapsing them into one `IdleManager` with a `Strategy`
interface carrying `Sleep`, `Wake` and `AsleepStatus`.

## Decision

The shared parts are one module: `idleTracker` (activity clock, claim,
player-count provider), `idleMode` (the four mode-specific values plus log
formats) and `idleRunner` (ticker, cancellation, sweep). Both managers are thin
adapters over them.

A new idle mode is **data**: an `idleMode` literal wired to a runner. It is not a
new manager, and it is not required to satisfy an interface.

We deliberately did **not** introduce the proposed `Strategy` interface.
`idleMode` already provides the extension point, and there are exactly two
modes whose container operations stay genuinely different (pause/unpause versus
stop/start). An interface with two implementations and no third caller would add
indirection without leverage, and it would invite folding the two `Wake`
operations together before their semantics actually converge.

The three rollback paths (`restoreRunningContainer`, `rollbackSleep`,
`rollbackWake`) were also left mode-specific. They look similar but map observed
container state differently, and merging them would change failure behaviour in
the code that runs when things have already gone wrong. A third mode is the
evidence that would justify a shared shape.

## Consequences

- Adding a mode means writing an `idleMode` and wiring a runner, not copying a
  manager.
- Single-flight and the idle clock have one owner, so a mode cannot release a
  claim it did not take.
- `FreezerStore` and `SleeperStore` remain separate names even though they are
  identical; removing the duplication is cosmetic and can follow.
- Revisit the `Strategy` split only when a third mode appears or the two wake
  paths converge.
