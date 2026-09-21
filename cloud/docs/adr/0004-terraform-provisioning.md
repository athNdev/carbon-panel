# ADR 0004 — Terraform is the provisioning engine, behind a provider abstraction

- Status: accepted
- Date: 2026-09-21

## Context

Managed nodes must be creatable on multiple clouds (and on plain SSH-reachable
hosts) without the application becoming a pile of provider SDK calls. The
requirement is explicit: "as cloud-provider agnostic as possible, terraform".

Two shapes were possible: (a) call each provider's API directly from Go and keep
Terraform only for the control plane's own infra; (b) make Terraform the single
provisioning engine for **nodes as well as** control-plane infra.

## Decision

(b) Terraform provisions everything, including tenant managed nodes.

- `cloud/internal/cloud/provider` defines the Go seam:
  `Provider{ ID, Regions, InstanceTypes(nodeType), RenderVars(spec), TerraformModuleDir }`.
  `generic` (any host reachable over SSH) is a first-class provider so on-prem and
  unlisted clouds still work.
- `cloud/internal/cloud/provision` runs a **workspace per node**:
  `init → plan → (approval) → apply → outputs`. Plan JSON is stored and shown to
  the tenant before apply, so a tenant approves a concrete diff, not a promise.
- State backend is pluggable (`local` for dev, `s3`-compatible for prod) and
  configured — never hardcoded. Per-node workspaces keep blast radius small.
- Module layout:
  `cloud/terraform/modules/node/{hetzner,aws,gcp,digitalocean,proxmox,generic}`,
  `cloud/terraform/modules/cloudinit` (renders node bootstrap), and
  `cloud/terraform/stacks/control-plane`.
- Provider selection is data, not code: adding a cloud = adding a module directory
  and an `InstanceTypes` mapping. No `switch provider` outside `internal/cloud/provider`.
- The provisioner runs Terraform as a subprocess with `-input=false
  -no-color -lock-timeout`, parses `plan -json` / `show -json`, and treats
  non-zero exit as a typed error. It refuses to run if a mutex/lease for that
  workspace is held by another apply.

## Consequences

- The application is provider-agnostic by construction; the only provider-shaped
  code is a mapping table and a module directory.
- Provisioning is auditable: the plan and apply output are stored per node.
- Cost of abstraction: a Terraform binary (or OpenTofu) must be present in the
  control-plane image and its version pinned in config. `terraform version` is
  checked at startup and reported in `/readyz`.
- Secrets for providers (cloud tokens) are read through the secrets provider and
  passed via `TF_VAR_*` env for the subprocess only, never written to disk or logs
  (redaction is tested).
