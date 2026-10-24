---
type: Workflow
title: Release
description: The checks and artifacts required before a tagged Carbon Panel release.
status: stable
owners: [oss-runtime]
code_refs:
  - .github/workflows
  - Makefile
  - CHANGELOG.md
sources:
  - title: Continuous integration
    resource: ../../.github/workflows/ci.yml
---

# Steps

1. Update `CHANGELOG.md` and any user-facing docs affected by the change.
2. Run the full verification from [testing](../testing/verification.md).
3. Build the frontend with `bash scripts/web-build.sh`.
4. Let CI build the backend, Windows compile target, frontend checks, proto
   lint and images.
5. Tag only after CI is green. Do not merge or deploy from an agent session;
   that is the supervisor's decision.

# Rules

- A release note must describe shipped behavior, not planned behavior.
- Do not publish an image that was built from a dirty or unverified tree.
- If a check cannot run, say so; do not describe it as passed.

# Read next

- [Verification](../testing/verification.md)
- [Repository map](../repo-map.md)
