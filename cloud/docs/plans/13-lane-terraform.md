# Lane `terraform` — provider-agnostic infrastructure modules

Branch: `cloud/terraform`
Write scope: `cloud/terraform/**`, `cloud/docs/runbooks/provisioning.md`

Depends on: nothing. Read ADR 0003 and ADR 0004 first.

Deliverables are Terraform/OpenTofu modules and a small amount of documentation.
No Go code. `tofu` is installed at `~/.local/bin/tofu` on the orchestrator but may
not exist on your node — if it is missing, do not install anything: write the
modules carefully and say in your report that `fmt`/`validate` could not be run.

## Layout

```
cloud/terraform/
  modules/
    node/
      generic/        # any SSH-reachable host (null_resource + remote-exec fallback)
      hetzner/
      aws/
      gcp/
      digitalocean/
      proxmox/
    cloudinit/        # renders the node bootstrap (user-data / cloud-init)
  stacks/
    control-plane/    # provider-agnostic control plane infra
  README.md
```

## The universal node module contract

Every `modules/node/<provider>` must expose this **exact** variable/output
interface so the Go provisioner can drive all of them identically and so adding a
provider never touches Go code:

Variables (names are the contract):

| variable | type | notes |
|---|---|---|
| `org_id` | string | tenant id, used for naming and tagging |
| `node_id` | string | control-plane node id, used for naming and tagging |
| `node_name` | string | DNS-safe name, must be unique per org |
| `node_type` | string | one of `nano/small/medium/large/xlarge/custom` |
| `vcpu` | number | effective requested vcpu |
| `ram_mb` | number | effective requested RAM in MiB |
| `disk_gb` | number | effective requested disk in GiB |
| `region` | string | provider region |
| `instance_type` | string | provider machine type; may be `custom` |
| `ssh_public_key` | string | key injected for the node agent's bootstrap |
| `cloud_init` | string | rendered user-data from `modules/cloudinit` |
| `control_plane_url` | string | endpoint the agent must dial |
| `join_token` | string | single-use token (sensitive) |
| `tags` | map(string) | extra tags/labels |
| `image` | string | optional OS image override |
| `provider_extra` | map(string) | provider-specific escape hatch |

Outputs (names are the contract):

| output | notes |
|---|---|
| `node_id` | echo of the input, for correlation |
| `public_ip` | empty when not applicable |
| `private_ip` | empty when not applicable |
| `hostname` | FQDN or provider hostname |
| `instance_id` | provider instance id |
| `provider` | provider id string |

Rules:
- **`generic` must work with no cloud credentials at all.** It takes an existing
  host (IP + SSH key) and only runs the bootstrap. This is the provider used by
  tests and by on-prem. It must be able to do a `plan` fully offline.
- Every provider module must declare a `required_providers` block pinned to a
  version range, and must not use a provider that is not in the official registry.
- Never hardcode a credential. Tokens arrive as variables (`sensitive = true`) and
  are expected to be fed via `TF_VAR_*` by the Go provisioner.
- All resources carry `tags`/`labels`: `carbon-cloud`, `org_id`, `node_id`,
  `node_type`. This is how cost attribution works.
- Instances must boot with the bootstrap applied (user-data / cloud-init), so the
  node appears in the registry without further operator action.
- Every module gets a `README.md` with the variable/output tables and one usage
  example, plus `versions.tf`, `variables.tf`, `main.tf`, `outputs.tf`.

## `modules/cloudinit`

Renders the node bootstrap. Inputs: `control_plane_url`, `join_token`, `node_name`,
`node_type`, `agent_version`, `agent_install_url`, `extra_runcmd`.
Output: `user_data` (string), `content_type` (`text/cloud-config` or
`text/x-shellscript`).

The bootstrap must:
- install Docker Engine from the distro packages or Docker's convenience script,
  detecting the package manager (apt/dnf/zypper/apk);
- create `/etc/carbon-cloud/agent.env` with `CARBONCLOUD_CONTROL_PLANE_URL` and
  `CARBONCLOUD_JOIN_TOKEN`, mode 0600;
- install the `cloudnoded` binary (download from `agent_install_url`, verify a
  SHA-256 when `agent_sha256` is supplied) and enable a systemd unit
  `cloudnoded.service` that restarts on failure;
- be idempotent (re-running the bootstrap must not break a joined node);
- never write the join token to a world-readable path or to a log.

## `stacks/control-plane`

Provider-agnostic stack for the control plane itself: network, compute, a managed
PostgreSQL where the provider offers one (else document an external database) and
object storage for Terraform state. Use variables for everything, no defaults that
bake in a vendor. Provide a `terraform.tfvars.example`. This stack is expected to
be **planned by a human**, not driven by the Go provisioner, so it may be more
opinionated — but it must still `validate` with no credentials.

## Acceptance

```sh
tofu fmt -check -recursive cloud/terraform
tofu -chdir=cloud/terraform/modules/node/generic validate
tofu -chdir=cloud/terraform/modules/cloudinit validate
# init pulls providers; needs network. If it cannot run on your node, say so.
```

- `modules/node/generic` must additionally support a real offline plan: provide
  `cloud/terraform/modules/node/generic/examples/offline/` with a
  `terraform.tfvars` and a README command that produces a plan with no network
  calls beyond provider download.
- Document in `cloud/docs/runbooks/provisioning.md` how an operator adds a brand
  new provider in one PR: add `modules/node/<name>`, add its instance-type mapping
  to the node type catalog, and nothing else.

Report per conventions §6.
