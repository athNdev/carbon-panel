# ADR 0009: Shared design-system foundations

Status: Proposed

## Context

Carbon Panel and Carbon Cloud have related Svelte interfaces but independently
maintained color and interaction foundations. The products have separate
repositories and Cloud must build and deploy without a Panel checkout.

## Decision

The Panel repository owns the shared design-system guide and canonical CSS
tokens in its frontend source. Both products consume the same token contract.
The design-system homepage is a standalone repository asset, separate from
both applications and their authentication or runtime builds.
Cloud commits a content-hash-verified snapshot and a maintainer sync tool;
Cloud's normal build and tests use only its own checkout. A sync can be checked
against a selected source file during coordinated branch work or updated from a
full upstream commit after it is published. The content hash identifies the
exact snapshot; a source revision is recorded when available.

Components remain product-owned Svelte implementations until a separately
reviewed shared-component distribution is justified. The guide documents their
contracts and adoption gaps; a changed token affects only consumers that use it.
Existing direct color literals are migration debt, not evidence of propagation.

## Consequences

- A reviewed token change must be propagated and verified in both products.
- The local lock detects a modified snapshot but not an upstream update. An
  explicit source comparison is needed to check for drift against upstream.
- No package registry, sibling checkout, network access, or moving branch is a
  dependency of a production build. The documented remote sync uses a full
  commit SHA and only runs when a maintainer requests an update.
- Token and guide updates do not by themselves prove accessibility, visual or
  behavioral parity. Review actual affected screens and states.
