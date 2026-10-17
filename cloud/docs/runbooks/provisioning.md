# Provisioning runbook

How managed nodes are provisioned, and how to add a provider.

## Model

One node abstraction, two onboarding paths (ADR 0003). Managed nodes
are Terraform workspaces driven by the Go provisioner
(`cloud/internal/cloud/provision`, owned by another lane) as
`init → plan → (tenant approval) → apply → outputs` (ADR 0004).

Every `cloud/terraform/modules/node/<provider>` exposes the same
variable/output contract (see `cloud/terraform/README.md`), so adding
a provider never touches Go code: provider selection is data, not code.

## Credentials

Never hardcoded. Join tokens arrive as `sensitive = true` variables
fed via `TF_VAR_*` for the subprocess only. Provider credentials are
environment variables read by the provider plugin:

| module | env |
|---|---|
| `generic` | none (SSH only) |
| `hetzner` | `HCLOUD_TOKEN` |
| `aws` | `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` |
| `gcp` | `GOOGLE_CREDENTIALS` (or ADC) |
| `digitalocean` | `DIGITALOCEAN_TOKEN` |
| `proxmox` | `PROXMOX_VE_ENDPOINT`, `PROXMOX_VE_API_TOKEN` |

## Offline plan (generic)

```sh
tofu -chdir=cloud/terraform/modules/node/generic/examples/offline init
tofu -chdir=cloud/terraform/modules/node/generic/examples/offline plan
```

No SSH or cloud calls; only the one-time `null` provider download
needs network.

## Adding a brand new provider in one PR

1. Copy `modules/node/hetzner/` to `modules/node/<name>/` and rewrite
   `main.tf` for the new provider. Keep the **exact** contract
   variable names (`org_id`, `node_id`, `node_name`, `node_type`,
   `vcpu`, `ram_mb`, `disk_gb`, `region`, `instance_type`,
   `ssh_public_key`, `cloud_init`, `control_plane_url`, `join_token`
   (sensitive), `tags`, `image`, `provider_extra`) and output names
   (`node_id`, `public_ip`, `private_ip`, `hostname`, `instance_id`,
   `provider`).
2. Pin `required_providers` to a version range from the official
   registry. Never hardcode a credential; feed secrets via `TF_VAR_*`.
3. Tag every resource with `carbon-cloud`, `org_id`, `node_id`,
   `node_type` for cost attribution.
4. Boot with `cloud_init` applied so the node joins with no further
   operator action.
5. Write `README.md` with the variable/output tables and one usage
   example.
6. Add the instance-type mapping for the new provider to the node type
   catalog (`cloud/internal/cloud/nodetype`, owned by another lane) —
   and nothing else. No Go provisioning code changes.
7. Run `tofu fmt -check -recursive cloud/terraform` and
   `tofu -chdir=cloud/terraform/modules/node/<name> validate`.
