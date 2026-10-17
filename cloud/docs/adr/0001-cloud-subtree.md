# ADR 0001 — Carbon Cloud lives additively under `cloud/` in this repository

- Status: accepted
- Date: 2026-09-21

## Context

Carbon Cloud is a proprietary managed-cloud downstream of the open-source Carbon
Panel. It needs its own control plane, provisioning, console and deployment
artifacts, but it must keep reusing the OSS data-plane primitives that already
work (`internal/docker`, `internal/proxy`, `internal/minecraft`, `internal/rcon`,
`internal/module`, `pkg/*`).

The options were: (a) fork the whole repository into a separate private repo,
(b) build inline inside `internal/` and branch on "cloud mode", (c) add a
self-contained `cloud/` subtree to the existing module.

## Decision

(c) Additive `cloud/` subtree in the existing Go module.

- `cloud/cmd/*` are new binaries; `cloud/internal/cloud/*` are new packages.
- `cloud/proto/cloud/v1` is a second proto module path, generated alongside the
  existing one.
- Cloud reuses OSS packages by importing them (`internal/...` is module-scoped, so
  a package under the same module root can import it).
- Cloud-specific behaviour is never expressed as `if cloudMode` inside OSS
  packages. If a shared primitive needs a seam, the seam is added to the OSS
  package as a neutral interface and the cloud implementation lives in `cloud/`.

## Consequences

- One `go test ./...` covers both; the OSS suite is a permanent regression gate
  for shared code.
- A future split into a private repo is a `git subtree split` away, and the shared
  code is already isolated by the rule above.
- Risk: someone puts cloud-only logic in an OSS package. Mitigated by the rule
  and by review.
- Proto generation for two paths in one `buf.yaml` needs the new module declared
  (done in P0/L0).
