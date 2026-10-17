locals {
  agent_env_content = join("\n", [
    "CARBONCLOUD_CONTROL_PLANE_URL=${var.control_plane_url}",
    "CARBONCLOUD_JOIN_TOKEN=${var.join_token}",
    "CARBONCLOUD_NODE_NAME=${var.node_name}",
    "CARBONCLOUD_NODE_TYPE=${var.node_type}",
  ])

  agent_install_sh = join("\n", concat(
    [
      "set -eu",
      "if command -v apt-get >/dev/null 2>&1; then apt-get update && apt-get install -y curl ca-certificates; PM=apt;",
      "elif command -v dnf >/dev/null 2>&1; then dnf install -y curl ca-certificates; PM=dnf;",
      "elif command -v zypper >/dev/null 2>&1; then zypper --non-interactive install curl ca-certificates; PM=zypper;",
      "elif command -v apk >/dev/null 2>&1; then apk add --no-cache curl ca-certificates; PM=apk;",
      "else echo 'no supported package manager found' >&2; exit 1; fi",
      "if ! command -v docker >/dev/null 2>&1; then curl -fsSL https://get.docker.com | sh; fi",
      "install -d -m 0700 /etc/carbon-cloud",
      "install -d -m 0755 /opt/carbon-cloud/bin",
      "TMP_BIN=$(mktemp)",
      "curl -fsSL -o \"$TMP_BIN\" \"${var.agent_install_url}\"",
      var.agent_sha256 != "" ? "echo \"${var.agent_sha256}  $TMP_BIN\" | sha256sum -c -" : "echo 'no agent checksum supplied, skipping verification'",
      "install -m 0755 \"$TMP_BIN\" /opt/carbon-cloud/bin/cloudnoded",
      "rm -f \"$TMP_BIN\"",
      "cat >/etc/carbon-cloud/agent.env <<'AGENT_ENV_EOF'",
      local.agent_env_content,
      "AGENT_ENV_EOF",
      "chmod 0600 /etc/carbon-cloud/agent.env",
      "cat >/etc/systemd/system/cloudnoded.service <<'UNIT_EOF'",
      "[Unit]",
      "Description=Carbon Cloud node agent",
      "After=network-online.target docker.service",
      "Wants=network-online.target",
      "[Service]",
      "EnvironmentFile=/etc/carbon-cloud/agent.env",
      "ExecStart=/opt/carbon-cloud/bin/cloudnoded",
      "Restart=on-failure",
      "RestartSec=5",
      "[Install]",
      "WantedBy=multi-user.target",
      "UNIT_EOF",
      "systemctl daemon-reload",
      "systemctl enable --now cloudnoded.service",
    ],
    var.extra_runcmd,
  ))

  cloud_config = {
    package_update = true
    packages       = ["curl", "ca-certificates"]
    write_files = [
      {
        path        = "/opt/carbon-cloud/install-agent.sh"
        permissions = "0700"
        content     = local.agent_install_sh
      },
    ]
    runcmd = concat(
      ["/opt/carbon-cloud/install-agent.sh"],
      var.extra_runcmd,
    )
  }

  cloud_config_user_data = "#cloud-config\n${yamlencode(local.cloud_config)}"
  shell_user_data        = "#!/bin/sh\n${local.agent_install_sh}\n"
}
