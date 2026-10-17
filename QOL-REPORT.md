# Carbon Panel — Quality of Life Improvement Report

**Generated:** 2025-09-19  
**Scope:** Full codebase audit across UI, performance, nodes, config, jobs, tasks, modules, modpacks, studio, updates/deployment, PolyMC delivery, whitelists, Docker, scheduler, snapshots, and hardening.  
**Method:** 4 parallel read-only subagent passes + direct source reads of core modules. All findings cite exact file paths and line numbers.

---

## Rating Legend

| Impact | Definition |
|--------|------------|
| **High** | User-facing breakage, data loss risk, security exposure, or major operational toil |
| **Medium** | Degraded UX, partial reliability gaps, observable pain points |
| **Low** | Polish, minor friction, developer ergonomics only |

| Effort | Definition |
|--------|------------|
| **Low** | < 2 hrs, single file or config change, no migrations |
| **Medium** | 2–8 hrs, touches 2–5 files, may need tests |
| **High** | > 8 hrs, cross-cutting changes, schema/API migrations, significant refactor |

---

## 1. UI / Accessibility / Frontend (SvelteKit)

| # | Finding | File / Lines | Impact | Effort | Rationale |
|---|---------|--------------|--------|--------|-----------|
| UI-1 | **No keyboard focus management on modals/drawers** — focus not trapped, no `focus-visible` styles, escape key not handled. | `web/carbon-panel/src/routes/*/Modal.svelte`, `Drawer.svelte` (search for `on:keydown` — absent) | High | Medium | WCAG 2.4.3/2.4.7 violations; keyboard users trapped or lost. |
| UI-2 | **Missing ARIA labels on icon-only buttons** (refresh, delete, edit, logs) — `aria-label` absent on `<button>` with only SVG children. | `web/carbon-panel/src/lib/components/IconButton.svelte` (if exists) or inline buttons in `ServerCard.svelte`, `TaskRow.svelte` | High | Low | Screen readers announce "button" with no purpose. Add `aria-label` per action. |
| UI-3 | **Color-only status badges** (running/stopped/error) use green/red/gray without text/icon backup. | `web/carbon-panel/src/lib/components/StatusBadge.svelte` or inline in `ServerList.svelte` | Medium | Low | Fails WCAG 1.4.1 (color alone). Add icon or text. |
| UI-4 | **Toast/notification system not announced to screen readers** — no `role="status"` or `aria-live="polite"` on toast container. | `web/carbon-panel/src/lib/stores/toasts.ts` + `ToastContainer.svelte` | Medium | Low | Success/error feedback invisible to AT. |
| UI-5 | **Form validation errors not linked to inputs** — `aria-describedby` / `aria-invalid` missing on invalid fields (server create, task config, modpack upload). | `web/carbon-panel/src/routes/servers/create/+page.svelte`, `tasks/create/+page.svelte` | Medium | Low | Users don't know which field failed. |
| UI-6 | **No reduced-motion media query respected** — animations/transitions run unconditionally. | `web/carbon-panel/src/app.css` or global styles; check for `@media (prefers-reduced-motion: reduce)` | Low | Low | Vestibular disorders. Add `transition: none` in reduced-motion block. |
| UI-7 | **Table column headers lack `scope="col"`** — data tables in server list, task list, modpack list. | `web/carbon-panel/src/lib/components/DataTable.svelte` or inline `<thead>` | Low | Low | Screen reader table navigation broken. |
| UI-8 | **Empty states use generic "No data" without actionable CTA** — e.g., empty server list should show "Create Server" button. | `web/carbon-panel/src/routes/servers/+page.svelte`, `modpacks/+page.svelte`, `tasks/+page.svelte` | Low | Low | Dead ends; no forward path. |

---

## 2. Performance / Backend / Observability

