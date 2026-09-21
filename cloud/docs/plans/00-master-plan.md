# Carbon Cloud — Master Development Plan

Status: **active**
Owner: orchestrator (`t3code/managed-cloud-rbac-nodes`)
Last updated: 2026-09-21

This document is the single source of truth for what gets built, in what order,
and what "done" means. Lane specs below are written to be handed to a subagent
verbatim. Items marked **[orchestrator]** are kept by the orchestrator because
they are cross-cutting or high-risk.

---

## 0. Definition of "prod ready"

Carbon Cloud is prod ready when **all** of the following are true and evidenced:

1. `go build ./...`, `go vet ./...`, `go test ./...` and `bun run check && bun run build`
   pass for `cloud/` on a clean checkout.
2. `make test` (whole module, OSS + cloud) is green — adding `cloud/` must not
   break the OSS suite.
3. A full happy path runs end-to-end against real services with fake provider
   credentials (a `docker compose` stack), verified by an integration test:
   - Clerk-shaped session token accepted → principal resolved with `org_id` + role
   - org created; RBAC enforced on every RPC (deny → permission denied)
   - BYO node registers via join token, heartbeats, appears online
   - managed node is `plan`-ed (Terraform plan, no apply needed for the test)
   - node type catalog listed; capacity accounting accepts/rejects placement
   - workload scheduled onto a node and status streamed back
   - audit log records every state-changing action with actor + org
4. Cross-org isolation is proven by tests: an actor in org A cannot read or mutate
   any object owned by org B via any RPC (automated, table-driven).
