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

# Proto

```sh
make proto-lint
```

# Knowledge

```sh
make knowledge-index
make knowledge-lint
```

# Local note

`prettier --check .` can fail on a Windows checkout because of CRLF. CI runs on
Linux; use `--end-of-line auto` for local checks. Do not change tracked
line-ending policy to silence a local-only failure.
