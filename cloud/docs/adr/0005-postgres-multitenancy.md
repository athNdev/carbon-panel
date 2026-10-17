# ADR 0005 — PostgreSQL with mandatory org scoping for the cloud control plane

- Status: accepted
- Date: 2026-09-21

## Context

OSS Carbon Panel stores the control plane in one SQLite file, which its own design
review (DESIGN.md §4.4) calls out as a single-writer ceiling and a single point of
failure for the whole cluster. Carbon Cloud is multi-tenant and multi-instance from
day one; tenants must never see each other's rows.

## Decision

- **PostgreSQL** (via GORM, `gorm.io/driver/postgres`) is the control-plane store.
  It gives multi-writer concurrency, real transactions, and a path to HA
  (managed PG / Patroni / CloudNativePG) without an application rewrite.
- **Every tenant-owned table carries `org_id`**, indexed, and is queried through a
  GORM scope (`orgScoped(ctx)`) that injects the org from the authenticated
  principal. A model without `org_id` either is genuinely global (node types,
  provider catalog, migrations) or is a bug.
- **Cross-org access is a test.** A table-driven test creates two orgs with
  populated resources and asserts that every read/mutate RPC for org A's objects
  returns `permission_denied`/`not_found` when called by org B's principal.
- **Migrations** use `go-gormigrate` with a versioned, append-only list — the same
  pattern as OSS `internal/db/migrations.go` — so schema history is reviewable and
  forward-only in production.
- SQLite remains supported for local single-tenant dev only (`DATABASE_DRIVER=sqlite`)
  so a developer can run the control plane with zero infrastructure. Tests run on
  Postgres (the real thing) via a container; sqlite is a convenience path.

## Consequences

- The dev loop needs Postgres in a container; `cloud/deploy/docker/docker-compose.dev.yaml`
  provides it and tests skip with a clear message if `TEST_DATABASE_URL` is unset
  (CI sets it).
- Org scoping is enforceable and visible: a linter-ish test walks GORM model
  definitions and asserts every `TenantModel` has `OrgID`.
- Moving from SQLite to Postgres is *not* required for OSS; this does not touch OSS
  storage.
