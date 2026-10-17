# `modules/node/aws`

Provisions a managed node on AWS (`aws_instance`) with the bootstrap
as `user_data`. Credentials via standard `AWS_*` env vars only.
`var.image` must be an AMI id for `var.region` (no default: AMIs are
region-specific). Default VPC/subnet is used unless
`provider_extra["subnet_id"]` is set.

## Contract

Variables: `org_id`, `node_id`, `node_name`, `node_type`
(nano/small/medium/large/xlarge/custom), `vcpu`, `ram_mb`, `disk_gb`
(root volume size), `region`, `instance_type` (e.g. `t3.small`),
`ssh_public_key`, `cloud_init`, `control_plane_url`,
`join_token` (sensitive), `tags`, `image` (AMI id, required),
`provider_extra` (`availability_zone`, `subnet_id`, `volume_type`).

Outputs: `node_id`, `public_ip`, `private_ip`, `hostname`,
`instance_id`, `provider` (`aws`).

## Example

```hcl
module "node" {
  source            = "../../node/aws"
  org_id            = "org_123"
  node_id           = "node_abc"
  node_name         = "mc-01"
  node_type         = "small"
  vcpu              = 2
  ram_mb            = 4096
  disk_gb           = 40
  region            = "eu-central-1"
  instance_type     = "t3.small"
  ssh_public_key    = file("~/.ssh/id_ed25519.pub")
  cloud_init        = module.bootstrap.user_data
  control_plane_url = "https://cloud.example.com"
  join_token        = var.join_token
  image             = "ami-0faab6bdbac9486fb"
}
```
