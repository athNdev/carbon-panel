package provision

import (
	"fmt"
	"strings"
)

// BootstrapRequest renders the node bootstrap (cloud-init user-data) with the
// join token, control-plane URL and node agent image/version. It mirrors
// cloud/terraform/modules/cloudinit inputs: control_plane_url, join_token,
// node_name, node_type, agent_version, agent_install_url, agent_sha256,
// extra_runcmd, format.
type BootstrapRequest struct {
	ControlPlaneURL string
	JoinToken       string
	NodeName        string
	NodeTypeID      string
	AgentVersion    string
	AgentInstallURL string
	AgentSHA256     string
	ExtraRuncmd     []string
	// Format is "cloud-config" (default) or "shellscript" (generic hosts).
	Format string
	// Provider selects the default format: generic -> shellscript.
	Provider string
}

// RenderUserData returns the bootstrap plus its MIME type
// (text/cloud-config or text/x-shellscript).
func RenderUserData(req BootstrapRequest) (userData, contentType string, err error) {
	format := req.Format
	if format == "" {
		if req.Provider == "generic" {
			format = "shellscript"
		} else {
			format = "cloud-config"
		}
	}
	if format != "cloud-config" && format != "shellscript" {
		return "", "", fmt.Errorf("provision: bad bootstrap format %q", format)
	}
	if strings.TrimSpace(req.ControlPlaneURL) == "" {
		return "", "", fmt.Errorf("provision: control-plane URL is required")
	}
	if strings.TrimSpace(req.JoinToken) == "" {
		return "", "", fmt.Errorf("provision: join token is required")
	}
	version := req.AgentVersion
	if version == "" {
		version = "latest"
	}
	install := renderInstallScript(req, version)
	if format == "shellscript" {
		return "#!/bin/sh\n" + install + "\n", "text/x-shellscript", nil
	}
	var b strings.Builder
	b.WriteString("#cloud-config\n")
	b.WriteString("package_update: true\n")
	b.WriteString("packages: [curl, ca-certificates]\n")
	b.WriteString("write_files:\n")
	b.WriteString("  - path: /opt/carbon-cloud/install-agent.sh\n")
	b.WriteString("    permissions: '0700'\n")
	b.WriteString("    content: |\n")
	for _, line := range strings.Split(install, "\n") {
		b.WriteString("      " + line + "\n")
	}
	b.WriteString("runcmd:\n")
	b.WriteString("  - /opt/carbon-cloud/install-agent.sh\n")
	for _, c := range req.ExtraRuncmd {
		b.WriteString("  - " + c + "\n")
	}
	return b.String(), "text/cloud-config", nil
}

func renderInstallScript(req BootstrapRequest, version string) string {
	lines := []string{
		"set -eu",
		"if command -v apt-get >/dev/null 2>&1; then apt-get update && apt-get install -y curl ca-certificates;",
		"elif command -v apk >/dev/null 2>&1; then apk add --no-cache curl ca-certificates;",
		"else echo 'no supported package manager found' >&2; exit 1; fi",
		"if ! command -v docker >/dev/null 2>&1; then curl -fsSL https://get.docker.com | sh; fi",
		"install -d -m 0700 /etc/carbon-cloud",
		"install -d -m 0755 /opt/carbon-cloud/bin",
		`TMP_BIN=$(mktemp)`,
		fmt.Sprintf("curl -fsSL -o \"$TMP_BIN\" %q", req.AgentInstallURL),
	}
	if req.AgentSHA256 != "" {
		lines = append(lines, fmt.Sprintf("echo %q | sha256sum -c -", req.AgentSHA256+"  $TMP_BIN"))
	} else {
		lines = append(lines, "echo 'no agent checksum supplied, skipping verification'")
	}
	lines = append(lines,
		"install -m 0755 \"$TMP_BIN\" /opt/carbon-cloud/bin/cloudnoded",
		"rm -f \"$TMP_BIN\"",
		"cat >/etc/carbon-cloud/agent.env <<'AGENT_ENV_EOF'",
		"CARBONCLOUD_CONTROL_PLANE_URL="+req.ControlPlaneURL,
		"CARBONCLOUD_JOIN_TOKEN="+req.JoinToken,
		"CARBONCLOUD_NODE_NAME="+req.NodeName,
		"CARBONCLOUD_NODE_TYPE="+req.NodeTypeID,
		"CARBONCLOUD_AGENT_VERSION="+version,
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
	)
	lines = append(lines, req.ExtraRuncmd...)
	return strings.Join(lines, "\n")
}
