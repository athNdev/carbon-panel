# Provider-agnostic control plane: compute is BYO hosts via the generic
# node module, the database is an externally managed PostgreSQL passed
# in as variables, and object storage for state is configured via the
# backend example. Validates with no credentials.
locals {
  control_plane_bootstrap = join("\n", [
    "#!/bin/sh",
    "set -eu",
    "if command -v apt-get >/dev/null 2>&1; then apt-get update && apt-get install -y curl ca-certificates;",
    "elif command -v dnf >/dev/null 2>&1; then dnf install -y curl ca-certificates;",
    "elif command -v apk >/dev/null 2>&1; then apk add --no-cache curl ca-certificates;",
    "else echo 'no supported package manager found' >&2; exit 1; fi",
    "if ! command -v docker >/dev/null 2>&1; then curl -fsSL https://get.docker.com | sh; fi",
    "install -d -m 0755 /opt/carbon-cloud/bin",
    "TMP_BIN=$(mktemp)",
    "curl -fsSL -o \"$TMP_BIN\" \"${var.control_plane_install_url}\"",
    var.control_plane_sha256 != "" ? "echo \"${var.control_plane_sha256}  $TMP_BIN\" | sha256sum -c -" : "echo 'no control-plane checksum supplied, skipping verification'",
    "install -m 0755 \"$TMP_BIN\" /opt/carbon-cloud/bin/cloudcontrold",
    "rm -f \"$TMP_BIN\"",
    "cat >/etc/systemd/system/cloudcontrold.service <<'UNIT_EOF'",
    "[Unit]",
    "Description=Carbon Cloud control plane",
    "After=network-online.target docker.service",
    "Wants=network-online.target",
    "[Service]",
    "Environment=CARBONCLOUD_DATABASE_URL=${local.database_url}",
    "Environment=CARBONCLOUD_STATE_BUCKET=${var.state_bucket}",
    "ExecStart=/opt/carbon-cloud/bin/cloudcontrold",
    "Restart=on-failure",
    "RestartSec=5",
    "[Install]",
    "WantedBy=multi-user.target",
    "UNIT_EOF",
    "systemctl daemon-reload",
    "systemctl enable --now cloudcontrold.service",
  ])

  database_url = "postgres://${var.db_user}:${var.db_password}@${var.db_host}:${var.db_port}/${var.db_name}?sslmode=require"
}

module "control_plane_host" {
  source            = "../../modules/node/generic"
  for_each          = { for h in nonsensitive(var.control_plane_hosts) : h.host => h }
  org_id            = "control-plane"
  node_id           = "control-plane-${each.key}"
  node_name         = "control-plane-${each.key}"
  node_type         = "custom"
  vcpu              = 0
  ram_mb            = 0
  disk_gb           = 0
  region            = var.region
  instance_type     = "custom"
  ssh_public_key    = ""
  cloud_init        = local.control_plane_bootstrap
  control_plane_url = var.control_plane_url
  join_token        = "control-plane-hosts-do-not-join"
  tags              = merge(var.tags, { environment = var.environment })
  host              = each.value.host
  ssh_user          = each.value.ssh_user
  ssh_port          = each.value.ssh_port
  ssh_private_key   = each.value.ssh_private_key
}
