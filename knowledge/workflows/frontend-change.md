---
type: Workflow
title: Frontend change
description: How to change the SvelteKit console and verify it without breaking the embedded build.
status: stable
owners: [oss-runtime]
code_refs:
  - web/carbon-panel
  - scripts/web-build.sh
sources:
  - title: SvelteKit resolve navigation ADR
    resource: ../../docs/adr/0002-sveltekit-resolve-navigation.md
---

# Steps

1. Work in `web/carbon-panel` (SvelteKit 2, Svelte 5, IBM Carbon tokens).
2. Use the generated Connect client from `src/lib/proto`; do not add a second
   API layer.
3. Run:

```sh
cd web/carbon-panel
bun install --frozen-lockfile
bun run check
bun run test
```

4. Build the embedded assets with `bash scripts/web-build.sh` before a
   production or binary build.

# Rules

- Keep the Carbon geometry, typography and token usage consistent with the
  existing shell.
- Use the approved artwork in [brand assets](../../assets/branding/README.md)
  for product-owned marks; preserve third-party and server-provided icons.
- Run `python scripts/check_brand_assets.py` from the repository root to check
  deployable logo copies and raster dimensions after a brand update.
- Loading, empty and error states are part of the change, not follow-up work.
- A frontend-only fix must not bypass an authorization check in the backend.

# Read next

- [RPC contract](../architecture/rpc-contract.md)
- [Verification](../testing/verification.md)

# Shared design system

Use [the shared guide](../../docs/design-system/README.md) and canonical CSS
tokens for new UI. Cloud carries a content-pinned snapshot; changes need review
and verification in both products. The standalone design-system homepage lives
in docs/design-system and is not a Panel application route. Literal colors in older views remain migration debt, so a token
change alone does not establish application-wide propagation.

See [ADR 0009](../../docs/adr/0009-shared-design-system.md).
