# Agent handoff records

Verbatim kickoff and follow-up prompts given to the implementation subagents
working the two active epics. They are kept here so a later agent — or a human
reviewer — can reconstruct what was asked, which constraints applied, and which
decisions were settled at the time.

These are working records, not product documentation. They contain homelab
endpoints and tracker identifiers; do not copy them into release notes,
published docs, or anything leaving the private repo.

## Active epics

| Epic | Repository | Branch | Review |
| --- | --- | --- | --- |
| **MINE-249** — OSS Docker resilience, data safety and feature completeness | `carbon-panel` (this repo) | `codex/oss-docker-resilience` | PR #225 |
| **MINE-231** — Managed Minecraft Cloud: catalog, providers, payments, OSS parity | `carbon-cloud` (design) and `carbon-cloud-managed` (implementation) | `codex/cloud-managed-minecraft` | draft PR #29 |

Both epics live in Plane workspace `homelab`, project `CarbonPanel` (`MINE`).
The live tracker is authoritative; these files are a snapshot of instruction,
not of issue state.

## Checkouts

| Path | Role | Branch |
| --- | --- | --- |
| `C:\research_learning_new\carbonpanel` | OSS product, this repository | `codex/oss-docker-resilience` |
| `C:\research_learning_new\carbon-cloud-managed` | **Live Cloud implementation.** Same GitHub repo as below, draft PR #29. | `codex/cloud-managed-minecraft` |
| `C:\research_learning_new\carbon-cloud` | Earlier Cloud checkout, superseded for implementation by the clone above. Holds uncommitted billing/node-service/deploy edits. | `codex/cloud-reliability-hardening` |

The design corpus (`CONTEXT.md`, `docs/plans/*`, `docs/research/*`) is committed
on `codex/cloud-managed-minecraft`, five commits ahead of `origin/main`. The
older `carbon-cloud` checkout still has untracked copies plus uncommitted edits
whose relationship to that branch has not been verified — diff before
discarding anything there.

## Documents

| File | Source | Purpose |
| --- | --- | --- |
| `mine-249-oss-kickoff.md` | `oss-agent.md` | Original OSS epic brief: scope, work-package order, boundaries, tracker rules. |
| `mine-249-oss-followup-1.md` | `oss-followup.md` | First supervisor review round and corrections. |
| `mine-249-oss-followup-2.md` | `oss-followup-2.md` | Second review round; current expectations for the OSS branch. |
| `mine-231-cloud-kickoff.md` | `cloud-agent.md` | Original Cloud epic brief, including C01/D02 direction and the D01–D14 decision register. |
| `mine-231-cloud-followup-1.md` | `cloud-followup.md` | First Cloud review round. |
| `mine-231-cloud-followup-2.md` | `cloud-followup-2.md` | Second Cloud review round; moves the work from synthetic to real fleet allocation. |

The raw Plane dumps used to build these prompts are deliberately not stored
here. They were large, and they duplicate state that is better read live.

## Agent allocation

Master runs on `opencode/go/deepseek-v4.1-flash`, thinking enabled, effort `max`.
It is not the highest-scoring model available, but it has the largest Go monthly
allowance ($80) and the cheapest turns. Models scoring higher on published
intelligence charts sit on $15 caps that a long orchestration session exhausts
mid-epic, and a mid-session model swap costs more than the difference in score.
Capability is spent on reviewers instead of on the orchestrator.

| Role | Model | Effort |
| --- | --- | --- |
| Master (orchestration, verification) | `go/deepseek-v4.1-flash` | `max` |
| Independent review | `go/grok-4.7` or `go/qwen3.8-max` | `high` |
| Implementation | `go/gpt-6-luna` | model default |
| Mechanical sweeps, log triage | `go/deepseek-v4.1-flash` | low, thinking off |
| Disputed-finding adjudication | strongest available non-DeepSeek model | `max` |

Reasoning effort is a Zed-level setting, not a per-spawn parameter: `spawn_agent`
accepts a model but no effort argument. Reviewers must come from a different
model family than the code author, otherwise review inherits the author's blind
spots.

DeepSeek peak pricing applies 01:00–04:00 and 06:00–10:00 UTC, Monday–Friday,
which is roughly 12:00–15:00 and 17:00–21:00 at UTC+11. Heavy runs are cheaper
scheduled outside those windows.

## Settled decisions

- **C01** — Carbon Cloud is one fully managed product with modular, general
  deployment configuration and a fixed Carbon identity. No rebranding and no
  white-label mode. Provider adapters, fleet inventory, regions, SKUs, pricing
  and payment settings stay configurable.
- **D02** — The first managed release ships both dedicated and shared hosting.
- **Terminology** — *hosting instance* (the purchased contract, which survives
  infrastructure replacement), *Minecraft server* (game and world), *worker
  node*, *worker VM*, *node agent*, *allocation*, *worker pool*, *control plane*.
  Glossary lives in `CONTEXT.md` in the Cloud repositories.

**D01 (merchant ownership), D03–D14 remain proposed.** Do not treat them as
accepted. They block real commerce work (MINE-238/239) until decided.

## Rules of engagement

- One feature branch and one PR per epic. Iterate CI to green. Do not create
  replacement branches or PRs.
- Never merge or deploy. A supervisor reviews; final Done/closure is theirs.
- If an item is ambiguous, blocked, partial, or cannot be verified, say so in a
  Plane comment and leave it in **Backlog**. Do not move it forward because a
  helper, mock, control, or partial patch exists.
- Travelling state: these agents reported clean worktrees while the actual
  worktrees were dirty. Always verify with `git status` and check each checkout
  separately.
