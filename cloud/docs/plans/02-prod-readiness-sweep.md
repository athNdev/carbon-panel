# Carbon Cloud — prod-readiness sweep and wave plan

Status: **active**
Owner: orchestrator (`cloud/integration`)
Last updated: 2026-09-21
Supersedes nothing; extends `00-master-plan.md`. Lane rules remain `01-lane-conventions.md`.

This file is the sweep: every surface required for a production, multi-tenant,
billable Carbon Cloud, what already exists, and which lane builds what. It is the
dispatch source for waves 2+.

---

## 1. Integrated state (wave 1, `cloud/integration`)

Branches merged clean (zero conflicts): `cloud/db` 062312d, `cloud/auth` d3549e1,
`cloud/terraform` e01d62a, `cloud/cfg` e5562fd, `cloud/console` b184ac5.

Gates green on the merged tree:

- `gofmt` clean, `go build ./cloud/...`, `go vet ./cloud/...`, `go test ./cloud/...` all pass.
- `cloud/web/cloud-console`: `bun install`, `bun run check` (0 errors/0 warnings),
  `bun run build`, `bun test` (7 pass) all pass.

Landed surfaces:

| Surface | State |
|---|---|
| `cloud/proto/cloud/v1/**` (9 services, 60 RPCs) + Go/TS codegen | done |
| `config` + `secrets` (capability-gated, no boot-time credential) | done |
| `db` (GORM store, models, migrations, org scoping, seed, SQLite tests) | done |
| `auth` (Clerk RS256 verify + JWKS, API keys, Svix webhooks) | done |
| `keys` + `principal` | done |
| `terraform/modules/{node/{hetzner,aws,gcp,digitalocean,proxmox,generic},cloudinit}` + `stacks/control-plane` | done |
| `web/cloud-console` SPA — 13 routes, real Connect-RPC client, Clerk, Carbon styling | done (UI) |

`db` already ships the models wave 2 needs (`Node`, `NodeType`, `JoinToken`,
`Provision`, `RoleBinding`, `AuditEvent`, `Workload`, `WorkloadEvent`,
`NodeCapacity`) with JSON helpers and `Store.Org(ctx)` scoping. Wave-2 lanes add
behaviour over those models; they must not reshape them.

---

## 2. Gap matrix vs `00-master-plan.md` §0

| # | Definition-of-prod-ready | State | Owner |
|---|---|---|---|
| 1 | `cloud/` builds/vets/tests on clean checkout | **partial** — Go green, no `cloud/Makefile`/CI | w2-deploy |
| 2 | whole-module `make test` green | **partial** — cloud subpackages green; no `cloud` Make targets | w2-deploy |
| 3 | happy-path e2e against compose stack | **missing** | w3-apiserver, w3-noded, w4-e2e |
| 4 | cross-org isolation proven by tests | **partial** — db-level only | w2-rbac, w3-apiserver, w4-e2e |
| 5 | RBAC fail-closed + every RPC mapped | **missing** | **w2-rbac** |
| 6 | terraform fmt/validate, plan ≥2 providers | **partial** — modules+stacks exist, no Go driver | **w2-provision** |
| 7 | deploy artifacts exercised (docker/compose/helm) | **missing** | **w2-deploy** |
| 8 | docs: README, ADRs, 4 runbooks | **partial** — 6 ADRs; 1 of 4 runbooks | w2-deploy, w4-docs |
| 9 | security baseline (audit all mutations, join token TTL, rate limits, JWKS) | **partial** — JWKS+keys done; audit/join/ratelimit missing | **w2-audit**, **w2-node**, w3-apiserver |
| 10 | node subsystem: registry, types, capacity, join | **missing** | **w2-node** |
| 11 | provisioning driver + provider abstraction | **missing** (Go side) | **w2-provision** |
| 12 | node agent + `cloudnoded` | **missing** | w3-noded |
| 13 | `cloudcontrold` + Connect-RPC server wiring | **missing** | w3-apiserver |
| 14 | `cloudctl` operator/tenant CLI | **missing** | w3-ctl |
| 15 | observability (`obs`: metrics/logs/traces) | **missing** | **w2-deploy** |
| 16 | **SaaS commercial layer** (plans, subscription, metering, quota, invoices) | **missing** | w4-saas |
| 17 | invites / email outbox / notification webhooks-out | **missing** | w4-saas |
| 18 | console: real data polish, Playwright smoke, a11y | **partial** — UI complete, no browser smoke | w3-console |
| 19 | security hardening + independent review | **missing** | w4-hardening |

