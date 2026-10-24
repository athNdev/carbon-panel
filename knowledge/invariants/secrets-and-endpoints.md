---
type: Invariant
title: Secrets and endpoints
description: Credentials, tokens, private endpoints and workstation paths never enter tracked source or the knowledge base.
status: stable
owners: [oss-runtime]
code_refs:
  - config.example.yaml
  - docker-compose.yml
sources:
  - title: Contributor guide
    resource: ../../AGENTS.md
verified: { by: human:athNdev, at: 2026-10-24 }
review_after: 2027-01-23
---

# Rule

Do not commit passwords, API keys, session secrets, private keys, customer data,
private network addresses, homelab hostnames or absolute workstation paths.
Examples use placeholders and environment variables.

# Why

The repository is shared across environments and may be published. A leaked
credential is not fixed by deleting the line; it is already in history.

# How to check

- Search the diff for credentials and private addresses before committing.
- Use `config.example.yaml` for documented settings and environment variables
  for real values.
- The knowledge linter rejects common secret and private-path patterns; it is a
  backstop, not permission to be careless.

# Review

Reviewed `2026-10-24` by `agent:mimo-v2.6-pro` against the tree and attested by
`human:athNdev`: the knowledge denylist and manual pattern searches find no
credentials, private endpoints or workstation paths.

History was not scanned, so a secret that was committed and later removed is not
covered by this rule; that remains an accepted limitation of the check.
