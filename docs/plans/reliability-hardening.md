# Carbon Panel reliability hardening plan

Status: active
Scope: runtime, persistence, Docker orchestration, APIs, UI, packaging, upgrades,
and supported operating systems.

## Audit findings and work order

| Priority | Surface | Finding | State |
|---|---|---|---|
| P0 | Developer lifecycle | `make clean`, `make dev`, and Docker dev targets could delete `./data`, `/tmp/carbon-panel`, or Compose volumes containing user data. | Fixed; dry-run output confirms clean removes only the generated binary and dev targets preserve Compose volumes. |
| P1 | Minecraft container bootstrap | Dropping all capabilities and read-only rootfs blocked upstream bootstrap operations. | Fixed in main (`b67d9f8b`); recheck default and override paths. |
| P1 | Container security | Audit required for required capabilities, host mounts, root user, readonly paths, and parity across server/module/backup paths. | Planned. |
| P1 | Persistence and upgrades | Audit SQLite migration rollback, backup restore, disk-full behavior, and shutdown consistency. | Planned. |
| P1 | Orchestration | Audit Docker daemon restart, container recreation, scheduler/reconciler races, and remote-node failure handling. | Planned. |
| P1 | Startup and health | Add explicit readiness semantics for Docker availability, migrations, and API availability. | Planned. |
| P2 | Portability | Audit path handling, interface/port detection, process control, signals, and filesystem semantics on Windows and Linux. | Active: native Windows `go test ./...` fails in POSIX permission, shell-stub, and Linux bind-path tests; WSL Go toolchain unavailable. Establish platform support and make tests/runtime behavior explicit. |
| P2 | External dependencies | Audit timeouts, retry/backoff, response-size limits, cancellation, and error classification for HTTP/RCON/registry calls. | Planned. |
| P2 | Auth and API | Audit fail-closed behavior, RPC coverage, authorization scope, and user-visible error recovery. | Planned. |
| P2 | Frontend | Audit loading/error/empty states, stale requests, reconnect behavior, accessibility, and API-version compatibility. | Planned. |
| P2 | Build and release | Pin mutable build inputs, validate clean-checkout builds, and exercise upgrade/rollback artifacts in CI. | Planned. |

## Acceptance

- Every P0/P1 row has a regression test or an explicitly documented test seam.
- Clean, interrupted, and repeated startup/upgrade paths preserve server data.
- Deployment health distinguishes a running process from a service ready to
  accept requests.
- CI covers Linux and Windows runtime-sensitive code and validates deploy files.
- Build artifacts identify source revision and use pinned tool/image versions.
- No host-specific credentials, addresses, databases, indexes, or generated
  state are required for a clean checkout.

## Research references

- Docker capabilities and `--cap-drop`: <https://docs.docker.com/engine/containers/run/>
- Docker Compose dependency health semantics:
  <https://docs.docker.com/compose/how-tos/startup-order/>
- Docker Engine security model: <https://docs.docker.com/engine/security/>
