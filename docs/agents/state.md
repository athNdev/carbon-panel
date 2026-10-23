# Working state

Ground truth for cross-agent coordination. Verify with `git`, never with a
report. Update this file before and after every work round.

Last updated: 2026-10-23 19:45 local

## Checkouts

| Path | Branch | HEAD | Upstream | Working tree |
| --- | --- | --- | --- | --- |
| `C:\research_learning_new\carbonpanel` | `codex/oss-docker-resilience` | `725508e2` | in sync | clean |
| `C:\research_learning_new\carbon-cloud-managed` | `codex/cloud-managed-minecraft` | `f804c68f` | in sync | clean |
| `C:\research_learning_new\carbon-cloud` | `codex/cloud-reliability-hardening` | `b9783fbe` | n/a | 8 modified, design docs untracked (superseded clone) |

The older `carbon-cloud` checkout holds uncommitted billing/node-service edits
whose relationship to the managed branch is still unverified. Diff before
discarding anything there.

## Open reviews

| Epic | PR | Base | State |
| --- | --- | --- | --- |
| MINE-249 | [#225](https://github.com/athNdev/carbon-panel/pull/225) | `main` | open, all 9 checks green, merge state CLEAN |
| MINE-231 | [#29](https://github.com/athNdev/carbon-cloud/pull/29) | `main` | draft, 3 checks green, merge state CLEAN |

Neither is merged. Merging is the supervisor's decision.

## Landed this round

OSS (`carbonpanel`): the previously uncommitted increment is now on the branch —
server-operation persistence, `internal/worldstore`, `internal/testharness`,
`cmd/carbon-fault-harness`, `cmd/carbon-storage`, the compress bump and docs.
Then the UI fixes for MINE-279 to 284, and a lint pass that migrated the Docker
client construction to `client.New`.

Cloud (`carbon-cloud-managed`): compress pin plus a `govulncheck` step scoped to
the node agent, the UI fixes for MINE-285 to 291, and a `go 1.26.10` minimum so
the standard-library advisory GO-2026-6809 is out of range.

## Verification ledger

| Date | Scope | Command | Result |
| --- | --- | --- | --- |
| 10-23 | OSS increment | `go build/vet`, `go test ./...` | pass |
| 10-23 | OSS UI | `bun run check`, `bun test` | 0 errors, 44 pass |
| 10-23 | OSS lint | `golangci-lint v2.13.2 --new-from-rev=origin/main` | 0 issues |
| 10-23 | Cloud backend | `go build/vet`, `go test ./internal/cloud/...` | pass, 25 pkgs |
| 10-23 | Cloud console | `bun run check`, `bun test` | 0 errors, 25 pass |
| 10-23 | CI | PR #225 and #29 | all checks green |

Local `prettier --check .` fails repo-wide on this Windows checkout because
`core.autocrlf` produces CRLF. CI runs on Linux and is unaffected; use
`--end-of-line auto` for local checks.

## Decisions in force

- **2026-10-23** — The uncommitted OSS increment folds into PR #225 rather than a
  separate PR. (user)
- Subagents run on `opencode/go/deepseek-v4.1-flash`. Zen models are unavailable
  on this plan, and `spawn_agent` exposes no per-subagent reasoning effort.
- Never merge, never deploy, never mutate the Proxmox cluster from agent work.
- D01 and D03–D14 remain proposed. MINE-238/239 stay in Backlog; no commerce
  implementation until a decision is recorded.

## Open blockers

- D01 (merchant ownership) and D03–D14 block real commerce. Nothing beyond the
  nonpayable development checkout can be finished until these are decided.