5. RBAC is fail-closed and there is a CI gate asserting every registered RPC maps
   to exactly one authorization table (mirrors OSS finding #1 in DESIGN.md §10).
6. Terraform: `terraform fmt -check`, `terraform validate` pass for every module;
   a `terraform plan` is produced for at least 2 providers (one public cloud, one
   generic/on-prem) with no real credentials.
7. Deploy artifacts exist and are exercised: Dockerfiles build, compose stack comes
   up, Helm chart renders (`helm template`) and lints.
8. Docs: README quick start is executable as written; every ADR accepted; runbooks
   for control-plane outage, node join failure, provisioning failure, key rotation.
9. Security baseline: no secret in the repo, secrets via the provider interface,
   JWKS verification enforced (no `alg=none`, no skipped `azp`), join tokens
   single-use + TTL, all mutating actions audited, rate limits on auth endpoints.

Anything not meeting 1–9 is not shipped.

---

## 1. Architecture summary

```
                    ┌──────────────────────────────────────────────┐
   Clerk (SaaS) ────│ cloud-console (SvelteKit + Carbon)           │
   JWKS / webhooks  │  sign-in, org switcher, nodes, RBAC, keys    │
                    └───────────────┬──────────────────────────────┘
                                    │ Connect-RPC (JSON/HTTP)
                    ┌───────────────▼──────────────────────────────┐
                    │ cloudcontrold  (control plane)               │
                    │  auth (Clerk JWT) → RBAC (fail-closed)       │
                    │  orgs · nodes · node types · workloads       │
                    │  provisioner (Terraform) · audit · api keys  │
                    │  PostgreSQL                                  │
                    └───────┬───────────────────────┬──────────────┘
                            │ agent stream (mTLS)   │ terraform apply
              ┌─────────────▼──────────┐   ┌────────▼─────────────────┐
              │ cloudnoded  (BYO node) │   │ managed node             │
              │  Docker socket,        │   │ (Hetzner/AWS/GCP/DO/     │
              │  workload lifecycle    │   │  Proxmox/generic)        │
              └────────────────────────┘   │  + cloudnoded via cloud-init
                                           └──────────────────────────┘
```

**Control plane / data plane split.** Control plane owns identity, tenancy,
authorization, node registry, capacity, provisioning and audit. Data plane owns
Minecraft containers, proxy routing and metrics. Nodes always dial **out** to the
control plane; no inbound firewall rule is required to add a node.

---

## 2. Phases

| Phase | Deliverable | Gate |
|---|---|---|
| P0 | Plans + ADRs + contracts | `buf lint` clean, ADRs accepted |
| P1 | Control-plane foundation (config, secrets, PG, migrations, Clerk auth, RBAC, audit, HTTP/RPC server) | unit + integration tests green, fail-closed gate test exists |
| P2 | Node subsystem (registry, join tokens, agent protocol, heartbeat, capacity, node types) | BYO join e2e test green |
| P3 | Provisioner + Terraform (provider abstraction, modules, plan/apply, cloud-init) | `plan` produced for ≥2 providers |
| P4 | Workload placement + node agent execution | server scheduled on node, status streamed |
| P5 | Console (Clerk, orgs, nodes, BYO wizard, managed provisioning, RBAC admin, API keys) | `check` + `build` green, Playwright smoke |
| P6 | Deploy + CI + observability + hardening | compose up, helm template, CI green, security checklist |
| P7 | Docs, runbooks, prod sign-off | definition-of-done §0 all evidenced |

Phases P1–P4 have internal parallelism (see lanes). P5 depends on P1 contracts.
P6 depends on P1–P5 artifacts existing.

---

## 3. Lane specs

Each lane is a bounded work package with an explicit write scope, an owner
(orchestrator or subagent), and acceptance criteria. Lanes within a phase can run
in parallel when their write scopes are disjoint.

### L0 — Contracts & plans **[orchestrator]**
Write: `cloud/proto/cloud/v1/**`, `cloud/docs/**`, `cloud/README.md`, `buf.yaml`.
Accept: `buf lint` clean; `buf generate` produces Go + TS; plan reviewed.

### L1 — Control-plane foundation
Write: `cloud/internal/cloud/{config,secrets,db,auth,rbac,audit,httpapi}/**`,
`cloud/cmd/cloudcontrold/**`, `cloud/deploy/docker/Dockerfile.cloudcontrold`.
Depends: L0.
Accept:
- config loads from YAML + env, validates, reports missing secrets without crashing.
- Postgres connect + gormigrate migrations run idempotently.
- Clerk verifier: RS256, JWKS cache + refresh, checks `iss`, `exp`, `azp`, `sub`,
  extracts `org_id`/`org_role`; rejects alg=none and unknown kid; tests with a
  locally-generated RSA key + JWKS served from httptest.
- RBAC: Casbin model + policy; org scoping; fail-closed interceptor;
  `TestEveryProcedureIsMapped` coverage test.
- Audit: every mutating RPC writes actor/org/action/resource/result.
- Server: `/healthz`, `/readyz`, `/metrics`, Connect-RPC handlers registered.

### L2 — Node subsystem (control plane side)
Write: `cloud/internal/cloud/{node,agent,capacity}/**`,
`cloud/internal/cloud/httpapi/nodes*`, `cloud/internal/cloud/nodetype/**`.
Depends: L1 contracts (proto).
Accept:
- node type catalog: `nano|small|medium|large|xlarge` + custom, with vcpu/ram/disk/price,
  per-provider instance-type mapping.
- join tokens: HMAC-signed, org + expiry bound, single-use, revocable; tests.
- node registry: register/heartbeat/drain/delete; capacity accounting sums
  allocations and rejects over-commit; tests.
- agent protocol: server-stream + client command envelope; reconnect/backoff.

### L3 — Provisioner + Terraform
Write: `cloud/internal/cloud/{provider,provision}/**`,
`cloud/terraform/**`.
Depends: L0, L2 (node types).
Accept:
- `Provider` interface implemented for `hetzner`, `aws`, `gcp`, `digitalocean`,
  `proxmox`, `generic` (generic = any SSH-reachable host).
- provisioner: workspace per node, `init → plan → apply → outputs`, state backend
  configurable (`local` or `s3`), plan stored and diffable, apply gated on RBAC.
- terraform `fmt -check` + `validate` clean for all modules; `.tfvars.example` for
  each provider; cloud-init renders a node join bootstrap.

### L4 — Node agent + workload execution
Write: `cloud/cmd/cloudnoded/**`, `cloud/internal/cloud/nodeagent/**`.
Depends: L2.
Accept:
- join with token → receives node identity; connects with mTLS; heartbeat loop.
- receives workload ops (create/start/stop/delete/exec/logs) and applies them via
  the existing `internal/docker` client; reports status + metrics.
- proxy route registration via the existing route syncer.
- survives control-plane outage (buffered status, reconnect with backoff).

### L5 — Console
Write: `cloud/web/cloud-console/**`.
Depends: L1–L4 contracts (generated TS).
Accept:
- Clerk sign-in, org switcher, guarded routes.
- pages: Overview, Nodes (list + detail), Add BYO node (join command), Provision
  managed node (node type + region + provider wizard with plan review), Node types,
  Members & roles, API keys, Audit log.
- Carbon Gray 100 styling consistent with `web/carbon-panel`; sharp geometry.
- `bun run check`, `bun run build`, and a Playwright smoke test.

### L6 — Deploy, CI, observability
Write: `cloud/deploy/**`, `.github/workflows/cloud-ci.yml`, `cloud/Makefile` (or
targets in root Makefile), `cloud/internal/cloud/obs/**`.
Depends: L1–L5 artifacts.
Accept: Dockerfiles build; compose stack healthy; Helm renders + lints; CI runs
build/vet/test/check/buf/terraform; metrics + traces + structured logs.

### L7 — Hardening & docs **[orchestrator + oracle review]**
Write: `cloud/docs/runbooks/**`, tests, fixes.
Accept: definition-of-done §0 satisfied; independent review found no high-severity
open issue.

---

## 4. Integration protocol (cross-node subagents)

Subagents run on other cluster nodes with their **own** checkout at
`/home/prox/carbon-panel`. Work is integrated through Forgejo, never through a
shared filesystem.

- Base branch on Forgejo: `cloud/base` (pushed by the orchestrator).
- Every lane works on `cloud/<lane>` branched from `cloud/base`, in a git worktree
  (`--worktree`) or after `git checkout -B` in a clean tree.
- Every lane must: `git add -A && git commit -m "<lane>: <summary>" && git push localrepo cloud/<lane>`.
- The orchestrator fetches and merges `cloud/<lane>` into the integration branch,
  resolves conflicts (orchestrator owns conflict resolution), and re-runs gates.
- A lane that cannot push is a failed lane: report it, do not silently leave work
  only on the remote node.

Write-scope rule: lanes must not touch files outside their listed write scope. If a
lane needs a change outside its scope, it must **report** the need instead of
editing. This is what keeps parallel lanes mergeable.

---

## 5. Non-negotiables

1. **No secrets in the repo.** Ever. Keys arrive later via the secrets provider.
2. **Deny by default.** New RPC with no policy entry must fail closed.
3. **Tenant isolation is a test, not a promise.** Every resource query is
   org-scoped and cross-org access has a regression test.
4. **Terraform is the only path to infrastructure.** No provider SDK calls that
   create VMs behind Terraform's back.
5. **Cloud-provider agnostic core.** Provider-specific code lives behind the
   `Provider` interface and in `cloud/terraform/modules/node/<provider>`; nothing
   else may branch on provider name.
6. **The OSS product keeps working.** `make test` covers OSS + cloud.
7. **Everything mutating is audited.**
8. **Docs are executable.** If the README says `docker compose up`, it works.
