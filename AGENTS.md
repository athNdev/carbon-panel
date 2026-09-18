# AGENTS.md — carbon-panel agent operating contract

Carbon-panel is a Go backend + SvelteKit web UI for Minecraft server orchestration
(Docker-isolated servers, single-port TCP proxy, Connect-RPC API).

## Build / test (what CI runs)

- Backend: `make test` (== `go test ./...`), plus `go vet ./...`.
- Frontend: `cd web/carbon-panel && bun install --frozen-lockfile && bun run check && bun run test && bun run build`
- Proto: `make proto-lint` (runs `buf lint` via Docker); `make check` type-checks the frontend.
- See `Makefile` targets (`test`, `fmt`, `lint`, `check`) and `README.md` for dev flow.

## MANDATORY session protocol (Tier 0 persistent context, file-native, no UI)

Every agent session MUST do session-start and session-end. Total start budget: <=3.5K tokens.

**Session-start (read, in order):**

1. This file (`AGENTS.md`).
2. `memory/index.md`, then `memory/decisions.md` (active rows only).
3. Semantic recall: `qmd` top-5 for the task if the qmd daemon is available,
   else `grep -ri "<keywords>" memory/`.
4. Full recipe: `.agents/skills/memory-ops/SKILL.md`.

**Session-end (write, <=0.5K tokens):**

1. Append one entry to `memory/YYYY-MM-DD.md`: what changed, why, commit SHA, Plane IDs.
2. Decision-grade changes: also append one `active` row to `memory/decisions.md`.
3. Never rewrite history: supersede old rows/logs with new entries, don't edit them.

## Product truth

- Roadmap and why live in Plane project **MINE** (MineServer). Markdown links to Plane
  work-item IDs; it never duplicates them.
- Durable architecture decisions live in `docs/adr/NNNN-title.md`.
- Runbook: `docs/context-layer.md`.

## Hard rules

- No secrets, PII, or tokens ever in `memory/` or skills. No exceptions.
- Per-project isolation: this context is carbon-panel only; never import other projects' memory.
- Personas/reviewers are read-only: no memory writes, no Plane edits.

---

## Cluster-Wide Standing Rules (Mandatory Across All Agents)

All agents operating in this repository must strictly comply with the cluster standing rules defined in `/home/prox/agent_setup/AGENTS.md`:

1. **Cross-Node Collision Avoidance (`agentplane`)**: Run `agentplane check <path>` before touching files/resources; claim long-running tasks and release them when done.
2. **Pre-Task Consultation**: Query `agent-context` (`qmd query "<topic>"` or `qmd search "<term>"`) before performing cluster, infrastructure, or service operations.
3. **Post-Action State Updates (MANDATORY COMPLETION GATE)**:
   Any task modifying cluster configuration, infrastructure, services, tooling, skills, provider configs, or architecture MUST update `agent-context`:
   - Document changes in `/home/prox/context-corpus/` (`runbooks/`, `decisions/`, `sessions/`).
   - Sync and reindex: `/home/prox/agent_setup/context/context-sync.sh`
   - Verify health: `/home/prox/agent_setup/context/context-healthcheck.sh`
   - No task modifying system state is complete without completing this step.
4. **Subagent Delegation via `t3-lb`**: Use `t3lb` (`t3lb spawn`, `t3lb wait`, `t3lb nodes`) to distribute heavy or parallel workloads across cluster nodes.
5. **Forgejo System of Record**: Regular git pushes to local Forgejo (port 2222) and run `localrepo snapshot` before ending sessions.
6. **Default Token-Saving Communication: Caveman Mode**:
   All agents operate in Caveman Mode (~65–75% token reduction):
   - Terse, fragment-based, zero conversational filler, no pleasantries.
   - Code blocks, commands, paths, diffs, and errors remain 100% exact and uncompressed.
   - Immediate tool execution without narrative announcements.
   - Switch to full sentences only for safety warnings, destructive actions (`rm -rf`, `DROP TABLE`), or when user requests "normal mode".
