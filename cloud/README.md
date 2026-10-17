# Carbon Cloud

Proprietary managed-cloud downstream of Carbon Panel.

Carbon Panel (OSS) is a single-tenant, self-hosted controller: one Go daemon, one
SQLite file, one Docker socket, optionally dialing other Docker daemons.

**Carbon Cloud** turns that into a multi-tenant managed service:

| Concern | OSS Carbon Panel | Carbon Cloud |
|---|---|---|
| Tenancy | single operator | orgs (tenants), many per deployment |
| Identity | local password + optional OIDC | **Clerk** (orgs, memberships, SSO, MFA) |
| Authorization | Casbin, global roles | **org-scoped RBAC**, fail-closed, audit-logged |
| Storage | SQLite (single writer) | **PostgreSQL** (multi-writer, HA-capable) |
| Nodes | operator-registered Docker endpoints | **BYO nodes** (agent join) + **managed nodes** (Terraform-provisioned) |
| Node sizing | free-form host/memory | **node type catalog** (cpu/ram/disk/price) |
| Provisioning | manual | **cloud-agnostic Terraform** modules per provider |
| Workloads | Minecraft servers on the local Docker socket | Minecraft servers scheduled onto tenant nodes |

## Layout

```
cloud/
  cmd/
    cloudcontrold/    # control-plane daemon (HTTP + Connect-RPC)
    cloudnoded/       # node agent (joins cluster, runs workloads)
    cloudctl/         # operator/tenant CLI (bootstrap, join, provision)
  internal/cloud/     # control-plane packages
  proto/cloud/v1/     # control-plane API contracts
  terraform/          # provider-agnostic infra + per-provider node modules
  deploy/             # Dockerfiles, compose, Helm chart
  web/cloud-console/  # SvelteKit + Carbon console (Clerk sign-in)
  docs/
    plans/            # development plans (source of truth for lane work)
    adr/              # architecture decision records
    runbooks/         # operational runbooks
```

The OSS product keeps building and shipping unchanged: `cloud/` is additive and
`make test` / `go test ./...` still cover the whole module, including `cloud/`.

## Quick start (local dev)

```sh
# 1. Postgres
docker compose -f cloud/deploy/compose/docker-compose.yml up -d postgres

# 2. Control plane (needs configuration or env vars — see cloud/deploy/compose/.env.example)
go run ./cloud/cmd/cloudcontrold --config cloud/deploy/docker/controld.dev.yaml

# 3. Node agent against a local Docker socket
go run ./cloud/cmd/cloudnoded --config cloud/deploy/docker/noded.dev.yaml

# 4. Console
cd cloud/web/cloud-console && bun install && bun run dev
```

No external API key is required to **build**; the control plane starts in a
degraded-but-honest mode and reports exactly which secret is missing. See
`cloud/docs/adr/0006-secrets-and-api-keys.md`.

## Documentation

- Development plan (phases, lanes, acceptance): `cloud/docs/plans/00-master-plan.md`
- Decisions: `cloud/docs/adr/`
- Runbooks: `cloud/docs/runbooks/`