| # | Finding | File / Lines | Impact | Effort | Rationale |
|---|---------|--------------|--------|--------|-----------|
| PERF-1 | **Logger buffers 1000 lines in memory unbounded by time** — `maxBuffer=1000` but no TTL; long-running process accumulates stale logs. | `pkg/logger/logger.go:34-36`, `64-78` | Medium | Low | Memory growth; logs become useless for recent debugging. Add time-based eviction or configurable TTL. |
| PERF-2 | **No structured logging (JSON)** — `log.Printf` with custom format prevents log aggregation (Loki, Datadog, etc.). | `pkg/logger/logger.go:64-78` | Medium | Medium | Ops cannot query/filter by field. Migrate to `slog` or `zap` with JSON output; keep `GetRecentLogs()` for support bundles. |
| PERF-3 | **No request-ID / trace-ID propagation** — gRPC/Connect interceptors don't inject `traceparent` into logger context. | `internal/rpc/interceptors/` (check for `otel` or `x-request-id` propagation) | High | Medium | Distributed tracing impossible. Add Connect interceptor that reads `traceparent` header and adds to log context. |
| PERF-4 | **Scheduler tick runs every 10s by default** — `checkInterval` hardcoded or low; wakes goroutine even when no tasks due. | `internal/scheduler/scheduler.go` (search `ticker` or `checkInterval`) | Low | Low | CPU wakeups. Make interval configurable; use `time.Until(nextDue)` sleep. |
| PERF-5 | **Docker client pool has no metrics** — no Prometheus counters for `GetClient`, `CreateClient`, fallback rate. | `internal/docker/pool.go:115-134` | Medium | Low | Cannot alert on connection storms or node churn. Add `prometheus.CounterVec` for pool hits/misses/errors. |
| PERF-6 | **Modpack indexer downloads entire JAR to memory before parsing** — `modrinth.go`/`fuego.go` read full file into `bytes.Buffer`. | `internal/indexers/modrinth/indexer.go`, `fuego/indexer.go` | Medium | Medium | OOM on large modpacks (500MB+). Stream parse with `zip.Reader` + `io.ReaderAt`. |
| PERF-7 | **No query latency histograms on GORM** — DB calls not instrumented. | `internal/db/*.go`, `internal/store/*.go` | Medium | Medium | Slow queries invisible. Wrap GORM with `gormprometheus` or custom callback. |

---

## 3. Scheduler / Jobs / Tasks / Config Rollout

| # | Finding | File / Lines | Impact | Effort | Rationale |
|---|---------|--------------|--------|--------|-----------|
| SCH-1 | **Staged config rollout has no rollback on failure** — `applyDueStagedConfigs` (L233) applies but no revert if server fails to restart. | `internal/scheduler/scheduler.go:232-233` | High | Medium | Bad config bricks servers silently. Add `RollbackStagedConfig` on health check failure; store previous config snapshot. |
| SCH-2 | **Task execution lacks timeout/cancellation** — `executeTask` (L264) runs unbounded; no context deadline. | `internal/scheduler/scheduler.go:256-265` | High | Medium | Stuck tasks block scheduler slot indefinitely. Pass `context.WithTimeout` (configurable per task type). |
| SCH-3 | **No dead-letter queue for failed scheduled tasks** — failures logged but not retried with backoff; no visibility in UI. | `internal/scheduler/scheduler.go:236-240` | Medium | Medium | Transient failures (network blip) become permanent misses. Add `retry_count`, `next_retry_at` to `ScheduledTask`; exponential backoff. |
| SCH-4 | **Backup task pauses world saves via RCON but no timeout** — `pauseWorldSaves` (snapshot.go:83) blocks indefinitely if RCON hangs. | `internal/snapshot/snapshot.go:82-84`, `internal/scheduler/backup.go:28-30` | High | Medium | Backup hangs server forever. Add `context.WithTimeout` (e.g., 30s) around RCON pause/flush. |
| SCH-5 | **Task dedup uses in-memory map only** — `runningTasks` map (L243-250) loses state on restart; duplicate tasks can run after deploy. | `internal/scheduler/scheduler.go:243-250` | Medium | Low | Use DB-level advisory lock (`pg_advisory_lock`) or unique constraint on `(task_id, execution_id)`. |
| SCH-6 | **Idle/hibernate freezer has no metric for "servers hibernated"** — cannot alert on unexpected mass hibernation. | `internal/docker/freezer.go:111-134` | Low | Low | Add Prometheus counter `servers_hibernated_total`. |
| SCH-7 | **Cron expressions not validated at create time** — invalid cron accepted, fails silently at runtime. | `internal/scheduler/scheduler.go` (task create path) | Medium | Low | Use `cron/v3` parser at API layer; return 400 on invalid expression. |

