# ADR 0003 — Node model: BYO nodes and managed nodes share one agent and one registry

- Status: accepted
- Date: 2026-09-21

## Context

Carbon Cloud must let a tenant (a) bring their own machine and (b) buy a managed
node provisioned for them. Both end up as "a Docker host that runs Minecraft
containers for this org". If these were two different subsystems, placement,
capacity, health, authorization and the UI would each need two code paths.

OSS Carbon Panel already models remote nodes as *Docker Engine API endpoints*
(`internal/docker/pool.go` dials `node_id` → host with optional TLS), which
requires the controller to reach **into** the node. That is wrong for a managed
cloud: BYO nodes are typically behind NAT with no inbound ports, and we should
not ask tenants to expose a Docker daemon to the internet.

## Decision

One node abstraction, **outbound-only** agent, two onboarding paths.

- **Every node runs `cloudnoded`**, the node agent, and dials **out** to the
  control plane over TLS with a per-node client certificate. No inbound port is
  required, ever. The Docker socket stays local to the node.
- **The registry is identical** for both kinds. A `nodes` row records
  `origin = byo | managed`, `provider`, `node_type`, `region`, capacity, status,
  agent version and last heartbeat. Placement and capacity accounting do not care
  which origin a node has.
- **BYO onboarding:** a tenant creates a join token in the console
  (`nodes:create_join_token`), runs a one-liner on their host
  (`cloudctl node join --token …`), and the node exchanges the token for an
  identity: node id + client certificate signed by the control-plane CA. Tokens
  are HMAC-signed, org-bound, expiring, single-use, and revocable.
- **Managed onboarding:** the tenant picks a **node type** (cpu/ram/disk/price
  class) and a region; the control plane renders a Terraform workspace from the
  selected provider module, `plan`s it, shows the plan, and on approval `apply`s
  it. The instance's cloud-init installs `cloudnoded` with a join token pinned to
  that node id. From the registry's point of view a managed node registers exactly
  like a BYO one.
- **Node types** are a catalog owned by the control plane: a type maps to a
  resource envelope (vcpu, ram_mb, disk_gb) and a per-provider instance type and
  price. Adding a provider means adding instance-type mappings, not new concepts.

## Consequences

- NAT-friendly, and the tenant's Docker daemon is never exposed.
- OSS `ClientPool`-style inbound dialing is **not** used by the cloud control
  plane. It stays an OSS feature. (If we ever need it for an enterprise
  self-hosted node, it becomes an additional transport behind the same agent
  protocol, not a second registry.)
- The control plane needs a CA (or an external one) to issue node certs; this is
  part of L2. Until then, a shared-secret HMAC agent token is the fallback
  transport, and the transport is an interface so the switch is contained.
- A node that violates its declared capacity is rejected by the control plane at
  placement time regardless of what it reports.
