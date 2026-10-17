# Lane `db` — PostgreSQL store, models, migrations, org scoping

Branch: `cloud/db`
Write scope: `cloud/internal/cloud/db/**`, `cloud/docs/plans/11-lane-db.md` (this file,
only to append a "notes" section if needed)

Depends on: `cloud/internal/cloud/config` (see `10-lane-cfg.md` — being written in
parallel). **Because it is written in parallel and you cannot import it yet**, take a
narrow input instead:

```go
// cloud/internal/cloud/db/db.go
type Options struct {
    Driver          string // "postgres" | "sqlite"
    DSN             string
    MaxOpenConns    int
    MaxIdleConns    int
    ConnMaxLifetime time.Duration
    AutoMigrate     bool
}
func Open(opts Options) (*Store, error)
```

The `cfg` lane (or the orchestrator at integration time) adapts `config.Database`
into `db.Options`. Do **not** import `config` — this keeps the two lanes parallel.

## Schema

Tables (GORM models, snake_case tables, string UUID primary keys generated with
`github.com/google/uuid`, `CreatedAt`/`UpdatedAt` managed by GORM where sensible):

| model | table | key fields |
|---|---|---|
| `Org` | `orgs` | `id`, `name`, `slug` (unique), `clerk_org_id` (unique, nullable), `plan`, timestamps |
| `Member` | `members` | `id`, `org_id` (idx), `user_id` (idx), `email`, `display_name`, `role`, `status`, unique(`org_id`,`user_id`) |
| `ApiKey` | `api_keys` | `id`, `org_id` (idx), `name`, `prefix` (idx), `hash` (unique), `permissions` (JSON text), `created_by`, `last_used_at`, `expires_at`, `revoked_at`, timestamps |
| `JoinToken` | `join_tokens` | `id`, `org_id` (idx), `name`, `node_type_id`, `origin`, `provision_id`, `secret_hash` (unique), `expires_at`, `used_at`, `used_by_node_id`, `revoked_at`, timestamps |
| `Node` | `nodes` | `id`, `org_id` (idx), `name`, `origin`, `provider`, `node_type_id`, `region`, `status`, `hostname`, `public_ip`, `private_ip`, `capacity` (JSON), `labels` (JSON), `agent_fingerprint` (unique, nullable), `provision_id`, `is_system`, `draining`, `last_heartbeat`, timestamps, unique(`org_id`,`name`) |
| `NodeType` | `node_types` | `id` (text pk, e.g. "small"), `name`, `vcpu`, `ram_mb`, `disk_gb`, `monthly_price_usd`, `description`, `sort_order`, `enabled`, `instance_types` (JSON) |
| `Provision` | `provisions` | `id`, `org_id` (idx), `name`, `provider`, `region`, `node_type_id`, `status`, `workspace` (unique), `plan_summary`, `plan_diff`, `plan_hash`, `outputs` (JSON), `error`, `node_id`, `created_by`, timestamps |
| `Workload` | `workloads` | `id`, `org_id` (idx), `node_id` (idx), `name`, `spec` (JSON), `status`, `container_id`, `host_port`, `hostname`, `status_detail`, `created_by`, timestamps, unique(`org_id`,`name`) |
| `WorkloadEvent` | `workload_events` | `id`, `workload_id` (idx), `org_id` (idx), `kind`, `message`, timestamps |
| `AuditEvent` | `audit_events` | `id`, `org_id` (idx), `actor_user_id`, `actor_api_key_id`, `action`, `resource_type`, `resource_id`, `result`, `detail_json`, `ip`, `user_agent`, `created_at` (indexed) |
| `RoleBinding` | `role_bindings` | `id`, `org_id` (idx), `subject_type`, `subject_id`, `resource_type`, `resource_id`, `permissions` (JSON), timestamps |

JSON columns are `datatypes`-free: store as `string` with `gorm:"type:text"` and
provide typed accessors on the model (`func (n *Node) Capacity() NodeCapacity`).
Do **not** add the `gorm.io/datatypes` module — it is not a dependency.