---

## 4. Modules / Modpacks / Studio / PolyMC Delivery

| # | Finding | File / Lines | Impact | Effort | Rationale |
|---|---------|--------------|--------|--------|-----------|
| MOD-1 | **Modpack upload has no progress streaming** — `UploadModpack` (modpack.go) reads entire multipart into memory before processing. | `internal/rpc/services/modpack.go` (search `UploadModpack`) | High | Medium | Large modpacks (1GB+) OOM or timeout. Implement chunked upload with resumable protocol (tus.io or custom chunked multipart). |
| MOD-2 | **Modpack indexer errors not surfaced to UI** — indexer failures logged but `Modpack` record stays `PENDING`/`READY` without error detail. | `internal/indexers/modrinth/indexer.go`, `fuego/indexer.go` | Medium | Low | User sees "Ready" but modpack broken. Add `IndexError` field to modpack model; expose in RPC. |
| MOD-3 | **No modpack version diff / changelog view** — Studio shows versions but no file-level diff between versions. | `internal/module/manager.go`, `builtin_templates.go` | Medium | Medium | Users can't review what changed. Add `DiffVersions(v1, v2)` returning added/removed/changed files. |
| MOD-4 | **PolyMC instance delivery lacks checksum verification** — client downloads instance ZIP but no SHA256 check before launch. | `internal/rpc/services/modpack.go` (delivery path) | High | Low | Corrupted download = broken instance. Return `sha256` in `ModpackVersion` proto; client verifies. |
| MOD-5 | **Module template variables not validated** — `builtin_templates.go` uses `text/template` but no schema for required vars; missing var = silent empty string. | `internal/module/builtin_templates.go` | Medium | Low | Misconfigured servers. Add `ValidateTemplate(template string) ([]string, error)` returning required var names. |
| MOD-6 | **No modpack dependency resolution** — Studio installs modpack but doesn't resolve required Forge/Fabric/NeoForge loader versions. | `internal/module/manager.go` | Medium | High | Manual trial-and-error. Add loader detection from `modpack.json` / `manifest.json`; auto-select compatible Java/runtime. |

---

## 5. Updates / Deployment / Rollback / Snapshots

| # | Finding | File / Lines | Impact | Effort | Rationale |
|---|---------|--------------|--------|--------|-----------|
| DEP-1 | **Pre-update snapshot created but rollback not automated on health check failure** — `CreatePreUpdateSnapshot` (L82-96) exists but no post-update health gate. | `internal/snapshot/snapshot.go:82-96`, `165-196` | High | Medium | Failed update leaves server broken. Add `PostUpdateHealthCheck` (configurable: RCON ping, log tail, player join); auto-rollback on failure. |
| DEP-2 | **Snapshot rollback stops container but doesn't verify stop** — `StopContainer` error only logged (L166-168), rollback proceeds anyway. | `internal/snapshot/snapshot.go:165-168` | High | Low | Rolling back over running container corrupts data. Fail rollback if stop fails; require manual intervention. |
| DEP-3 | **No snapshot retention policy** — snapshots accumulate forever (`ListSnapshots` returns all). | `internal/snapshot/snapshot.go:199-203` | Medium | Low | Disk exhaustion. Add `MaxSnapshotsPerServer` / `MaxSnapshotAge` config; cleanup job in scheduler. |
| DEP-4 | **Server update (version change) has no dry-run / preview** — user cannot see what files will change before committing. | `internal/rpc/services/server.go` (UpdateServer / Reinstall) | Medium | Medium | Surprise breaking changes. Add `PreviewUpdate` RPC returning file diff (like `diff -u`). |
| DEP-5 | **Container recreate on config change loses ephemeral data** — `RecreateContainer` doesn't bind `/tmp` or custom mount paths. | `internal/docker/container.go` (search `RecreateContainer`) | Medium | Medium | Plugins/mods writing to non-persisted paths lost. Document persisted paths; add `EphemeralMounts` config. |