Gap 16 is the deliberate extension beyond `00-master-plan.md`: a hosted product
that bills per managed node and per BYO node is not "a full-fledged SaaS" without
plans, metering, quota enforcement and invoices. It is scoped behind provider
interfaces so it works with **no** external keys (ADR 0006) and lights up when the
operator supplies them.

---

## 3. Wave plan

- **Wave 2 (dispatch now, 5 lanes)** — `w2-rbac`, `w2-audit`, `w2-node`,
  `w2-provision`, `w2-deploy`. All pure-Go/spec work over already-merged wave-1
  packages. Disjoint write scopes; no cross-lane imports.
- **Wave 3 (4 lanes)** — `w3-apiserver` (+`cmd/cloudcontrold`), `w3-noded`
  (+`agent`, `nodeagent`, `cmd/cloudnoded`), `w3-ctl`, `w3-console`.
  Depends on wave-2 interfaces in §5.
- **Wave 4 (3 lanes)** — `w4-saas` (billing/metering/quota), `w4-e2e` (compose
  happy path + cross-org table tests), `w4-docs` (runbooks, README, security pass),
  then an independent `@oracle` review before prod sign-off.

**Base for wave 2:** `localrepo/cloud/integration` @ the sha carrying this file.
**Branch per lane:** `cloud/w2-<lane>`. Push is mandatory; an unpushed lane is failed.

Wave-2 lanes **may** import `config`, `secrets`, `db`, `auth`, `keys`,
`principal` (all merged). They must not import another wave-2 lane.

---

## 4. Wave-2 lane specs

### Lane `w2-rbac` — authorization catalogue, policy engine, fail-closed interceptor

Write scope: `cloud/internal/cloud/rbac/**`

Depends on (merged): `principal`, `keys`, generated `pkg/proto/cloud/v1`.
Must NOT import `db` (policy storage arrives through an interface; the apiserver
lane implements it over `db.RoleBinding`).

Deliverables:

1. **Permission catalogue.** `Permission` is a lowercase dotted string
   (`nodes.create`, `nodes.read`, `workloads.exec`, `org.members.write`,
   `keys.manage`, `audit.read`, `provision.apply`, …). Roles, each a set of
   permissions, exactly matching the Clerk role normalisation in `auth`:
   `owner > admin > operator > billing > viewer`.
2. **Complete RPC → permission mapping.** `PermissionForProcedure(procedure string)
   (Permission, bool)` where `procedure` is the Connect name
   `"/cloud.v1.NodeService/ListNodes"`. Every one of the 60 RPCs in the 9 proto
   services must map, including streaming RPCs and `AgentService.JoinNode`.
3. **Explicit public allowlist.** `PublicProcedures() []string` — unauthenticated
   procedures only (`JoinNode`, `SystemService/GetBuildInfo`, `SystemService/GetCapabilities`).
   Everything not in it requires authentication **and** a mapped permission.
   Unknown/absent mapping is a denial, never a bypass.
4. **Engine.** `New(Options) (*Engine, error)` with Casbin model (RBAC with
   resource-scoped bindings) + role policies; `Engine.Allowed(ctx, principal.Principal,
   Permission) (bool, error)`. Resource-scoped bindings:
   `BindingSource interface { Bindings(ctx context.Context, orgID string) ([]Binding, error) }`
   so the apiserver can back it with `db.RoleBinding` without rbac importing db.
   Bindings are org-scoped and may narrow a role to one `ResourceType`/`ResourceID`.
5. **Interceptor.** `Interceptor(engine *Engine, opts Options) connect.Interceptor`
   covering **unary and streaming** handlers. Order: authenticate → resolve
   procedure → check allowlist/mapping → evaluate policy → attach audit metadata to
   the context for the audit lane → next. Denials return `connect.CodePermissionDenied`
   with no detail leakage. Errors from the engine deny.
6. **Introspection helpers** for the console/tests: `Permissions() []Permission`,
   `PermissionsForRole(role string) []Permission`, `AllProcedures() []string`
   (enumerated from generated service descriptors).

Tests (required): `TestEveryProcedureIsMapped` — fails if any descriptor procedure
has no permission; `TestUnknownProcedureDenied`; `TestPublicAllowlistIsExhaustive`
(each entry must exist in descriptors); `TestRoleMatrix` (table-driven, every role ×
catalogue, asserting owner ⊇ admin ⊇ operator and viewer cannot mutate);
`TestBindingScoping` (a binding on node A must not authorize node B);
`TestEngineErrorDenies`; streaming-denial test.

### Lane `w2-audit` — append-only audit writer/reader with redaction

