# `modules/node/generic`

Bootstraps any existing SSH-reachable host. **Needs no cloud
credentials at all.** Used by tests, on-prem, and unlisted clouds.

Extra variables beyond the contract: `host`, `ssh_user` (`root`),
`ssh_port` (`22`), `ssh_private_key` (empty uses the SSH agent).

Pass a **shellscript** bootstrap: render `modules/cloudinit` with
`format = "shellscript"`.

## Example

```hcl
module "node" {
  source            = "../../node/generic"
  org_id            = "org_123"
  node_id           = "node_abc"
  node_name         = "onprem-01"
  node_type         = "small"
  vcpu              = 2
  ram_mb            = 4096
  disk_gb           = 40
  region            = "onprem"
  instance_type     = "custom"
  ssh_public_key    = file("~/.ssh/id_ed25519.pub")
  cloud_init        = module.bootstrap.user_data
  control_plane_url = "https://cloud.example.com"
  join_token        = var.join_token
  host              = "192.168.1.50"
}
```

## Offline plan

`provisioner` blocks run only on apply, so `plan` makes no SSH or
cloud calls. The only network needed is the one-time `null` provider
download:

```sh
tofu -chdir=cloud/terraform/modules/node/generic/examples/offline init
tofu -chdir=cloud/terraform/modules/node/generic/examples/offline plan
```
