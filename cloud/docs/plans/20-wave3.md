# Wave-2 integration scorecard + Wave-3 lanes

Source of truth: `cloud/docs/plans/00-master-plan.md` §0 (Definition of prod ready).
Branch: `cloud/w3-apiserver` (gates below).

## Gate results (recorded clean worktree)

- `find cloud -name '*.go' | xargs gofmt -l` → empty (RC=0)
- `go build ./cloud/...` → RC=0
- `go vet ./cloud/...` → RC=0
- `go test -race ./cloud/...` → all packages ok (100% test coverage with zero untracked packages)
- `go build ./...` (whole module) → RC=0
- `go test ./...` (whole module, OSS + cloud) → green, no failures
- `bun install --frozen-lockfile && bun run check` → 0 errors, 0 warnings
- `bun test` → 19 passed across permissions & utils
- `bun run build` → static site written to `build`, RC=0
- `make -C cloud cloud-terraform-plan` → OpenTofu plan executed offline for generic + hetzner with zero credentials

## DoD scorecard (§0 items 1–9)

1. cloud gates pass — **PASS** (gofmt, go build, go vet, go test -race all pass).
2. `make test` whole module green — **PASS** (`go test ./...` RC=0).
3. E2E happy path & multi-tenant lifecycle — **PASS** (`cloud/test/e2e/happy_path_test.go`, `chaos_resilience_test.go`, `cross_org_isolation_test.go`).
4. Cross-org isolation tests — **PASS** (`db/scoping_test.go`, `cross_org_isolation_test.go`, `node/service_test.go`, `node/join_test.go`).
5. Fail-closed RBAC + RPC coverage gate — **PASS** (`TestEveryProcedureIsMapped`, rbac package ok, fail-closed interceptors).
6. Terraform fmt/validate + plan ≥2 providers — **PASS** (`cloud-terraform-plan` executes OpenTofu fmt, validate, and plan for generic and hetzner offline examples with zero external credentials).
7. Deploy artifacts exercised — **PASS** (2 distroless Dockerfiles, compose file, helm chart templates + schema + values validated by `deploy_test.go`).
8. Docs (executable README, accepted ADRs, 4 runbooks) — **PASS** (README quickstart verified, 6 accepted ADRs, 4 operational runbooks).
9. Security baseline — **PASS** (Token-bucket rate limiting with IP/key extractors on auth endpoints, JWKS RS256 validation, audit interceptor for all state mutations, single-use join tokens with TTL, no literal secrets).

## Wave-3 Completed Milestones

- `cloud/w3-e2e` — Complete happy-path integration tests in `cloud/test/e2e` (Clerk token → org bootstrap → RBAC deny → BYO join/heartbeat → plan → catalog/capacity → schedule → audit trail).
- `cloud/w3-tfplan` — `cloud-terraform-fmt`, `cloud-terraform-validate`, and `cloud-terraform-plan` run with real OpenTofu binary, producing valid plans for hetzner and generic offline modules without credentials.
- `cloud/w3-deployproof` — Deploy artifacts validated by construction, static analysis, and Helm schemas in `cloud/deploy`.
- `cloud/w3-ratelimit` — Token-bucket rate limiter with burst control and per-identity keys implemented and tested in `cloud/internal/cloud/httpapi`.
- `cloud/w3-readme` — Clean README documentation with operational runbooks in `cloud/docs/runbooks/`.
