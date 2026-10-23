# Working state

Ground truth for cross-agent coordination. Verify with `git`, never with a
report. Update this file before and after every work round.

Last updated: 2026-10-23 18:35 local

## Checkouts

| Path | Branch | HEAD | Upstream | Working tree |
| --- | --- | --- | --- | --- |
| `C:\research_learning_new\carbonpanel` | `codex/oss-docker-resilience` | `db19d699` | in sync | 3 modified, 13 untracked (see below) |
| `C:\research_learning_new\carbon-cloud-managed` | `codex/cloud-managed-minecraft` | `a85136bb` | in sync | 3 modified, 1 untracked |
| `C:\research_learning_new\carbon-cloud` | `codex/cloud-reliability-hardening` | `b9783fbe` | n/a | 8 modified, design docs untracked (superseded clone) |

## Open reviews

| Epic | PR | Base | State |
| --- | --- | --- | --- |
| MINE-249 | [#225](https://github.com/athNdev/carbon-panel/pull/225) | `main` | open, awaiting supervisor |
| MINE-231 | [#29](https://github.com/athNdev/carbon-cloud/pull/29) | `main` | draft |

## Uncommitted OSS increment

Modified: `go.mod`, `go.sum` (klauspost/compress 1.18.6 → 1.18.7),
`internal/db/migrations.go` (registers `ServerOperation`).

New: `internal/worldstore/` (store, journal, tests), `internal/testharness/`
(proxy, manifest, tests), `internal/db/server_operation.go`,
`cmd/carbon-fault-harness/`, `cmd/carbon-storage/`,
`docs/testing/oss-fault-harness.md`, `docs/research/compress-advisory-2026-10-23.md`.

## Decisions in force

- **2026-10-23** — The uncommitted OSS increment folds into PR #225 rather than a
  separate PR. (user)
- Subagents run on `opencode/go/deepseek-v4.1-flash`. Zen models are unavailable
  on this plan.
- Never merge, never deploy, never mutate the Proxmox cluster from agent work.
- D01 and D03–D14 remain proposed. MINE-238/239 stay in Backlog; no commerce
  implementation until a decision is recorded.

## Verification ledger

| Date | Scope | Command | Result |
| --- | --- | --- | --- |
| — | — | — | not yet run |
