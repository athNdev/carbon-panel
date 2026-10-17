# Carbon Cloud Terraform

Provider-agnostic infrastructure for Carbon Cloud managed nodes,
plus a provider-agnostic control-plane stack.

Layout:

- `modules/cloudinit/` — renders the `cloudnoded` node bootstrap
  (cloud-config or shell script).
- `modules/node/<provider>/` — one module per provider (`generic`,
  `hetzner`, `aws`, `gcp`, `digitalocean`, `proxmox`). Every module
  exposes the **same variable/output contract** so the Go provisioner
  (`cloud/internal/cloud/provision`, owned by another lane) drives all
  of them identically.
- `stacks/control-plane/` — control-plane infra (BYO hosts, external
  managed database, S3-compatible state). Planned by a human.

## Universal node module contract

Inputs (exact names): `org_id`, `node_id`, `node_name`, `node_type`,
`vcpu`, `ram_mb`, `disk_gb`, `region`, `instance_type`,
`ssh_public_key`, `cloud_init`, `control_plane_url`, `join_token`
(sensitive), `tags`, `image`, `provider_extra`.

Outputs (exact names): `node_id`, `public_ip`, `private_ip`,
`hostname`, `instance_id`, `provider`.

Rules every module follows:

- No hardcoded credentials. Tokens arrive as `sensitive = true`
  variables fed via `TF_VAR_*` by the provisioner.
- All resources carry `carbon-cloud`, `org_id`, `node_id`, `node_type`
  for cost attribution.
- Instances boot with `cloud_init` applied, so the node joins the
  registry with no further operator action.

## Credentials

Provider credentials are **never** Terraform variables here. Each
module README names the environment variable its provider reads
(`HCLOUD_TOKEN`, `AWS_*`, `GOOGLE_CREDENTIALS`, `DIGITALOCEAN_TOKEN`,
`PROXMOX_VE_*`). `generic` needs no cloud credentials at all.

## Adding a provider

See `cloud/docs/runbooks/provisioning.md`.
