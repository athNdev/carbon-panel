# Persistent Context Layer — Dev Plan + Runbook (Workstream B)

Tier 0 retrieval via project-local `qmd`. No UI. No secrets in repo.

## Phase 0 — Tier 0+1 (this PR)

Scope: `AGENTS.md`, `memory/`, memory-ops skill
(`.agents/skills/memory-ops/SKILL.md`), qmd wiring (`.qmd/index.yml`,
`scripts/memory-sync.sh`, `memory/eval-fixture.json`), Plane work items.

### Steps

1. Workstream A: seed `AGENTS.md`, `memory/*.md`, memory-ops skill.
2. Workstream B (here): qmd index config, sync script, bench fixture,
   this runbook.
3. Active replica: `ACTIVE_WRITER=1 ./scripts/memory-sync.sh`
   (installs qmd, updates index, embeds, prints `qmd status`).
4. Run bench: `qmd bench` against `memory/eval-fixture.json`.
5. File Plane work items for Phase 1/2 triggers (no Plane Pages — see limits).

### Acceptance

- Cold-agent why-recall: 5 seeded decisions retrievable by a fresh agent.
- Bench hybrid score >= 0.85 on the 10-query fixture.
- Passive rebuild drill: wipe `.qmd/`, re-run sync script, bench still green.

## Single-writer rule

- Exactly one ACTIVE replica runs `scripts/memory-sync.sh` (`ACTIVE_WRITER=1`).
- Passive replicas: `git pull` only; never run the sync script concurrently.
- The script refuses to run without `ACTIVE_WRITER=1`.
- `index.sqlite` is git-ignored; it propagates via re-embed from source,
  never via commit.

## Phase 1 — pgvector triggers (any one fires)

Quantitative gates; evaluate on a 2-week cadence:

1. Scale: `memory/` > 150 files OR `decisions.md` > 400 lines with
   prompt-inject over budget.
2. Quality: top-3 miss rate > 25% on the 20-question eval, 2 weeks running.
3. Toil: compaction costs > 1 agent-hr/week.
4. Conflict: >= 2 memory contradictions recorded per month.

On trigger: provision pgvector, dual-write from qmd source files, cut reads
over behind a flag, keep qmd as fallback until bench parity.

## Phase 2 — persona hardening

- Viewer tokens: read-only tokens for passive/persona agents; writers keep
  the active-replica gate.
- MCP allowlist: memory tools exposed only to the memory-ops skill scope.
- Secret-scan: pre-commit scan over `memory/`, `docs/`, `.qmd/index.yml`;
  block on match; rotate and purge on leak.

## Backup story

- `git` is the source of truth for `memory/`, `docs/`, `AGENTS.md`, skill.
- `pg_dump`: n/a until Phase 1 (no database yet).
- qmd index: disposable; rebuild any time via the sync script
  (`qmd update` + `qmd embed` from source files).

## Known limitation

- Plane Pages API returns 404 on this instance. Do NOT plan on Plane Pages.
- Use Plane work items + `docs/adr/` markdown ADRs instead.

## Rollback

- Phase 0: delete `.qmd/` artifacts, `git revert` this PR, passive replicas
  re-pull. No data loss (source markdown remains).
- Phase 1: drop the pgvector schema/objects, flip read flag back to qmd,
  delete dual-write job.
- Phase 2: revoke Viewer tokens, remove MCP allowlist entries, keep scan.

## Runbook quick-ref

```sh
ACTIVE_WRITER=1 ./scripts/memory-sync.sh   # active replica only
bash -n scripts/memory-sync.sh             # syntax check
qmd status                                 # health
qmd bench                                  # eval vs memory/eval-fixture.json
```
