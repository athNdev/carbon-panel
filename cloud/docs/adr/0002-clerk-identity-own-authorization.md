# ADR 0002 — Clerk for identity, our own engine for authorization

- Status: accepted
- Date: 2026-09-21

## Context

Carbon Cloud is multi-tenant. It needs authentication, organizations,
memberships, invitations, SSO, MFA and session management — all things Clerk
provides as a service — plus fine-grained resource authorization that Clerk does
not model (per-node, per-server, per-environment permissions inside an org).

OSS Carbon Panel uses local passwords + optional OIDC and Casbin for RBAC, with a
hand-maintained procedure→permission map that was found fail-**open** in the 2026-09
audit (DESIGN.md §10.1 finding #1).

## Decision

- **Identity: Clerk.** Sign-in flows, user records, organizations, invitations,
  MFA and SSO are Clerk's. The console uses Clerk's frontend SDK. The control
  plane never stores passwords.
- **Verification: Clerk session JWTs, verified locally.** Control plane fetches
  Clerk's JWKS (`CLERK_JWKS_URL` or derived from `CLERK_ISSUER`), caches it, and
  verifies signature + `iss` + `exp`/`nbf` + `azp`. It never calls Clerk per
  request. Algorithm is pinned to RS256; `alg: none` and unknown `kid` are
  rejected. JWKS refresh on unknown `kid`, with a cache TTL and single-flight.
- **Sync: Clerk webhooks** (`organization.created`, `organizationMembership.*`,
  `user.deleted`) keep a local mirror of orgs/memberships for joins and audit, using
  Svix signature verification. The mirror is _derived_ — Clerk stays authoritative;
  a periodic reconcile job repairs drift.
- **Authorization: ours, fail-closed.** Roles come from Clerk claims
  (`org_role`) but every request is authorized against our own policy engine
  (Casbin) with an org scope. Clerk's role is an input, never the decision.
- **Deny by default.** A procedure absent from the three authorization tables is
  rejected. A generated coverage test asserts every registered RPC is mapped to
  exactly one table. This directly closes the OSS finding.

## Consequences

- Local dev needs fake-but-valid Clerk tokens: the test suite generates an RSA key,
  serves a JWKS from `httptest`, and mints tokens. No Clerk account needed to run
  tests, satisfying "API keys will be provided later".
- Role changes made in our console are written to Clerk (for consistency) and
  reflected locally by webhook; the local policy is the enforcement point.
- We own revocation semantics for API keys; sessions are Clerk's.
