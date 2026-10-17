# `modules/node/proxmox`

Provisions a managed node on Proxmox VE (`proxmox_virtual_environment_vm`,
bpg/proxmox provider). Credentials via `PROXMOX_VE_*` env vars only.

Cloud-init arrives as a **snippet file**: the Go provisioner uploads
`var.cloud_init` to a snippet datastore and passes the file id via
`provider_extra["user_data_file_id"]`.

## Contract

Variables: `org_id`, `node_id`, `node_name`, `node_type`
(nano/small/medium/large/xlarge/custom), `vcpu` (cores), `ram_mb`,
`disk_gb`, `region` (informational, e.g. cluster name),
`instance_type` (informational), `ssh_public_key` (recorded for
audit; key injection happens via the snippet), `cloud_init`
(sensitive), `control_plane_url`, `join_token` (sensitive), `tags`,
`image` (informational), `provider_extra`: `target_node` (default
`pve`), `vm_id` (default auto), `datastore_id` (default `local-lvm`),
`cloudinit_datastore_id` (default `local`), `user_data_file_id`,
`bridge` (default `vmbr0`), `ipv4_address` (default `dhcp`),
`ipv4_gateway`.

Outputs: `node_id`, `public_ip` (empty; nodes dial out), `private_ip`
(first guest-agent IPv4), `hostname`, `instance_id`, `provider`
(`proxmox`).

## Example

```hcl
module "node" {
  source            = "../../node/proxmox"
  org_id            = "org_123"
  node_id           = "node_abc"
  node_name         = "mc-01"
  node_type         = "small"
  vcpu              = 2
  ram_mb            = 4096
  disk_gb           = 40
  region            = "dc1"
  instance_type     = "custom"
  ssh_public_key    = file("~/.ssh/id_ed25519.pub")
  cloud_init        = module.bootstrap.user_data
  control_plane_url = "https://cloud.example.com"
  join_token        = var.join_token
  provider_extra = {
    target_node       = "pve1"
    user_data_file_id = "local:snippets/node-abc-user-data.yaml"
  }
}
```