`Org`, `Member`, `ApiKey`, `JoinToken`, `Node`, `Provision`, `Workload`,
`WorkloadEvent`, `AuditEvent`, `RoleBinding` are tenant-owned and **must** carry
`OrgID`. `NodeType` is global.

## Org scoping (the security-critical part)

```go
// Returns a query already constrained to the caller's org.
// It MUST fail when the context carries no org.
func (s *Store) Org(ctx context.Context) (*gorm.DB, error)

var ErrNoOrg = errors.New("db: no org in context")

// Unscoped escape hatch for genuinely global tables and for migrations.
func (s *Store) Unscoped() *gorm.DB
```

- `Org` reads `principal.OrgID(ctx)` and returns a `*gorm.DB` with
  `Where("org_id = ?", orgID)`, erroring with `ErrNoOrg` when empty. This is the
  only sanctioned way to reach tenant tables in later lanes — make it hard to get
  wrong: give every tenant model a marker method `IsTenantOwned() bool { return true }`
  and add a test that reflects over the registered models and fails if a model
  with an `OrgID` field does not implement it (or vice versa).
- A cross-org test: create org A and org B, insert a row in each, assert
  `Org(ctxA)` never returns org B's row and cannot update it (updates must include
  the org predicate).

## Migrations

Use `github.com/go-gormigrate/gormigrate/v2` exactly like OSS
`internal/db/migrations.go`: an append-only `[]*gormigrate.Migration` list with
string ids, `AutoMigrate: true` applied inside each migration.
`func (s *Store) Migrate() error` must be idempotent (running twice is a no-op) —
test that.

## Seeding

Default node type catalog and provider mappings (used by the `terraform` and
`nodetype` lanes; the table is yours, the values are shared):

| id | vcpu | ram_mb | disk_gb | monthly USD |
|---|---|---|---|---|
| `nano` | 1 | 2048 | 20 | 6 |
| `small` | 2 | 4096 | 40 | 12 |
| `medium` | 4 | 8192 | 80 | 24 |
| `large` | 8 | 16384 | 160 | 48 |
| `xlarge` | 16 | 32768 | 320 | 96 |

`instance_types` JSON per type, at minimum:
- hetzner: nano `cx22`, small `cx32`, medium `cx42`, large `cx52`, xlarge `ccx33`
- aws: nano `t3.small`, small `t3.medium`, medium `t3.large`, large `m6i.xlarge`, xlarge `m6i.2xlarge`
- gcp: nano `e2-small`, small `e2-medium`, medium `e2-standard-2`, large `e2-standard-4`, xlarge `e2-standard-8`
- digitalocean: nano `s-1vcpu-2gb`, small `s-2vcpu-4gb`, medium `s-4vcpu-8gb`, large `s-8vcpu-16gb`, xlarge `s-16vcpu-32gb`
- proxmox/generic: `custom` (operator supplies the machine)

`func (s *Store) SeedDefaults(ctx context.Context) error` must be idempotent
(upsert by id, never clobber operator edits to price/enabled).

## Tests

- `TestOpenSQLiteMemory` helper: `func OpenTest(t *testing.T) *Store` using
  `github.com/glebarez/sqlite` with `file::memory:?cache=shared` (or a temp file)
  and auto-migration. Every test uses it.
- A Postgres test that `t.Skip`s unless `TEST_DATABASE_URL` is set, and that runs
  the same migration + org-scoping assertions against it.
- Migration idempotency, unique constraints (org slug, node name per org,
  workload name per org), `ErrNoOrg`, tenant-marker reflection test.

## Acceptance

```sh
gofmt -l ./cloud/... # empty
go build ./cloud/... && go vet ./cloud/... && go test ./cloud/...
git diff --stat go.mod go.sum   # only gorm.io/driver/postgres / pgx may flip indirect->direct
```

Report per conventions §6.