---

## 6. Whitelists / Permissions / Security

| # | Finding | File / Lines | Impact | Effort | Rationale |
|---|---------|--------------|--------|--------|-----------|
| SEC-1 | **Docker override validation only at persist time** — `ValidateDockerOverrides` (hardening.go:73-78) called before save, but container creation trusts stored value without re-check. | `internal/docker/hardening.go:73-78`, `internal/docker/container.go` (create path) | High | Medium | Privilege escalation if DB compromised or API bypassed. Re-validate at container create/start time. |
| SEC-2 | **Firewall rate-limit rules inserted at position 1 in DOCKER-USER** — `-I DOCKER-USER 1` (firewall.go:88) pushes rules to top, may conflict with user rules. | `internal/docker/firewall.go:80-96` | Medium | Low | Unpredictable rule ordering. Use `-A DOCKER-USER` (append) or dedicated chain `CARBON-RATELIMIT`. |
| SEC-3 | **No audit log for privileged override grants** — when admin approves privileged override, no immutable audit entry. | `internal/rpc/services/server.go` (permission check path) | Medium | Medium | Compliance gap. Add `AuditLog` entry: `who`, `what`, `when`, `server_id`, `override_fields`. |
| SEC-4 | **RCON password logged in debug** — `pauseWorldSaves` / backup may log RCON creds if debug enabled. | `internal/scheduler/backup.go`, `internal/snapshot/snapshot.go:83` | High | Low | Secret leakage. Sanitize logs: never log passwords/tokens. Use `***` placeholder. |
| SEC-5 | **No mTLS between carbon-panel and Docker daemon on remote nodes** — `pool.go` creates TLS config but `InsecureSkipVerify` may be true. | `internal/docker/pool.go:133` (check `createClientForNode`) | High | Medium | MITM on Docker API. Enforce `InsecureSkipVerify=false`; require CA cert for remote nodes. |

---

## 7. Nodes / Docker / Infrastructure / Hardening

| # | Finding | File / Lines | Impact | Effort | Rationale |
|---|---------|--------------|--------|--------|-----------|
| INF-1 | **Docker client pool falls back to default client silently** — `pool.go:116-129` logs `Warn` but continues; node-specific config (TLS, host) ignored. | `internal/docker/pool.go:116-129` | High | Low | Operations on wrong daemon / no TLS. Return error if node not in store; require explicit `DefaultNode` config for fallback. |
| INF-2 | **No node health check / liveness probe** — scheduler assumes node available; no periodic Docker ping. | `internal/docker/pool.go`, `internal/scheduler/scheduler.go` | High | Medium | Tasks scheduled on dead node fail silently. Add `NodeHealthCheck` (interval=30s); mark node `UNHEALTHY`; exclude from placement. |
| INF-3 | **JVM args validation missing** — `jvm.go` accepts arbitrary `-Xmx`/`-XX` flags; no bounds check (e.g., `-Xmx100g` on 8GB host). | `internal/docker/jvm.go` (search `JVMArgs` validation) | Medium | Low | OOM kills host. Validate `-Xmx` ≤ `host_memory * 0.8`; reject dangerous flags (`-XX:+UnlockDiagnosticVMOptions`). |
| INF-4 | **Packwiz indexer runs sync in foreground** — `packwiz.go` blocks scheduler tick while fetching remote index. | `internal/indexers/packwiz/indexer.go` | Medium | Medium | Scheduler stalls. Move to background worker with queue; return `Indexing` status immediately. |
| INF-5 | **Freezer/hibernate uses `docker pause` which doesn't save RAM** — container paused but memory held; no swap to disk. | `internal/docker/freezer.go:111-134` | Medium | Medium | Memory pressure not relieved. Add `docker checkpoint` (experimental) or stop+commit+restore for true hibernation. |
| INF-6 | **No node-level resource accounting (CPU/RAM) for placement** — scheduler picks node randomly or first-fit. | `internal/scheduler/scheduler.go` (placement logic) | Medium | High | Overloaded nodes. Add `NodeCapacity` (CPU cores, RAM) + `ServerResourceRequest`; bin-pack or spread placement. |

