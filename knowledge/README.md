# Knowledge base

This directory is the repository's agent-facing context database. It is not a
second documentation site.

## What belongs here

- Durable facts an agent needs before changing code: boundaries, invariants,
  seams, workflows and known failure modes.
- Routing metadata: which concept governs which paths (`code_refs`).
- A link to the authoritative `docs/` page, ADR, plan or issue.

## What does not belong here

- Task state, assignees or sprint status. The live tracker is authoritative.
- Long prose, API tables or configuration reference. Link to `docs/`.
- Secrets, credentials, customer data, private endpoints or workstation paths.
- Generated inventories. Run the command instead.

## Concept types

| Type | Use |
| --- | --- |
| `Overview` | What the project or repository is, and its boundaries |
| `Architecture` | A component, data flow or seam |
| `Invariant` | A rule that must not be violated |
| `Workflow` | How to perform a recurring change |
| `Runbook` | A pointer to an operational procedure |
| `Decision` | An ADR or a pending decision |
| `Domain Term` | Vocabulary with a project-specific meaning |
| `Failure Mode` | A known failure or open gap |

## Frontmatter

```yaml
---
type: Architecture
title: Runtime model
description: How the daemon starts and which components it owns.
status: stable
owners: [oss-runtime]
code_refs:
  - cmd/carbon-panel
  - internal/rpc
sources:
  - title: Design specification
    resource: ../../DESIGN.md
---
```

Rules:

- One concept per file. The path is the stable ID; do not rename a concept.
  Deprecate it with `status: deprecated` and link to the replacement.
- `description` is one sentence and at most 240 characters.
- `code_refs` are repository-relative. Use a directory for a subsystem.
- `verified` is required for a **stable** `Invariant`, and must start with
  `human:`. Draft invariants are allowed but are reported as unverified.
- `review_after` is optional; the linter warns when it has passed.

## Writing protocol

1. Search before writing: `rg -n "remote node docker client" knowledge`
   (or `make knowledge-search q="remote node docker client"`).
2. Read the governing concept before editing its `code_refs`.
3. Create or update the concept in the same change as the behavior.
4. Run `go run ./cmd/knowledge-lint -root . -fix`, then run it again without
   `-fix`.
5. Commit the concept with the code change.

A subagent writes only inside its assigned section. Shared concepts
(`overview`, `glossary`, `invariants`) are supervisor-owned.

## Retrieval smoke test

`knowledge/queries.yaml` fixes ten questions and their expected concept. The
linter scores each question against IDs, titles, descriptions and bodies and
fails if the expected concept is not in the top three. Add a query when a new
subsystem becomes load-bearing.

## MCP protocol smoke test

```sh
node scripts/mcp-smoke.mjs
```

It starts the pinned filesystem and git servers in temporary directories,
exercises read/write/edit/search and status/add/commit/log, and does not touch
the repository. It needs Node 24+ and network access on the first run.

## Conflict resolution

Conflicts are expected and are resolved in git, not in a service.

1. Merge the body semantically; keep the newer verified claim.
2. Union list fields (`code_refs`, `sources`); do not duplicate entries.
3. Set `status: draft` if the change is provisional.
4. For an `Invariant`, remove the stale `verified` entry; a human must re-verify.
5. Never hand-merge `index.md`. Run `make knowledge-index`.
6. Run `make knowledge-lint` and commit the resolution.

`index.md` is generated. `log.md` is deliberately absent; `git log -- knowledge/`
is the history.

## MCP access

The committed `.mcp.json` exposes two pinned servers:

- `carbon-knowledge` — `@modelcontextprotocol/server-filesystem@2.0.0`, scoped
  to `knowledge/`. Read, write, edit, move and list concepts. Requires Node 24+.
- `carbon-git` — `@cyanheads/git-mcp-server@2.15.3`. Status, diff, add, commit,
  merge and rebase. Requires Node 20+.

Launch the client from the repository root so the relative `knowledge` path
resolves. On Windows, if the client cannot spawn `npx`, use `cmd /c npx ...`.
If a client ignores project-level MCP settings, add the same entries to its
user configuration with an absolute path.

## CLI fallback

The Go command is the portable path; `make` is a convenience wrapper.

```sh
go run ./cmd/knowledge-lint -root .        # validate
go run ./cmd/knowledge-lint -root . -fix   # regenerate indexes
rg -n --heading "world preservation" knowledge
```

On systems with `make`:

```sh
make knowledge-search q="world preservation"
make knowledge-index
make knowledge-lint
make knowledge-mcp        # filesystem server on knowledge/
make knowledge-mcp-git    # git server for this repository
```

The CLI path is authoritative. MCP is an accelerator and must not be required
to build, test or merge a change.