Write scope: `cloud/internal/cloud/audit/**`

Depends on (merged): `db` (owns `db.AuditEvent`), `principal`, `keys`.

Deliverables:

1. `Event` (actor kind/user/API key, org, action, resource type/id, result, IP,
   user agent, detail) and `NewEvent(ctx, req metadata) Event` deriving actor + org
   from `principal.From(ctx)`.
2. `ActionName(procedure string) string` — deterministic `"<service>.<verb>"`
   (`NodeService/DeleteNode` → `nodes.delete`). `IsMutating(procedure string) bool`
   from an explicit table, so "every mutating action is audited" is testable.
3. `Store` interface `Append(ctx, Event) error` / `List(ctx, Filter) ([]Event, Result)`
   / `Get(ctx, id) (Event, error)`, plus `NewGormStore(store *db.Store) Store` that
   reads/writes **through** `store.Org(ctx)` and refuses without an org.
4. `Redact(Event) Event` — removes secret-shaped keys (`token`, `secret`, `password`,
   `key`, `authorization`, `join`, raw credential values) from detail, and truncates
   oversized detail. Must be applied on the write path, not the read path.
5. `Interceptor(store Store, opts Options) connect.Interceptor` — writes exactly one
   event per mutating RPC, success or failure, and **must not** fail the RPC if the
   audit write fails only in the sense of: audit write failure is surfaced via
   `opts.OnError` and increments a counter, but a *missing* write for a mutating
   action is treated as an error the operator can alert on (document the choice).
6. `Filter` supports org, actor, action, resource, result, time range, paging.

Tests (required): redaction removes every secret-shaped key (table-driven, assert
the secret string is absent from the stored detail); mutating-action table covers
every mutating procedure in the catalogue; org scoping (org A cannot read org B
events); append-only (no update/delete API); failure-path event records the error
class without leaking it; no-org write refuses.

### Lane `w2-node` — node-type catalog, node registry, join tokens, capacity

Write scope: `cloud/internal/cloud/nodetype/**`, `cloud/internal/cloud/node/**`

Depends on (merged): `db` (`db.Node`, `db.NodeType`, `db.JoinToken`,
`db.NodeCapacity`), `principal`, `keys`, `secrets` (join-token HMAC pepper).

Deliverables:

1. **nodetype:** `Catalog` with `List(ctx) ([]db.NodeType, error)`,
   `Get(ctx, id) (db.NodeType, error)` (global table — `Store.Unscoped()`),
   `EnsureSeeded(ctx) error` idempotently seeding `nano, small, medium, large, xlarge`
   with vcpu/ram/disk/price and per-provider instance types for
   `hetzner, aws, gcp, digitalocean, proxmox, generic`; `InstanceType(kind, provider)
   (string, bool)`; custom types allowed (`Upsert`).
2. **node registry:** `Service` with `Register`, `Heartbeat`, `Get`, `List`,
   `Update`, `Drain`, `Resume`, `Delete`. Register/heartbeat assign an agent
   fingerprint and `LastHeartbeat`; a node silent beyond `StaleAfter` reports
   `offline` on read without mutating the row. All access org-scoped.
3. **join tokens:** HMAC-signed, org- and expiry-bound, single-use, revocable.
   `Issue(ctx, req) (secret string, tok db.JoinToken, err error)` (secret shown
   once, only its hash stored), `List`, `Revoke`, `Redeem(ctx, secret) (db.JoinToken,
   error)` enforcing signature, org, TTL, revocation and single-use atomically.
   Typed errors: `ErrTokenUnknown`, `ErrTokenExpired`, `ErrTokenRevoked`,
   `ErrTokenRedeemed`. Secret format `ccj_<id>.<hmac-base64url>`; pepper from
   `secrets` via a `keys` constant, never a literal.
4. **capacity:** `NodeCapacity` accounting — sum of allocations per node, accept
   placement only when the remaining capacity covers the request, reject
   over-commit, honour drain state, and account BYO vs managed identically.
   Typed errors `ErrCapacityExceeded`, `ErrNodeDraining`, `ErrNodeNotFound`.

Tests (required): join token happy path → redeem once, second redeem fails
`ErrTokenRedeemed`; expired token fails; revoked fails; tampered signature fails;
org mismatch fails; concurrency test proving two goroutines cannot both redeem the
same token; capacity accepts/rejects at the exact boundary and after drain;
heartbeat staleness boundary; node-type seeding idempotent; no-org access refuses.

### Lane `w2-provision` — provider abstraction and Terraform workspace driver

