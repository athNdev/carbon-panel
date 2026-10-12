# ADR 0002 — SvelteKit navigation always goes through resolve()

- Status: accepted
- Date: 2026-09-25
- Plane: MINE-154
- PR: projects/carbon-panel#1

## Context

Hard-coded `href="/..."` / `goto('/...')` break under a subpath
`base`. The `svelte/no-navigation-without-resolve` rule now enforces
`resolve()` from `$app/paths`, and the typed `resolve()` only accepts
known route IDs — generic `string` props do not typecheck.

## Decision

- Page-level internal links/gotos use `resolve()` with literals or
  template literals (both infer into the route union); this also fixed
  two literal `href="/servers/{server.id}"` anchors that never
  interpolated.
- Generic wrapper components (`CarbonButton`, `CarbonTile`,
  `CarbonBreadcrumbs`) keep plain `href` props with a narrow
  `eslint-disable` (a generic string cannot satisfy typed `resolve()`);
  their call sites pass `resolve()`d literals.
- `resolve()` is inapplicable — with a scoped disable and justification
  comment — for: external URLs (`resolve()` throws on non-absolute
  pathnames), the backend `/api/v1/openapi.yaml` endpoint (not a route),
  and caller-provided generic hrefs.
- `{#each}` blocks are all keyed; `unknown` + `instanceof Error`
  narrowing replaces `any` at JSON/catch boundaries
  (`ManifestJson` helpers in `manifest-inspector.ts`).

## Consequences

- `bun run lint` (prettier + eslint) is green and enforced in CI
  (`test-frontend` job), so regressions fail fast.
- Component authors: new `href` props must document whether the caller
  or the component resolves; the lint rule catches violations.
