# ADR 0001 — Migrate Docker Go SDK to moby/moby/{client,api}

- Status: accepted
- Date: 2026-09-25
- Plane: MINE-134
- PR: projects/carbon-panel#2

## Context

`github.com/docker/docker` (`v28.5.2+incompatible`) is a deprecated,
frozen module: no fixes, widening drift toward v29, and it drags
daemon-side advisories (GO-2026-4887/4883) that `govulncheck` can only
excuse as N/A. The maintained SDK is the split
`github.com/moby/moby/client` + `github.com/moby/moby/api` pair.

## Decision

Migrate to `moby/moby/client v0.6.0` + `moby/moby/api v1.56.0`;
`go.mod` carries zero `docker/docker` references. Adapt to the new
Options/Result call shapes rather than shimming the old ones:

- List/inspect/create/remove/stop/start/kill/pause/unpause/stats/exec/
  pull/network calls use single `XxxOptions` structs and return
  `(XxxResult, error)`; list results expose `.Items`.
- `Events()` returns `EventsResult{Messages, Err}` with no error return;
  callers select on the channels (reconnect/backoff unchanged).
- `Ping` takes `PingOptions`; eager negotiation is
  `PingOptions{NegotiateAPIVersion: true}` (`NegotiateAPIVersionPing`
  is gone).
- `container.NetworkSettings` / `EndpointSettings.IPAddress`,
  `HostConfig.DNS`, and `PortBinding.HostIP` are `netip.Addr`:
  parse at the boundary (`parseDNSAddrs`, `PortFrom`/`ParsePort`,
  `MustParseAddr` for constants); unparsable DNS entries are skipped
  (daemon default applies) instead of failing the whole create.
- `network.Port` is a struct: build `PortSet`/`PortMap` via
  `PortFrom`/`ParsePort`, not string keys.
- Pool TLS: the SDK no longer exposes the transport, so the custom
  TLS config (incl. skip-verify) rides a cloned `http.DefaultTransport`
  via `WithHTTPClient` (TCP branch only, as before).
- The removed `client.CommonAPIClient` is replaced by the narrow,
  panel-owned `proxy.DockerInspector` (ContainerInspect only); fakes
  implement it.

## Consequences

- Govulncheck Moby findings lose their source module; the N/A excuse
  in `ci.yml` can be dropped once CI confirms.
- Callers must pass `XxxOptions{}` even for empty options; mocks must
  return `XxxResult` wrappers.
- `netip.Addr` zero value (`IsValid() == false`) replaces `""` checks
  for IPs throughout proxy/reconciler/docker.
