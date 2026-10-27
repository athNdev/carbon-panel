<!--
SPDX-License-Identifier: MIT AND MPL-2.0
This file includes DiscoPanel material under the MIT License,
Copyright (c) 2025 Nicholas W. Heyer. The MIT license text is in LICENSE
and still applies to that inherited material.
Maintainer changes are licensed under the Mozilla Public License, v. 2.0.
If a copy of the MPL was not distributed with this file, You can obtain
one at https://mozilla.org/MPL/2.0/.
-->

<div align="center">
  <img src="assets/banner.svg" alt="Carbon Panel — Minecraft servers, run like production." width="100%" />

  <p>
    <strong>A self-hosted, Docker-native control plane for Minecraft servers.</strong><br />
    Every server in its own container &mdash; players routed by hostname, modpacks shipped
    straight from CurseForge and Modrinth.
  </p>

  <p>
    <a href="https://github.com/athNdev/carbon-panel/actions/workflows/ci.yml"><img src="https://github.com/athNdev/carbon-panel/actions/workflows/ci.yml/badge.svg" alt="Continuous Integration" /></a>
    <a href="https://go.dev/"><img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26" /></a>
    <a href="LICENSE.MPL-2.0"><img src="https://img.shields.io/badge/license-MPL--2.0-0f62fe" alt="License: MPL-2.0" /></a>
    <a href="https://hub.docker.com/"><img src="https://img.shields.io/badge/deploy-Docker-2496ED?logo=docker&logoColor=white" alt="Docker" /></a>
  </p>

  <p>
    <a href="https://athNdev.github.io/carbon-panel/">Website</a> &bull;
    <a href="#quick-start">Quick start</a> &bull;
    <a href="#features">Features</a> &bull;
    <a href="#one-panel-every-machine">Fleet</a> &bull;
    <a href="https://github.com/athNdev/carbon-panel/issues">Issues</a>
  </p>
</div>

<table>
  <tr>
    <td align="center" width="25%"><strong>11+</strong><br /><sub>server types</sub></td>
    <td align="center" width="25%"><strong>0 B</strong><br /><sub>idle RAM with deep-sleep</sub></td>
    <td align="center" width="25%"><strong>&#8734;</strong><br /><sub>machines, one panel</sub></td>
    <td align="center" width="25%"><strong>MPL-2.0</strong><br /><sub>permanently open</sub></td>
  </tr>
</table>

---

## Why Carbon Panel?

Hosting servers shouldn't feel like babysitting.

One flaky plugin, one forgotten backup, one accidental `rm -rf` and the whole night is gone.
Carbon Panel treats your worlds the way production teams treat state: isolated, observable,
and recoverable.

<table>
  <tr>
    <td width="50%" valign="top">
      <strong>The usual way</strong><br /><br />
      Hand-rolled scripts and bare JVM processes on a shared host.
      <ul>
        <li>One bad mod corrupts every world sharing the host</li>
        <li>Port-forwarding a new server means editing firewall rules</li>
        <li>Backups are a cron job nobody has ever restored from</li>
        <li>Idle servers burn RAM all night, every night</li>
      </ul>
    </td>
    <td width="50%" valign="top">
      <strong>The Carbon way</strong><br /><br />
      Every server is a Docker container with real limits, routes and history.
      <ul>
        <li>Blast radius is one container, never the host</li>
        <li>New server = new hostname, routed automatically</li>
        <li>World-aware backups with retention and offsite copies</li>
        <li>Deep-sleep to zero RAM, wakes when a player connects</li>
      </ul>
    </td>
  </tr>
</table>

---

## Features

