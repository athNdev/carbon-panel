# Wave-2 integration scorecard + Wave-3 lanes

Source of truth: `cloud/docs/plans/00-master-plan.md` §0 (Definition of prod ready).
Branch: `cloud/integration-w2` (5 merges, zero conflicts; gates below).

## Gate results (recorded 2026-09-21, clean worktree /tmp/cloud-master)

- `find cloud -name '*.go' | xargs gofmt -l` → empty (RC=0)
- `go build ./cloud/...` → RC=0
- `go vet ./cloud/...` → RC=0
- `go test ./cloud/...` → all packages ok (audit, auth, config, db, node,
  nodetype, obs, provider, provision, rbac, secrets, deploy)
- `go build ./...` (whole module) → RC=0
- `go test ./...` (whole module, OSS + cloud) → green, no failures
- `bun install --frozen-lockfile && bun run check` → 0 errors, 0 warnings
- `bun run build` → static site written to `build`, RC=0

## DoD scorecard (§0 items 1–9)

1. cloud gates pass — **PASS** (evidence above).
2. `make test` whole module green — **PASS** (`go test ./...` RC=0).
3. E2E happy path on compose stack — **GAP** (no e2e test outside node_modules).
4. Cross-org isolation tests — **PASS** (`db/scoping_test.go`,
   `node/service_test.go`, `node/join_test.go`; suite green).
5. Fail-closed RBAC + RPC coverage gate — **PASS**
   (`TestEveryProcedureIsMapped`, rbac package ok).
6. Terraform fmt/validate + plan ≥2 providers — **GAP** (6 provider modules
   exist: aws, digitalocean, gcp, generic, hetzner, proxmox; but Makefile
   targets take the SKIP path — no terraform/tofu binary in any lane env,
   no recorded plan output).
7. Deploy artifacts exercised — **GAP (partial)** (2 Dockerfiles, compose file,
   helm chart, cloud-ci.yml all exist; `deploy_test.go` is static/by-construction
   only — no real `docker build` / `compose up` / `helm template` evidence).
8. Docs (executable README, accepted ADRs, 4 runbooks) — **GAP (partial)**
   (README 67 lines, 6 ADRs, 4 runbooks incl. provisioning.md exist;
   README never executed verbatim as written).
9. Security baseline — **GAP (scoped: rate limits)** (JWKS enforcement tests,
   single-use+TTL join tokens, audit interceptor, `TestNoLiteralSecretsInDeploy`
   present; **no rate limiting** on auth endpoints — zero hits for
   rate-limit/throttle in `cloud/internal/cloud`).

## Wave-3 lanes (each: branch + one-line spec)

- `cloud/w3-e2e` — compose-stack happy-path integration test per §0.3
  (Clerk-shaped token → org → RBAC deny → BYO join/heartbeat → plan →
  catalog/capacity → schedule/stream → audit trail).
- `cloud/w3-tfplan` — run `cloud-terraform-fmt` + `cloud-terraform-validate`
  with a real terraform/tofu binary and record `plan` output for ≥2 providers
  (hetzner + generic), no real credentials (§0.6).
- `cloud/w3-deployproof` — real `docker build` of both images, `compose up`
  health check, `helm lint` + `helm template` render evidence (§0.7).
- `cloud/w3-ratelimit` — token-bucket rate limits on auth endpoints with tests
  (§0.9 remainder).
- `cloud/w3-readme` — execute README quickstart verbatim on a clean checkout,
  fix all drift until it runs as written (§0.8 remainder).
