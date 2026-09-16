---
name: memory-ops
description: Session-start and session-end recipe for carbon-panel Tier 0 file-native persistent context.
---

# memory-ops — Tier 0 session recipe

## Session-start (budget <=3.5K tokens, read in order)

1. `AGENTS.md` (~1K) — operating contract.
2. `memory/index.md` + `memory/decisions.md` active rows only (~1K).
3. Semantic recall (~1K): `qmd query "<task>" --json -n 5`
   (top-5 hits only). Fallback if no daemon: `grep -ri "<keywords>" memory/`.
4. Plane context (~0.5K): top-3 work-item titles from project MINE — titles only, never full bodies.

## Session-end (append <=0.5K tokens)

Append to `memory/YYYY-MM-DD.md` (create the file if missing):

```md
## <HH:MM UTC> — <short title>
- What: <1-2 lines>
- Why: <1 line>
- SHA: <commit sha or `uncommitted`>
- Plane: <MINE-xxx, ... or `none`>
```

Decision-grade change? Also append one row to `memory/decisions.md`
(`D-NNN | <date> | active | <decision> | <why> | <links>`).
Never rewrite history: supersede, don't edit.

## Weekly compaction (logs older than 7 days)

1. Fold durable outcomes from old `memory/YYYY-MM-DD.md` files into
   `memory/decisions.md` rows or `docs/adr/NNNN-title.md` records.
2. Clear `memory/scratchpad.md` back to its one-line placeholder.
3. Run `scripts/memory-sync.sh` to verify qmd index freshness.
4. Keep history: compact by superseding, never by deleting.

## qmd CLI fallback (no daemon)

- Daemon up: `qmd query "<task>" --json -n 5`.
- Daemon down: `qmd search "<keywords>" -c carbon-panel`, then `grep -ri "<keywords>" memory/`.
- Ripgrep is always the last resort and always works offline.
