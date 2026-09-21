# Lane conventions (read this first, every lane)

These rules exist so that several agents can work on `cloud/` in parallel, on
different machines, and still merge cleanly. Violating them is a failed lane even
if the code "works".

## 1. Getting a workspace

Your node has its own clone at `/home/prox/carbon-panel` which is **dirty and on
someone else's branch**. Never work in it directly. Always:

```sh
cd /home/prox/carbon-panel
git fetch localrepo
rm -rf /tmp/cloud-LANE && git worktree prune
git worktree add -b cloud/LANE /tmp/cloud-LANE localrepo/t3code/managed-cloud-rbac-nodes
cd /tmp/cloud-LANE
```

Work only inside `/tmp/cloud-LANE`. When finished:

```sh
gofmt -l ./cloud/... | head            # must be empty
go build ./cloud/... && go vet ./cloud/... && go test ./cloud/...
git add -A
git commit -m "cloud(LANE): <what changed>"
git push localrepo cloud/LANE
```

If the push fails, that is a **failed lane** — report it. Do not leave work only on
your node.

## 2. Hard constraints

1. **No new Go dependencies.** `go.mod`/`go.sum` are shared across lanes and
   concurrent edits conflict. Everything required is already in the module graph
   (GORM + postgres/sqlite drivers + `casbin` + `go-oidc` + `golang-jwt` +
   `connectrpc.com/connect` + `golang.org/x/crypto` + stdlib). If you truly need a
   module that is not present, **stop and report** it in your final answer instead
   of adding it.
2. **Never run `go mod tidy`.** The orchestrator owns `go.mod`.
3. **Never edit `cloud/proto/**` or anything under `pkg/proto/**` or
   `cloud/web/cloud-console/src/lib/proto/**`.** Generated code is owned by the
   orchestrator. If a contract is missing, report it.
4. **Stay inside your write scope.** The scope is listed in your lane spec. If you
   need a change outside it, report the need; do not make the change.
5. **No secrets, ever.** Credentials are read through `cloud/internal/cloud/secrets`.
   No key material in source, tests, fixtures, or examples — use obvious fakes.
6. **Deny by default.** Anything new that authorizes something must default to
   refusing when it cannot decide.
7. **Every mutating action is audited** (the audit lane provides the writer).
8. **The OSS product must keep building.** `go build ./...` and `go test ./...`
   over the whole module must stay green.

## 3. Shared packages you may import (owned by the orchestrator)

```go
import (
    "github.com/athNdev/carbon-panel/cloud/internal/cloud/keys"
    "github.com/athNdev/carbon-panel/cloud/internal/cloud/principal"
)
```

- `principal.Principal{Kind, UserID, OrgID, Role, Email, APIKeyID, NodeID, Permissions}`,
  `principal.WithPrincipal(ctx, p)`, `principal.From(ctx)`, `principal.OrgID(ctx)`.
  `OrgID` is empty only for requests that have not selected a tenant; every
  tenant-scoped storage query must refuse to run without one.
- `keys.*` declares every credential name and which capability needs which keys.
  Read values through the secrets provider; never hardcode a key name as a bare
  string when a constant exists.

Do **not** import another lane's package unless your spec says it is available.
Parallel lanes cannot see each other's code, so cross-lane imports must be agreed
in advance by the orchestrator.

## 4. Package layout (fixed)

```
cloud/internal/cloud/config      # config loading + validation
cloud/internal/cloud/secrets     # secret providers
cloud/internal/cloud/keys        # credential names + capabilities   [orchestrator]
cloud/internal/cloud/principal   # request principal + context       [orchestrator]
cloud/internal/cloud/db          # GORM store, models, migrations, org scoping
cloud/internal/cloud/auth        # Clerk JWT verification, API keys, webhooks
cloud/internal/cloud/rbac        # permission catalog, policy engine, interceptor
cloud/internal/cloud/audit       # audit writer/reader, redaction
cloud/internal/cloud/nodetype    # node type + provider catalog
cloud/internal/cloud/node        # node registry, join tokens, capacity
cloud/internal/cloud/agent       # control-plane side of the agent protocol
cloud/internal/cloud/provider    # provider abstraction (data, not code)
cloud/internal/cloud/provision   # Terraform workspace driver
cloud/internal/cloud/apiserver   # HTTP + Connect-RPC wiring, interceptors
cloud/cmd/cloudcontrold          # control-plane binary
cloud/cmd/cloudnoded             # node agent binary
cloud/cmd/cloudctl               # operator/tenant CLI
cloud/internal/cloud/nodeagent   # node-side agent runtime
```

## 5. Testing standard

- Tests live next to the code as `*_test.go` and must pass with `go test ./cloud/...`
  on a machine with **no** Docker, **no** Postgres, **no** Terraform and **no**
  network. That means:
  - storage tests use the in-memory SQLite driver (`github.com/glebarez/sqlite`,
    already a dependency) — plus a Postgres test that **skips** unless
    `TEST_DATABASE_URL` is set;
  - Clerk tests generate an RSA key, serve a JWKS from `httptest`, and mint tokens
    locally — never call clerk.com;
  - Terraform is exercised through the command runner behind an interface, with a
    fake runner in tests;
  - `t.TempDir()` for anything on disk.
- Every package you add must have meaningful tests: happy path, at least one
  denial/error path, and at least one boundary case.
- Use `t.Parallel()` where safe. Use `github.com/stretchr/testify` (already a
  dependency) for assertions if you like.

## 6. Reporting

Your final answer must contain, in this order:
1. `LANE: <name>`, `BRANCH: cloud/<name>`, `COMMIT: <sha>`.
2. Files added/changed (paths only).
3. Tests: exact command run and its result.
4. Anything you could not do, and why.
5. Any interface you invented that another lane will need to know about.

Keep it under 30 lines. No code dumps.