Write scope: `cloud/internal/cloud/provider/**`, `cloud/internal/cloud/provision/**`

Depends on (merged): `db` (`db.Provision`, `db.Node`), `config`
(`config.Provisioner`), `secrets` + `keys` (provider credential names),
`nodetype` is NOT available — take the node type as a plain struct parameter.

Deliverables:

1. **provider (data, not code):** `Descriptor{Name, Title, Regions []Region,
   StorageBackends, SupportsUserData, CredentialKeys []string, Notes}` and
   `Registry` with `Get(name)`, `List()`. Six descriptors: `hetzner`, `aws`, `gcp`,
   `digitalocean`, `proxmox`, `generic`. `CredentialKeys` list `keys.*` constants.
   A provider whose credentials are absent is `Available() == false`, with the
   missing key names reported — it is listed, not hidden.
2. **provision driver:** `Driver` interface `Init`, `Plan`, `Apply`, `Destroy`,
   `Outputs` over a per-provision workspace (`Options.Root`, workspace dir named by
   `db.Provision.Workspace`), state backend `local|s3` from config, `.tfvars`
   rendered from provider + region + node type + join token bootstrap, plan stored
   with `PlanHash` and a diffable `PlanDiff`.
3. **command runner behind an interface** (`Runner`) so tests use a fake — no
   Terraform, no network, no credentials in tests. `ExecRunner` is the real one.
4. **apply gated by plan hash:** `ErrPlanRequired` / `ErrPlanChanged` when applying
   without a plan or after a re-plan changed the hash.
5. **`Service`** orchestrating `CreateProvision` → plan → apply → node row →
   outputs, persisting status transitions on `db.Provision`, with `StreamLogs`
   support (bounded ring buffer + subscriber fan-out, replayable while running).
6. **cloud-init bootstrap** rendered with the join token, control-plane URL and
   node agent image/version.

Tests (required): fake runner — full lifecycle plan→apply→destroy; apply without
plan rejected; apply after plan changed rejected; `terraform fmt -check`-equivalent
formatting assertion is out of scope (no terraform binary) but every module's
`main.tf` must be rendered/validated as text by the tfvars renderer test; provider
registry lists 6 and marks uncredentialed ones unavailable with the missing key
names; workspace path traversal rejected; log ring buffer replay + live fan-out;
state-backend config local and s3 both produce the expected `-backend-config`.

### Lane `w2-deploy` — deploy artifacts, CI, Makefile, observability

Write scope: `cloud/deploy/**`, `cloud/Makefile`, `.github/workflows/cloud-ci.yml`,
`cloud/internal/cloud/obs/**`, `cloud/docs/runbooks/**`

Depends on (merged): `config` (`config.Server`, `config.Telemetry`).

Deliverables:

1. `cloud/deploy/docker/Dockerfile.cloudcontrold` and `Dockerfile.cloudnoded` —
   multi-stage, distroless/static final image, non-root user, no secrets baked in,
   `-trimpath`, version stamped by ldflags.
2. `cloud/deploy/compose/docker-compose.yml` — Postgres + controld + noded +
   console, all credentials obvious fakes, healthchecks, named volumes, no host
   network. `cloud/deploy/compose/.env.example`.
3. `cloud/deploy/helm/carbon-cloud/**` — chart with values schema, deployments,
   services, ingress, secret refs (no literal secrets), `helm template` and
   `helm lint` render for default values.
4. `.github/workflows/cloud-ci.yml` — gofmt (find-based, not `./cloud/...`),
   `go build/vet/test ./cloud/...`, whole-module `go build ./...`, then
   `bun install --frozen-lockfile && bun run check && bun run test && bun run build`
   in `cloud/web/cloud-console`. Cache Go and bun. Must pass on the integration tree.
5. `cloud/Makefile` with `cloud-test`, `cloud-check`, `cloud-fmt`,
   `cloud-terraform-fmt`, `cloud-terraform-validate`, `cloud-compose-up/down`,
   `cloud-helm-template`, plus a `cloud-ci` aggregate. Root `Makefile` untouched.
6. `cloud/internal/cloud/obs/**` — Prometheus registry + `/metrics` handler
   (RED metrics: request count/duration/errors by procedure), structured `slog`
   logging with request-scoped fields (org id, actor id, procedure, trace id), and a
   tracer interface with a no-op default so nothing is required at boot.
7. Runbooks: `cloud/docs/runbooks/control-plane-outage.md`,
   `node-join-failure.md`, `key-rotation.md` (provisioning.md already exists).