<table>
  <tr>
    <td width="50%" valign="top">
      <b>Docker-native server management</b><br /><br />
      Deploy vanilla, Paper, Purpur, Fabric, Quilt, Forge, NeoForge and Bedrock in seconds &mdash;
      each in its own container with CPU and RAM limits, automatic Java selection, and
      Aikar-tuned JVM presets.<br /><br />
      <code>vanilla</code> <code>paper</code> <code>fabric</code> <code>forge</code> <code>bedrock</code>
    </td>
    <td width="50%" valign="top">
      <b>One port, every server</b><br /><br />
      A handshake-aware reverse proxy routes players by hostname through a single ingress &mdash;
      with Velocity forwarding, PROXY protocol, and Bedrock UDP built in.<br /><br />
      <code>:25565</code> <code>SRV</code> <code>velocity</code> <code>bedrock udp</code>
    </td>
  </tr>
  <tr>
    <td width="50%" valign="top">
      <b>Modpack Studio</b><br /><br />
      Search CurseForge and Modrinth without an API key, resolve dependencies and loader versions
      automatically, and export to <code>.mrpack</code> or packwiz.<br /><br />
      <code>curseforge</code> <code>modrinth</code> <code>packwiz</code>
    </td>
    <td width="50%" valign="top">
      <b>Live console &amp; RCON</b><br /><br />
      Stream ANSI-rendered logs over WebSocket with command history and an interactive RCON
      shell.<br /><br />
      <code>websocket</code> <code>ansi</code> <code>mclo.gs</code>
    </td>
  </tr>
  <tr>
    <td width="50%" valign="top">
      <b>Full file manager</b><br /><br />
      Browse, edit, zip and bulk-download files with resumable uploads and SHA-256 verified
      remote pulls.<br /><br />
      <code>resumable</code> <code>zip / unzip</code>
    </td>
    <td width="50%" valign="top">
      <b>Backups that restore</b><br /><br />
      World-aware snapshots pause saves through RCON, respect retention rules, and push offsite
      to S3-compatible storage.<br /><br />
      <code>S3 / MinIO</code> <code>retention</code>
    </td>
  </tr>
  <tr>
    <td width="50%" valign="top">
      <b>Your machines, one panel</b><br /><br />
      Add your home machine, a homelab box, or a cloud VPS as a node &mdash; then place each
      server wherever it makes sense. Routes and state stay in sync.<br /><br />
      <code>home lab</code> <code>cloud VPS</code> <code>placement</code>
    </td>
    <td width="50%" valign="top">
      <b>Automation &amp; scheduling</b><br /><br />
      Cron, interval and event-driven task chains &mdash; commands, backups, webhooks, even
      Chunky pre-generation and Git modpack updates.<br /><br />
      <code>cron</code> <code>chains</code> <code>webhooks</code>
    </td>
  </tr>
  <tr>
    <td width="50%" valign="top">
      <b>Security &amp; access</b><br /><br />
      Role-based access control, OIDC single sign-on, scoped API tokens, invite links, and an
      audit log of everything.<br /><br />
      <code>RBAC</code> <code>OIDC</code> <code>audit</code>
    </td>
    <td width="50%" valign="top">
      <b>Typed, observable API</b><br /><br />
      Protocol Buffers over Connect RPC &amp; gRPC, with a live OpenAPI viewer, reflection, and
      Prometheus metrics endpoints.<br /><br />
      <code>Connect RPC</code> <code>OpenAPI</code> <code>Prometheus</code>
    </td>
  </tr>
  <tr>
    <td colspan="2" valign="top">
      <b>Sleep, freeze, wake on join</b> &mdash; Empty servers can freeze in place at ~0% CPU,
      or stop entirely at zero RAM while the proxy keeps answering pings. Either way, a player
      joining brings them back &mdash; nothing to start by hand, no admin in the loop.<br /><br />
      <code>freeze</code> <code>deep-sleep</code> <code>0 B</code> <code>wake-on-connect</code>
    </td>
  </tr>
</table>

---

## One panel. Every machine.

Carbon Panel runs Minecraft servers wherever you have machines &mdash; the PC in your room, a box
in the closet, a VPS in the cloud &mdash; and manages them as one fleet behind one address. Put
each world on the machine that suits it, spread the load across the hardware you already own,
and move anything between nodes as your players change.

| | |
|---|---|
| **Live migration** | Move a server to another node from the panel. The world transfers, the route is repointed and the container starts on the target &mdash; same hostname the whole way. |
| **Standby replicas** | Keep a copy of a world warm on a second machine. Promote it and players are back on the same address with the last state that reached the replica. |
| **Scheduled backups** | Snapshots run on a cron or interval schedule, flush saves through RCON first, respect retention rules, and upload to any S3-compatible bucket. |
| **Capacity guard** | Every placement is measured against the target node's real memory headroom, so a migration or a wake never oversubscribes a machine. |

---

## Quick start

### 1. Build the image

```bash
git clone https://github.com/athNdev/carbon-panel.git
cd carbon-panel

docker build -f docker/Dockerfile.carbon-panel -t carbon-panel:latest .
```

### 2. Bring the stack up

```bash
docker compose up -d
```

Then open <http://localhost:8080>.

<details>
<summary><b>Deploy with <code>docker run</code> instead</b></summary>
<br />

```bash
docker run -d \
  --name carbon-panel \
  --restart unless-stopped \
  --network host \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v ./data:/app/data \
  -v ./backups:/app/backups \
  -v ./tmp:/app/tmp \
  -v ./config.yaml:/app/config.yaml:ro \
  -e CARBONPANEL_DATA_DIR=/app/data \
  -e CARBONPANEL_HOST_DATA_PATH="$(pwd)/data" \
  -e TZ=UTC \
  carbon-panel:latest
```

</details>

<details>
<summary><b>Build from source</b></summary>
<br />

Requirements: **Go** (v1.26+) and **Bun** (v1.2+).

