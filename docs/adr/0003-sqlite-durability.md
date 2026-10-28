# ADR 0003 — Controller SQLite durability (OSS)

Date: 2026-10-21
Status: Superseded by ADR 0007
Issue: MINE-181

## Context

The OSS panel uses a single SQLite database for its control plane. Multi-controller
failover is not a near-term OSS goal (cloud uses Postgres). Losing the DB file takes
down management of every server on that controller.

## Decision

Superseded by [ADR 0007](0007-panel-sqlite-snapshots.md). The text below is the
original decision and is not the shipped durability mode.

Ship **Litestream-style continuous backup** of the panel SQLite file to a local or
S3-compatible destination, plus a documented restore runbook. Do not implement
dual-writer SQLite or migrate OSS to Postgres/rqlite.

## Consequences

- Single-writer model retained; no schema fork.
- Recovery objective: restore from last replicated segment; RPO ~seconds.
- Non-goal: multi-controller active/active failover for OSS.

## Restore drill

1. Stop panel.
2. Restore latest snapshot + WAL segments to data dir.
3. Run `integrity_check`; restart panel.