Tests (required): obs — metrics registration is idempotent (no double-register
panic), handler serves `text/plain` with the expected metric families, redaction
of org id in logs is configurable, no-op tracer works. YAML/deploy artifacts are
validated by `make cloud-helm-template` where helm exists and by a YAML-parse test
otherwise.

---

## 5. Contracts wave 3 will wire against

```go
// rbac
type Engine struct{ /* ... */ }
func New(Options) (*Engine, error)
func (e *Engine) Allowed(ctx context.Context, p principal.Principal, perm Permission) (bool, error)
func PermissionForProcedure(procedure string) (Permission, bool)
func PublicProcedures() []string
func AllProcedures() []string
func Interceptor(*Engine, Options) connect.Interceptor
type BindingSource interface{ Bindings(ctx context.Context, orgID string) ([]Binding, error) }
type Binding struct{ SubjectType, SubjectID, ResourceType, ResourceID string; Permissions []Permission }

// audit
type Store interface {
    Append(ctx context.Context, e Event) error
    List(ctx context.Context, f Filter) ([]Event, string, error)
    Get(ctx context.Context, id string) (Event, error)
}
func NewGormStore(*db.Store) Store
func Interceptor(Store, Options) connect.Interceptor
func IsMutating(procedure string) bool
func ActionName(procedure string) string

// nodetype
type Catalog struct{ /* ... */ }
func NewCatalog(*db.Store) *Catalog
func (c *Catalog) EnsureSeeded(ctx context.Context) error
func (c *Catalog) List(ctx context.Context) ([]db.NodeType, error)

// node
type Service struct{ /* ... */ }
type JoinTokenService struct{ /* ... */ }
func NewService(deps Deps) *Service
func NewJoinTokenService(deps Deps) *JoinTokenService
type Capacity struct{ /* ... */ }

// provider
type Descriptor struct{ Name, Title string; Regions []Region; CredentialKeys []string; /* ... */ }
func List() []Descriptor
func Get(name string) (Descriptor, bool)

// provision
type Driver interface{ Init, Plan, Apply, Destroy, Outputs }
type Runner interface{ Run(ctx context.Context, dir string, args ...string) (string, error) }
type Service struct{ /* ... */ }
func NewService(Options) (*Service, error)

// obs
func NewMetrics() *Metrics
func (m *Metrics) Handler() http.Handler
func NewLogger(cfg config.Telemetry) *slog.Logger
```

Wave-3 lanes must not re-implement any of the above; if a signature is missing they
report it instead of editing another lane's package.

---

## 6. Wave-3 / wave-4 preview

- **w3-apiserver** — `cloud/internal/cloud/apiserver/**` + `cloud/cmd/cloudcontrold/**`:
  connect handler registry for all 9 services over `db` + `node` + `provision` +
  `audit` + `rbac` + `auth`; `/healthz`, `/readyz`, `/metrics`; interceptor chain
  (recovery → obs → auth → rbac → audit); API-key authentication path; Clerk webhook
  endpoint; org sync from webhooks; rate limits on auth endpoints; graceful
  shutdown. Implements `rbac.BindingSource` over `db.RoleBinding`.
- **w3-noded** — `cloud/internal/cloud/agent/**` (control-plane side),
  `cloud/internal/cloud/nodeagent/**`, `cloud/cmd/cloudnoded/**`: bidirectional
  agent stream, join → identity, heartbeat, workload ops (create/start/stop/delete/
  exec/logs) via the existing `internal/docker` client, route sync via the existing
  proxy route syncer, buffered status + reconnect with backoff across control-plane
  outage.
- **w3-ctl** — `cloud/cmd/cloudctl/**`: login, orgs, nodes, join tokens, node types,
  provisions (plan/apply/destroy/logs), workloads, api keys, audit tail.
- **w3-console** — finish real-data wiring, error/empty/loading states, RBAC-gated
  nav, a11y pass, Playwright smoke (sign-in mocked, API faked).
- **w4-saas** — `cloud/internal/cloud/billing/**` (plan catalog, subscription state,
  usage metering per node-hour/workload-hour, quota enforcement at placement time,
  invoice provider interface with a fake + Stripe-shaped adapter that stays
  **disabled** without keys), `cloud/internal/cloud/notify/**` (email outbox +
  outbound webhooks, provider interface, no-op default), invite acceptance flow.
- **w4-e2e** — compose-backed happy path (fake provider credentials), table-driven
  cross-org isolation across every RPC, RBAC deny matrix, join-token replay,
  capacity rejection.
- **w4-docs** — README quick start executable as written, remaining ADRs, security
  checklist, status/metrics docs; then independent `@oracle` review and sign-off.
