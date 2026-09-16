# memory/decisions.md — decision log (append-only)

Never edit or delete rows. Supersede with a new row (new id, status `supersedes <id>`).

| id | date | status | decision | why | links |
|----|------------|--------|----------|-----|-------|
| D-001 | 2026-09-16 | active | Tier 0 file-native context + qmd chosen over mem0/Zep/pgvector-now | Zero infra, no UI, grep fallback keeps recall working without a daemon | docs/context-layer.md |
| D-002 | 2026-09-16 | active | Plane work items are product truth; Pages API 404s on this instance so ADRs live in docs/adr/ | Avoids a second roadmap copy; markdown links Plane IDs instead of duplicating them | docs/adr/ |
| D-003 | 2026-09-16 | active | Single-writer active-VM rule for memory writes | Prevents concurrent sessions clobbering memory/ logs and decisions | .agents/skills/memory-ops/SKILL.md |
