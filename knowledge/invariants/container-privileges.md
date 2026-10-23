---
type: Invariant
title: Container privileges
description: Do not broaden container privileges beyond what the server image requires, including through operator overrides.
status: draft
owners: [oss-runtime]
code_refs:
  - internal/docker
  - docker
sources:
  - title: Contributor guide
    resource: ../../AGENTS.md
review_after: 2027-01-23
---

# Rule

Do not add `privileged`, broad capabilities, host mounts or a writable Docker
socket to a game container unless the server image requires it and the change
documents why. Test the effective Docker configuration, including operator
overrides.

# Why

Each Minecraft server is an isolation boundary. A container that can reach the
host or the Docker socket can escape that boundary.

# How to check

- Inspect the effective config after overrides, not only the defaults.
- A new mount needs a path and a reason; a new capability needs a named
  requirement.
- Do not copy a hardening exemption from one image to another.

# Review

This invariant is a draft until a human reviews it and records `verified`.
