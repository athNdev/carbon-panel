---
type: Workflow
title: Verification
description: The commands that define a verified change in this repository.
status: stable
owners: [oss-runtime]
code_refs:
  - Makefile
  - .github/workflows/ci.yml
sources:
  - title: Contributor guide
    resource: ../../AGENTS.md
---

# Backend

```sh
go build ./...
go vet ./...
go test ./...
```

`make test` is the same suite. CI also compiles every package for Windows
without executing the Windows binaries.

# Frontend

```sh
cd web/carbon-panel
bun install --frozen-lockfile
bun run check
bun run test
```

# Production frontend build

```sh
bash scripts/web-build.sh
```

# Landing page

```sh
python3 -m unittest scripts.test_landing_page
python3 scripts/check_brand_assets.py
```

CI runs these checks for the standalone landing page and distributed brand assets.

# Proto

```sh
make proto-lint
```

# Knowledge

```sh
go run ./cmd/knowledge-lint -root . -fix
go run ./cmd/knowledge-lint -root .
node scripts/mcp-smoke.mjs
```

The linter runs the fixed queries in `knowledge/queries.yaml`. The MCP smoke
needs Node 24+ and network access on the first run.

# Local note

`prettier --check .` can fail on a Windows checkout because of CRLF. CI runs on
Linux; use `--end-of-line auto` for local checks. Do not change tracked
line-ending policy to silence a local-only failure.
