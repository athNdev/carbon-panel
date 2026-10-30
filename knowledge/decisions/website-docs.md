---
type: Decision
title: Website and documentation publication
description: Publish the homepage and commit-pinned application documentation as one Pages artifact with independent presentation.
status: stable
owners: [oss-runtime]
code_refs:
  - .github/workflows/landing.yml
  - .github/workflows/docs.yml
  - docs/website-source.json
  - docs/carbon-panel
  - docs/design-system
  - web/carbon-panel/src/lib/design-system/tokens.css
  - scripts/build_website.py
  - scripts/check_website.py
sources:
  - title: Website publishing
    resource: ../../docs/website-publishing.md
---

# Decision

- The homepage and documentation share one artifact. Only the website deployment
  workflow publishes Pages; docs validation must not replace the root homepage.
- Application prose, imported examples and API schema come from the same full
  commit in `docs/website-source.json`. Never substitute the current checkout or
  a floating branch if that commit is unavailable.
- The standalone shared design system is published with the homepage from the
  current branch, including its canonical tokens. It is not application content.
- The website theme and docs navigation can evolve independently of the source
  pin. Display the pin on every documentation page.
- A development commit must be labelled as a development snapshot. Do not infer
  a stable release from a package version or an inherited tag.
- Homepage and documentation previews share the versioned social card documented
  in `assets/branding/README.md`. Keep preview metadata free of em dashes.

# Rationale

Independent Pages deployments overwrite one another. A single artifact preserves
both routes, and an explicit source pin prevents examples and API schemas from
silently following unreleased changes.

# Verification

Use the commands and review procedure in the publishing guide. Snapshot-isolation
tests verify that working-tree edits cannot leak into pinned docs or examples.