```bash
# Generate the RPC/API code using buf in docker (optional if pre-generated)
docker run --rm -v "$(pwd):/workspace" -w /workspace bufbuild/buf:latest generate

# Install dependencies and build the frontend
cd web/carbon-panel && bun install && bun run build && cd ../..

# Build the backend
go build -o carbon-panel cmd/carbon-panel/main.go

# Start Carbon Panel
./carbon-panel
```

</details>

<details>
<summary><b>Live development (hot reloading)</b></summary>
<br />

Starts the Go backend (Air live reload) and the SvelteKit frontend (Vite HMR) together:

```bash
# Bun
bun run dev

# or Make
make dev

# or Windows (PowerShell)
.\scripts\dev.ps1

# or Linux/macOS (Bash)
./scripts/dev.sh
```

- **Frontend (Vite HMR)**: <http://localhost:5174> &mdash; auto-proxies API, RPC and WebSocket traffic to `:8080`
- **Backend API**: <http://localhost:8080> &mdash; automatically rebuilds and restarts on Go source changes

</details>

---

## Docker deployment

The Compose file in the repository is the recommended shape. Host networking keeps the proxy
and every server port reachable without extra mapping; bridge mode works too and Carbon Panel
attaches itself to the managed Docker network on startup.

```yaml
services:
  carbon-panel:
    image: carbon-panel:latest
    container_name: carbon-panel
    restart: unless-stopped

    # Recommended: host network mode
    network_mode: host

    # Bridge mode with port mapping
    # ports:
    #   - "8080:8080"         # Carbon Panel web interface
    #   - "25565:25565"       # Minecraft default proxy port
    #   - "25565-25665:25565-25665/tcp"

    volumes:
      # Docker socket for orchestrating server containers
      - /var/run/docker.sock:/var/run/docker.sock
      # Data directories on the host
      - ./data:/app/data
      - ./backups:/app/backups
      - ./tmp:/app/tmp
      #- ./config.yaml:/app/config.yaml:ro

    environment:
      - CARBONPANEL_DATA_DIR=/app/data
      - CARBONPANEL_HOST_DATA_PATH=/opt/carbon-panel/data
      - TZ=UTC

    extra_hosts:
      - "host.docker.internal:host-gateway"
```

---

## Configuration

Carbon Panel loads configuration from `config.yaml` or environment variables:

```yaml
server:
  port: "8080"
  host: "0.0.0.0"

storage:
  data_dir: "./data"
  backup_dir: "./backups"

proxy:
  enabled: true
  base_url: "minecraft.yourdomain.com"
  listen_ports: [25565]
```

---

## API &amp; extensibility

A comprehensive Connect-RPC &amp; gRPC API, typed end to end with Protocol Buffers:

```bash
# List managed servers
curl http://localhost:8080/carbonpanel.v1.ServerService/ListServers

# Restart a server
curl -X POST http://localhost:8080/carbonpanel.v1.ServerService/RestartServer \
  -H "Content-Type: application/json" \
  -d '{"id": "your-server-id"}'
```

---

## How it compares

| Capability | Carbon Panel | Raw Docker Compose | Typical free panel |
|---|---|---|---|
| Per-server container isolation | &#9989; Built-in | &#9989; Manual | &#10070; Sometimes |
| Hostname-based player routing | &#9989; One port | &#10060; DIY | &#10060; Rare |
| Modpack studio (CurseForge / Modrinth) | &#9989; Keyless | &#10060; &mdash; | &#10070; Add-ons |
| Multi-node clustering | &#9989; First-class | &#10070; DIY | &#10070; Paid tiers |
| Deep-sleep &amp; wake-on-connect | &#9989; Zero-RAM idle | &#10060; &mdash; | &#10060; &mdash; |
| Typed API &amp; Prometheus metrics | &#9989; Connect RPC | &#10060; Shell | &#10070; Varies |
| Open source &amp; self-hosted | &#9989; MPL-2.0 | &#9989; Yours | &#10070; Varies |

<sub>Comparison reflects publicly documented, commonly available capabilities and is a general
guide &mdash; evaluate each option against your own requirements.</sub>

---

## Acknowledgments &amp; credits

Carbon Panel (formerly MineServer) is built upon the open-source foundation of the original
[DiscoPanel](https://github.com/discohaus/discopanel) project created by
[nickheyer](https://github.com/nickheyer).

---

## License

Carbon Panel's own code is licensed under the **Mozilla Public License 2.0**
([LICENSE.MPL-2.0](LICENSE.MPL-2.0)) &mdash; free as in cost, free as in speech, and permanently
open.

Material inherited from DiscoPanel remains under the **MIT License**
([LICENSE](LICENSE)), Copyright (c) 2025 Nicholas W. Heyer. [LICENSING.md](LICENSING.md)
documents the file-by-file boundary between the two.