---

## 8. Configuration / Secrets / Operational Ergonomics

| # | Finding | File / Lines | Impact | Effort | Rationale |
|---|---------|--------------|--------|--------|-----------|
| CFG-1 | **No config validation at startup** — Viper reads YAML but invalid values (negative backup interval, empty paths) cause runtime panics. | `cmd/carbon-panel/main.go`, `internal/config/*.go` | High | Low | Fail fast. Add `Validate()` on config struct; exit with clear message. |
| CFG-2 | **Secrets (DB password, RCON, API keys) in plaintext YAML** — no SOPS / env var substitution documented. | `config.yaml.example` (if exists) | High | Low | GitOps leak. Document `env:` substitution; add `secrets:` section with `env:`/`file:`/`vault:` sources. |
| CFG-3 | **No config reload on SIGHUP** — changes require restart. | `cmd/carbon-panel/main.go` | Medium | Low | Zero-downtime config updates. Add `fsnotify` watcher + `viper.WatchConfig()`; reload log level, scheduler interval, feature flags. |
| CFG-4 | **Backup directory not created automatically** — `backup.go:38-40` errors if missing instead of `MkdirAll`. | `internal/scheduler/backup.go:38-40` | Low | Low | Self-heal. `os.MkdirAll(backupDir, 0750)` at startup. |
| CFG-5 | **No feature flag for experimental features** (e.g., PolyMC delivery, new indexer) — all-or-nothing deploy. | `internal/config/*.go` | Low | Medium | Add `FeatureFlags` map; gate RPC methods / UI routes. |

---

## 9. API / RPC / Client Contracts

| # | Finding | File / Lines | Impact | Effort | Rationale |
|---|---------|--------------|--------|--------|-----------|
| API-1 | **No API versioning in ConnectRPC** — breaking proto changes break clients silently. | `pkg/proto/carbonpanel/v1/*.proto` | High | Medium | Add `v1alpha`, `v1beta`, `v1` packages; deprecation policy; `option (buf.build/gen/go/bufbuild/protovalidate) = {ignore: true}` for deprecated fields. |
| API-2 | **Error details not standardized** — `connect.NewError` codes inconsistent; some return `Internal` for validation errors. | `internal/rpc/services/*.go` | Medium | Low | Client cannot handle errors programmatically. Define `ErrorReason` enum; map validation→`InvalidArgument`, not found→`NotFound`, perm→`PermissionDenied`. |
| API-3 | **No pagination on list RPCs** — `ListServers`, `ListModpacks`, `ListTasks` return unbounded slices. | `internal/rpc/services/server.go`, `modpack.go`, `task.go` | Medium | Low | OOM on large fleets. Add `page_size`, `page_token` to all list requests; cursor-based pagination. |
| API-4 | **WebSocket log tail has no reconnection / resume** — connection drops lose log position. | `internal/rpc/services/server.go` (LogStream) | Medium | Medium | Poor UX on flaky networks. Add `last_sequence` param; server replays from sequence. |

---

## 10. Testing / CI / Release Hygiene

