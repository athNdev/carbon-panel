# `modules/node/gcp`

Provisions a managed node on Google Cloud (`google_compute_instance`)
with the bootstrap as `user-data` metadata. Credentials via
`GOOGLE_CREDENTIALS` / application-default credentials only.
`var.image` must be a full image reference (e.g.
`ubuntu-os-cloud/ubuntu-2204-lts`).

## Contract

Variables: `org_id`, `node_id`, `node_name`, `node_type`
(nano/small/medium/large/xlarge/custom), `vcpu`, `ram_mb`, `disk_gb`
(boot disk size), `region` (zone defaults to `<region>-a`),
`instance_type` (e.g. `e2-small`), `ssh_public_key`, `cloud_init`,
`control_plane_url`, `join_token` (sensitive), `tags`
(lowercased to satisfy GCP label rules), `image` (required),
`provider_extra` (`zone`, `network`, `subnetwork`, `disk_type`,
`ssh_user` default `carbon`).

Outputs: `node_id`, `public_ip`, `private_ip`, `hostname`,
`instance_id`, `provider` (`gcp`).

## Example

```hcl
module "node" {
  source            = "../../node/gcp"
  org_id            = "org_123"
  node_id           = "node_abc"
  node_name         = "mc-01"
  node_type         = "small"
  vcpu              = 2
  ram_mb            = 4096
  disk_gb           = 40
  region            = "europe-west3"
  instance_type     = "e2-small"
  ssh_public_key    = file("~/.ssh/id_ed25519.pub")
  cloud_init        = module.bootstrap.user_data
  control_plane_url = "https://cloud.example.com"
  join_token        = var.join_token
  image             = "ubuntu-os-cloud/ubuntu-2204-lts"
}
```
