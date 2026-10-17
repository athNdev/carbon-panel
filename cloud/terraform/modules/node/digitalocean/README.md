# `modules/node/digitalocean`

Provisions a managed node on DigitalOcean (`digitalocean_droplet`)
with the bootstrap as `user_data`. Credentials via
`DIGITALOCEAN_TOKEN` env var only.

## Contract

Variables: `org_id`, `node_id`, `node_name`, `node_type`
(nano/small/medium/large/xlarge/custom), `vcpu`, `ram_mb`, `disk_gb`,
`region` (slug, e.g. `fra1`), `instance_type` (slug, e.g.
`s-2vcpu-4gb`), `ssh_public_key`, `cloud_init`,
`control_plane_url`, `join_token` (sensitive), `tags` (map, rendered
as `key:value` droplet tags), `image` (default `ubuntu-24-04-x64`),
`provider_extra` (reserved).

Outputs: `node_id`, `public_ip`, `private_ip`, `hostname`,
`instance_id`, `provider` (`digitalocean`).

## Example

```hcl
module "node" {
  source            = "../../node/digitalocean"
  org_id            = "org_123"
  node_id           = "node_abc"
  node_name         = "mc-01"
  node_type         = "small"
  vcpu              = 2
  ram_mb            = 4096
  disk_gb           = 40
  region            = "fra1"
  instance_type     = "s-2vcpu-4gb"
  ssh_public_key    = file("~/.ssh/id_ed25519.pub")
  cloud_init        = module.bootstrap.user_data
  control_plane_url = "https://cloud.example.com"
  join_token        = var.join_token
}
```