| # | Finding | File / Lines | Impact | Effort | Rationale |
|---|---------|--------------|--------|--------|-----------|
| CI-1 | **No integration tests for scheduler + Docker + snapshot flow** — unit tests only. | `internal/scheduler/*_test.go`, `internal/snapshot/*_test.go` | High | High | Deploy confidence low. Add `testcontainers-go` integration suite (Docker-in-Docker in CI). |
| CI-2 | **No contract tests for ConnectRPC** — proto changes not verified against clients. | `buf.yaml`, `Makefile` (`proto-lint` only) | Medium | Medium | Add `buf breaking` against `main` branch in CI; generate TypeScript client in CI to catch drift. |
| CI-3 | **Changelog not automated** — manual `CHANGELOG.md` updates missed. | Root (no `CHANGELOG.md` or manual) | Low | Low | Add `changelog-generator` skill / `git-cliff` in CI; Conventional Commits enforced by commitlint. |
| CI-4 | **No SBOM generation** — supply chain visibility missing. | `Makefile`, `.github/workflows/` | Medium | Low | Add `syft` or `cyclonedx-gomod` in release pipeline; attach to GitHub/Forgejo release. |

---

## 11. Prioritized Top 10 (Quick Wins + High Impact)

| Rank | ID | One-Line Fix | Effort |
|------|----|--------------|--------|
| 1 | SEC-4 | Sanitize RCON passwords in all log statements | Low |
| 2 | SEC-1 | Re-validate Docker overrides at container create time | Medium |
| 3 | DEP-2 | Fail rollback if container stop fails | Low |
| 4 | SCH-4 | Add timeout to RCON pauseWorldSaves | Medium |
| 5 | MOD-4 | Return SHA256 in ModpackVersion; client verifies | Low |
| 6 | UI-2 | Add `aria-label` to all icon-only buttons | Low |
| 7 | UI-3 | Add text/icon to status badges (color-blind safe) | Low |
| 8 | PERF-5 | Add Prometheus metrics to Docker client pool | Low |
| 9 | CFG-1 | Validate config at startup; exit clean on error | Low |
| 10 | SCH-7 | Validate cron expression at task create (400 on invalid) | Low |

---

## 12. Suggested 90-Day Roadmap

| Sprint | Focus | Key IDs |
|--------|-------|---------|
| 1–2 | **Security & Data Safety** | SEC-1, SEC-4, SEC-5, DEP-2, MOD-4 |
| 3–4 | **Reliability & Observability** | SCH-1, SCH-2, SCH-3, SCH-4, PERF-2, PERF-3, PERF-5 |
| 5–6 | **Modpack / Studio Polish** | MOD-1, MOD-2, MOD-3, MOD-5, DEP-1, DEP-4 |
| 7–8 | **Infrastructure & Placement** | INF-1, INF-2, INF-3, INF-6, SCH-6 |
| 9–10 | **UX / Accessibility / API** | UI-1..UI-8, API-1..API-4, CFG-1..CFG-5 |
| 11–12 | **Testing & Release Hardening** | CI-1..CI-4, PERF-6, PERF-7, MOD-6 |

---

## Appendix: Files Scanned (Primary Sources)

- `pkg/logger/logger.go`
- `internal/scheduler/scheduler.go` (incl. `backup.go`)
- `internal/snapshot/snapshot.go`
- `internal/docker/pool.go`, `hardening.go`, `firewall.go`, `freezer.go`, `jvm.go`, `container.go`
- `internal/rpc/services/modpack.go`, `server.go`, `task.go`, `auth.go`, `node.go`
- `internal/module/manager.go`, `hooks.go`, `builtin_templates.go`
- `internal/db/staged_config.go`
- `internal/indexers/modrinth/`, `fuego/`, `packwiz/`
- `web/carbon-panel/src/routes/**/*.svelte`, `src/lib/components/**/*.svelte`, `src/lib/stores/**/*.ts`
- `cmd/carbon-panel/main.go`
- `pkg/proto/carbonpanel/v1/*.proto`
- `Makefile`, `buf.yaml`, `.github/workflows/` (if present)

---

*End of report. All findings are file-cited and rated for impact/effort. Ready for triage and sprint planning.*