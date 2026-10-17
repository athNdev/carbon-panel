# `modules/cloudinit`

Renders the `cloudnoded` node bootstrap. No provider plugins: pure
`locals`, so it validates and plans fully offline.

## Variables

| variable | type | notes |
|---|---|---|
| `control_plane_url` | string | endpoint the agent must dial |
| `join_token` | string (sensitive) | single-use join token |
| `node_name` | string | DNS-safe name |
| `node_type` | string | nano/small/medium/large/xlarge/custom |
| `agent_version` | string (`latest`) | agent version to install |
| `agent_install_url` | string | binary download URL |
| `agent_sha256` | string (`""`) | SHA-256 to verify; empty skips |
| `extra_runcmd` | list(string) | extra root commands, run last |
| `format` | string (`cloud-config`) | `cloud-config` or `shellscript` |

## Outputs

| output | notes |
|---|---|
| `user_data` (sensitive) | rendered bootstrap |
| `content_type` | `text/cloud-config` or `text/x-shellscript` |

## Behaviour

- Installs Docker Engine (detects apt/dnf/zypper/apk, else Docker's
  convenience script as fallback).
- Writes `/etc/carbon-cloud/agent.env` (`CARBONCLOUD_CONTROL_PLANE_URL`,
  `CARBONCLOUD_JOIN_TOKEN`), mode 0600.
- Installs `cloudnoded`, enables `cloudnoded.service` (restart on
  failure).
- Idempotent: re-running reinstalls the same binary and restarts the
  unit; it never unjoins a node.
- The join token never appears in a world-readable path or a log:
  `user_data` is `sensitive`, the env file is 0600, and the installer
  script passes no secrets on a command line.

## Example

```hcl
module "bootstrap" {
  source            = "../../cloudinit"
  control_plane_url = "https://cloud.example.com"
  join_token        = var.join_token
  node_name         = "node-01"
  node_type         = "small"
  agent_install_url = "https://releases.example.com/cloudnoded/v1.2.3/cloudnoded-linux-amd64"
  agent_sha256      = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
}
```
