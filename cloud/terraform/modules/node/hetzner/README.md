# `modules/node/hetzner`

Provisions a managed node on Hetzner Cloud (`hcloud_server`) with the
bootstrap as `user_data`. Credentials via `HCLOUD_TOKEN` env var only.

## Contract

Variables: `org_id`, `node_id`, `node_name`, `node_type`
(nano/small/medium/large/xlarge/custom), `vcpu`, `ram_mb`, `disk_gb`,
`region` (Hetzner location, e.g. `fsn1`), `instance_type` (server type,
e.g. `cx22`; may be `custom` when sized elsewhere), `ssh_public_key`,
`cloud_init`, `control_plane_url`, `join_token` (sensitive),
`tags`, `image` (default `ubuntu-24.04`), `provider_extra`
(`ipv6_enabled = "true"/"false"`).

Outputs: `node_id`, `public_ip`, `private_ip`, `hostname`,
`instance_id`, `provider` (`hetzner`).

## Example

```hcl
module "node" {
  source            = "../../node/hetzner"
  org_id            = "org_123"
  node_id           = "node_abc"
  node_name         = "mc-01"
  node_type         = "small"
  vcpu              = 2
  ram_mb            = 4096
  disk_gb           = 40
  region            = "fsn1"
  instance_type     = "cx22"
  ssh_public_key    = file("~/.ssh/id_ed25519.pub")
  cloud_init        = module.bootstrap.user_data
  control_plane_url = "https://cloud.example.com"
  join_token        = var.join_token
}
```
