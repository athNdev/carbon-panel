# Changelog

All notable changes to Carbon Panel are documented here.
Format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- Global `+error.svelte` boundary with retry/home affordances.
- Server list now surfaces fetch failures with a retry action.
- ConnectRPC `buf breaking` CI job.
- Per-route error/empty/loading states (in progress).
- Reusable `CarbonErrorState` and `CarbonEmptyState` components exported from `carbon/index.ts`.
- Retryable error states for dashboard, ActiveModules, profile (API tokens), and settings (global settings).
- Docker daemon reachability/config integration tests behind the `integration` build tag (`internal/docker/integration_test.go`).

### Fixed
- Surface unchecked packwiz import/export and proxy route-refresh write errors.
- Make upload tests OS-independent (Windows temp-file locks, huge allocations).
- Accessibility gaps (aria-invalid/describedby, modal focus trap, table scope, reduced motion, toast aria-live).
- RBAC no longer denies every RPC for Clerk sessions whose org is not yet linked: falls back to the bootstrap default organization and auto-provisions membership.

## [3.0.0-beta.3] - 2026-10-21

### Added
- Deep-sleep / wake-on-connect handling for Minecraft servers.
- Scheduler backup pruning and snapshot rollback safety checks.
- Module template and modpack studio improvements.

### Changed
- Proxy routing and node capacity handling hardened for remote nodes.

## [2.0.15] - 2026-09-30

### Added
- Multi-node clustering and remote Docker management.
- OIDC authentication providers.
- Modpack import/export and CurseForge/Modrinth indexers.

## [1.0.38] - 2026-09-15

### Added
- Initial single-node Minecraft server management.
- Web UI with Carbon Design System.
- Docker-based server isolation.

[Unreleased]: https://github.com/athNdev/carbon-panel/compare/v3.0.0-beta.3...HEAD
[3.0.0-beta.3]: https://github.com/athNdev/carbon-panel/releases/tag/v3.0.0-beta.3
[2.0.15]: https://github.com/athNdev/carbon-panel/releases/tag/v2.0.15
[1.0.38]: https://github.com/athNdev/carbon-panel/releases/tag/v1.0.38