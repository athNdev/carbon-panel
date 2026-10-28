<!--
SPDX-License-Identifier: MPL-2.0
This Source Code Form is subject to the terms of the Mozilla Public
License, v. 2.0. If a copy of the MPL was not distributed with this
file, You can obtain one at https://mozilla.org/MPL/2.0/.
-->

# Staging and applying Minecraft server changes

Date: 2026-10-28. Branch read: `feat/lifecycle-reliability` (includes `origin/main`). Primary sources are this tree's Go, proto, and Svelte. `docs/research/minecraft-sleep-deep-sleep.md` (2026-09-17) and `docs/runbook-sleep-baseline.md` are cited where they still match the code, and called out where this branch has moved on.

## Question

How can an admin stage updates to a Minecraft server's config, mods, datapacks, and other mutable server state without accidentally restarting, and later apply them on a schedule or after the server has been idle for X minutes, including while the server is in deep sleep or freeze mode?

## What exists today

### 1. Staged ServerConfig records

`StagedConfigChange` is a SQLite row (`internal/db/migrations.go` auto-migrates `&StagedConfigChange{}`). The model is `internal/db/models.go` `StagedConfigChange`:

- `Payload` is a JSON object of `ServerConfig` JSON names to new values. The struct comment says it is the diff against the live config at stage time, one-shot.
- `PreviousPayload` is filled at apply time with the previous values of those keys (`Store.ApplyStagedConfigChange` in `internal/db/staged_config.go`). `RolledBackAt` records a later rollback. This is the SCH-1 snapshot described in `docs/issues/016-SCH-1-staged-config-rollback.md`.
- `ApplyMode` is only `on_restart` (`StagedApplyOnRestart`) or `scheduled` (`StagedApplyScheduled`).
- `CronExpr` is required for `scheduled`. `ValidateCronExpr` accepts a 5-field cron expression (`internal/db/staged_config.go`).
- `Status` constants are `staged`, `applied`, `discarded`, and `rolled_back`. Apply and rollback also write the literals `"applying"` and `"rolling_back"` as in-transaction claims. Those two strings are not `StagedStatus` constants. The claim and the config merge share one GORM transaction, so a failed apply returns the row to `staged` (`TestStagedConfigChange` corrupt-payload case in `internal/db/staged_config_test.go`).

`Store.StageConfigChange` inserts the diff and returns. Its comment says it works regardless of operational state and applies nothing. It rejects an empty diff, an unknown mode, a bad cron expression, a missing server, unknown keys, and the identity keys `id` and `serverId` (`validateStagedConfigFields`). Any other key present on a JSON rendering of `ServerConfig` is allowed. That includes MOTD, difficulty, JVM flags (`jvmOpts`, `jvmXxOpts`, `jvmDdOpts`), memory env fields, `serverPort`, CurseForge and Modrinth fields, `pauseWhenEmptySeconds`, whitelist, and RCON. It does not include `Server` columns (Docker image, host port, mod loader, memory allocation, Docker overrides).

The only production caller of `StageConfigChange` is `StagedConfigHandler.handleStage` (`internal/rpc/handlers/staged_config.go`). Routes, mounted in `internal/rpc/server.go` at `/api/v1/staged-config/`:

| Method | Path | Store call |
| --- | --- | --- |
| GET | `/{serverID}/staged` | `ListStagedConfigChanges` |
| POST | `/{serverID}/staged` | `StageConfigChange` (default mode `on_restart`) |
| POST | `/{serverID}/staged/{id}/apply` | `ApplyStagedConfigChange` |
| POST | `/{serverID}/staged/{id}/rollback` | `RollbackStagedConfigChange` |
| DELETE | `/{serverID}/staged/{id}` | `DiscardStagedConfigChange` (only from `staged`) |

There is no staged-config message in `proto/`. A search of `proto/**/*.proto` for `staged` returns no matches. Auth on the HTTP handler, when any auth is enabled, checks `rbac.ResourceServerConfig`: read for GET, create for every other method. The model comment lists RBAC-gated approvals and audit linkage as remaining work. The handler does not write an activity-log event.

`ApplyStagedConfigChange` merges the payload onto the live `ServerConfig` and sets `applied`. The comment says container recreation stays the caller's job. Rollback restores `PreviousPayload` onto the live row and sets `rolled_back`. Rollback's HTTP handler does not call Docker. The model comment says per-field conflict detection (live config moved since staging) is remaining work: apply overwrites the keys in the payload with no comparison against the values observed at stage time. `PreviousPayload` is the live values at apply time, not a copy taken at stage time.

`ListStagedConfigChanges` orders `created_at DESC`. `applyStagedByMode` (`internal/scheduler/staged_config.go`) walks that list and applies each matching mode in turn, stopping on the first error. When two `on_restart` rows touch the same key, the older row is merged last.

`StagedConfigView` (`web/carbon-panel/src/lib/components/staged-config-view.svelte`) posts those HTTP routes. Its own comment says it is not wired by default. A search of `web/**/*.{svelte,ts}` for `StagedConfigView` and `staged-config-view` hits only that file. The settings editor (`server-configuration.svelte` `handleSave`) calls `ConfigService.UpdateServerConfig` instead.

### What triggers an apply

`on_restart` rows are applied by `ApplyOnRestartStagedConfigRecords`, which calls `applyStagedByMode` for that mode only. Call sites:

- `ServerService.StartServer` and `RestartServer` (`internal/rpc/services/server.go`). If any row was applied, or `HasPendingRestartStagedConfigRecreate` is true, and a container id exists, they call `RecreateContainer`. `HasPendingRestartStagedConfigRecreate` is true when an `on_restart` row is `applied` with `AppliedAt` after `Server.LastStarted` (or `LastStarted` is nil). It ignores `scheduled` rows.
- Scheduler `executeStartTask`, `executeRestartTask`, and `StartServerForBoot` call `recreateForPendingStagedConfig` (`internal/scheduler/scheduler.go`). That helper applies `on_restart` rows, and if any were applied or a pending recreate remains, recreates. `startReplacement` and `markStarted` are true for those three callers, so a stopped replacement is started and `LastStarted` advances.
- `DeepSleepManager.WakeServer` calls the installed `WakePreparer` before `StartContainer`. `cmd/carbon-panel/main.go` sets that to `Scheduler.PrepareDeepSleepWake`, which calls `recreateForPendingStagedConfig` with both flags false: replace the stopped container, do not start it, do not advance `LastStarted`. `WakeServer` then starts it and sets `LastStarted`.

`ServerService.RecreateServer` recreates from the current `ServerConfig` and does not call `ApplyOnRestartStagedConfigRecords`. A recreate therefore leaves `on_restart` rows staged. The next start or restart applies them and can recreate again.

`scheduled` rows are applied by `Scheduler.applyDueStagedConfigs`, invoked at the start of every `checkAndRunDueTasks` tick (`internal/scheduler/scheduler.go`). `ListDueScheduledStagedChanges` selects `staged` + `scheduled` rows whose first cron fire after `CreatedAt` is not after now. The tick calls `ApplyStagedConfigChange` and releases a process-local transition. It does not recreate, start, or restart. The struct comment says a scheduled change applies once, then stays `applied`. Recurring schedules are listed as remaining work.

`ConfigService.UpdateServerConfig` (`internal/rpc/services/config.go`) is a different path. It writes the live `ServerConfig` immediately. If `Server.ContainerID` is non-empty it calls `recreateContainer` in the same RPC. `CreateContainer` (`internal/docker/client.go`) builds the process environment with `buildEnvFromConfig`, which emits every `ServerConfig` field that has an `env` tag, and always sets `DUMP_SERVER_PROPERTIES=true`. A plain `RestartContainer` stops and starts the same container id. It does not read `ServerConfig`.

The settings UI disables edits and save while `server.status === RUNNING` and shows "Server must be stopped to modify configuration. Changes will take effect after restart." (`server-configuration.svelte`). The RPC itself has no running-status guard: any caller that hits `UpdateServerConfig` while a container id exists recreates. Paused and deep-sleep statuses are not `RUNNING`, so the form allows save. `statusAfterContainerRecreation` (`internal/rpc/services/server.go`) keeps `deepsleep` when the replacement was not running, and maps every other non-running previous status, including `paused`, to `stopped`.

Auto-rollback of the rows just applied runs when recreate or the following start fails, in `StartServer`, `RestartServer`, and `recreateForPendingStagedConfig`. A successful recreate is not rolled back by those paths. A later HTTP rollback restores the database row only.

`ClearEphemeralConfigFields` (`internal/db/store.go`) clears only `CFForceReinstallModloader`, and the start/restart/recreate-for-staged paths call it after a successful start. The `ServerConfig` tag marks that field `ephemeral:"true"`.

### 2. Restart, recreate, start, stop, freeze, deep sleep, wake

ADR 0006 (`docs/adr/0006-idle-modes-are-data.md`, accepted 2026-10-25) says freeze and deep sleep share `idleTracker`, `idleMode`, and `idleRunner` (`internal/docker/idle.go`). The modes differ in which column opts in, the timeout, and the container action: pause/unpause versus stop/start. It explicitly did not merge those container operations.

| Word in this code | What the function does |
| --- | --- |
| Start | `StartServer`: unpause in place when status is `paused` and no `on_restart` recreate is needed; otherwise start the existing container, or recreate first when staged `on_restart` work is pending. Panel boot skips servers already in `deepsleep` (`main.go`) and uses `StartServerForBoot`, which unpauses a Docker-paused container or starts a stopped one, after the staged recreate check. |
| Stop | `StopServer`: if status is `paused`, unpause, persist `stopping`, then `StopContainerWithin`. End status is `stopped`, not `deepsleep`. |
| Restart | `RestartServer`: if status is `paused`, unpause before the staged-recreate branch. With pending `on_restart` work, `RecreateContainer`. Otherwise `RestartContainer`, which is stop then start of the same id (`Client.RestartContainer`). |
| Recreate | `RecreateContainer` (`internal/docker/client.go`): preflight ports and image, stop a running, unhealthy, or paused container, rename the old container aside, `CreateContainer` from the current `Server` and `ServerConfig`, start the replacement only when the old one was running or unhealthy. A paused container is treated as not-running, so the replacement stays stopped. `RecreateServer` always starts a stopped replacement afterward. |
| Freeze | `HibernationManager.HibernateServer`: `PauseContainer` (Docker cgroup freeze), require Docker status `paused`, persist `StatusPaused`. `WakeServer` unpauses and persists `running`. It does not call `PrepareDeepSleepWake`. Sweep interval in `main.go` is 30s. Eligible when `Server.AutoHibernate` is set. Timeout is `IdleTimeoutMinutes`, default 10 (`HibernationManager.idleMode`). |
| Deep sleep | `DeepSleepManager.SleepServer`: persist `StatusDeepSleep` and refresh the proxy route before the stop, set Docker restart policy to `no`, RCON `save-all` then RCON `stop`, wait, then `StopContainer` if still running. `WakeServer` runs `WakePreparer`, restores `DefaultRestartPolicy` (`on-failure`, maximum 5, `internal/docker/restart_policy_test.go`), starts, waits until Docker reports running, persists `running` and `LastStarted`. Sweep interval in `main.go` is 60s. Eligible when `AutoDeepSleep` is set. Timeout is `DeepSleepTimeoutMinutes`, default 30. |

`docs/runbook-sleep-baseline.md` says status becomes `deepsleep` only after the container exits. `SleepServer` writes `StatusDeepSleep` before the stop so the reconciler will not treat the exit as an unexpected stop (comment in that function). The same runbook says auto-hibernate and auto-deep-sleep have no UI toggle. `web/carbon-panel/src/lib/components/server-settings.svelte` has checkboxes and timeout fields for both, and `UpdateServer` persists them without setting `needsRecreation`.

`docs/research/minecraft-sleep-deep-sleep.md` describes an earlier tree: wake on any handshake, `RecordActivity` with no callers, and `NewHibernationManager` unwired. On this branch `main.go` constructs both managers, points their player-count providers at the metrics collector, and registers proxy wake and activity handlers. `internal/proxy/minecraft.go` wakes and records activity only for login intent (`isLoginIntent`: handshake next-state 2 or 3). Status pings (next-state 1) get a cached MOTD and do not wake. `TCPProxy.SetRouteHibernated` and `UDPProxy.SetRouteHibernated` are no-ops (`internal/proxy/tcp.go`, `udp.go`).

Native tick pause is a third mechanism. `ServerConfig.PauseWhenEmptySeconds` is env `PAUSE_WHEN_EMPTY_SECONDS`, default 60 (`internal/db/models.go`). The runbook says new servers receive that value and that it is the JVM pausing itself, not a cgroup freeze. The September research note's `ENABLE_AUTOPAUSE` / `AUTOSTOP` passthrough is gone from Go sources: a search of `*.go` for `ENABLE_AUTOPAUSE` and `AUTOSTOP` returns no matches. The runbook says that family was removed.

ADR 0005 (`docs/adr/0005-lifecycle-transitions.md`) is the in-process lease `docker.TryBeginServerTransition`. It is not the database operation journal. Freeze, deep sleep, start/stop/restart RPCs, config update, and `applyDueStagedConfigs` take that lease. Claims are process-local.

### 3. Mods, datapacks, files, properties, JVM flags, image, port, env, modules

`StageConfigChange` is not called from mod, datapack, file, module, or `UpdateServer` code. A search of `*.go` for `StageConfigChange` hits the store, the HTTP handler, and tests.

| Mutation | What the code does | Restart? |
| --- | --- | --- |
| `ServerConfig` via `UpdateServerConfig` | Saves the row. Recreates when a container id exists. Env is applied only by `CreateContainer` / `buildEnvFromConfig`. | Recreate, and the replacement is started when the old container was running or unhealthy. |
| `ServerConfig` via stage + `on_restart` | DB diff until start, restart, boot, scheduler start/restart, or deep-sleep wake preparation, which then recreate. | One recreate on that path when a container id exists. |
| `ServerConfig` via stage + `scheduled`, or HTTP "apply now" | DB merge on the cron tick or on POST `.../apply`. | No Docker call. |
| JVM flags | `jvmOpts` / `jvmXxOpts` / `jvmDdOpts` are `ServerConfig` fields. `applyConfigUpdates` rejects values that fail `docker.ValidateJVMOptions`. They become container env at create time. | Same as other `ServerConfig` env: recreate, not an in-place restart. |
| Properties file | `server-configuration.svelte` labels the screen "Minecraft server environment variables". The image is told `DUMP_SERVER_PROPERTIES=true` at create. `FileService.UpdateFile` writes bytes through the node storage accessor and returns. No status check, no RCON, no recreate. | The file RPC does not restart. Whether the running JVM reloads `server.properties` is not decided in this code. |
| Docker image, host port, memory, mod loader, MC version, extra ports, Docker overrides (including `environment` on `DockerOverrides` in `proto/carbonpanel/v1/common.proto`) | `UpdateServer` sets `needsRecreation` and calls `RecreateContainer` in the same RPC. Proxy-enabled servers cannot change port. | Recreate now. Not stageable: these are `Server` fields, not `StagedConfigChange` keys. Idle flags and timeouts on `UpdateServer` do not set `needsRecreation`. |
| Mods and plugins | `ModService.ImportUploadedMod`, `UpdateMod`, and `DeleteMod` (`internal/rpc/services/mod.go`) move or delete files under `minecraft.GetModsPath`. `GetModLoaderInfo` (`internal/minecraft/modloader.go`): vanilla has an empty mods directory and the import rejects that; Forge, NeoForge, Fabric, and Quilt use `mods/` and `.jar`; Bukkit, Spigot, Paper, Purpur, Pufferfish, and Folia use `plugins/` and `.jar`. `InspectModSide` reads Fabric/Quilt `environment` and rejects `client`. Its comment says Forge and NeoForge have no reliable equivalent, so those return `SideUnknown` and are installed anyway. | No restart and no RCON in these RPCs. |
| Datapacks | `DatapackService.InstallDatapack`, `UploadDatapack`, and `DeleteDatapack` write or remove a zip under `GetDatapacksPath`, which is `{data}/{level}/datapacks` (default level `world`). The comment says datapacks are world data, not mods. | No restart and no reload command in `datapack.go`. |
| Modpack file sync | `executeModpackUpdateTask` rsyncs (or copies) the git tree into `server.DataPath` before it decides whether to restart. `StageConfigUpdates` writes `.carbon-panel_modpack_staged/staged_manifest.json` and then still syncs. A search for `staged_manifest` hits that writer, the snapshot exclude list, and `modpack_update_test.go` (which expects the synced file on disk and the manifest). Restart is `RestartImmediately`, or a maintenance cron if `isWithinMaintenanceWindow` (the previous fire of that cron within 30 minutes). Graceful restart sends `say` warnings and `save-all` at the 5-second mark, then `executeRestartTask`. | Optional restart after the files are already copied. |
| Modules | `ModuleService.CreateModule` starts a module container in the background. `UpdateModule` recreates the module container when env, volumes, memory, ports, command, uid, or gid change. | The module container, not the Minecraft server, and not via `StagedConfigChange`. |

### 4. Scheduler tasks

`TaskType` (`internal/db/models.go`) is `command`, `backup`, `restart`, `start`, `stop`, `script`, `webhook`, `modpack_update`, and `chunky_pregen`. `Scheduler.runTaskType` switches on those and returns an error for anything else. There is no "apply staged changes" task type.

`ScheduleType` is `cron`, `interval`, `once`, and `event`. That is separate from a staged row's cron. A staged `scheduled` row is not a `ScheduledTask`.

`executeTaskAtDepth` skips the executor when `RequireOnline` is set and `Server.Status != running`, and records `skipped` / `server offline`. `TaskService` create forces `RequireOnline` for `TASK_TYPE_COMMAND` only (`internal/rpc/services/task.go`). Other types keep the request's bool, which is false when unset. Paused and deep-sleep statuses are not `running`, so a require-online task is skipped for both. A restart or start task with `RequireOnline` false still runs `executeRestartTask` / `executeStartTask`. Those functions do not branch on `deepsleep` or `paused` (the only `StatusPaused` check in `internal/scheduler/scheduler.go` is inside `StartServerForBoot`). A restart task with pending `on_restart` config recreates. Without it, the task stop-then-starts the same container. `executeStopTask` persists `stopped`, including over a server that was `deepsleep`.

`applyDueStagedConfigs` does not read server status. It takes `TryBeginServerTransition` per row and skips the row until the next tick if the lease is held. It does not start or unpause the container.

A restart task is the closest existing "apply then restart": `executeRestartTask` calls `recreateForPendingStagedConfig`, which applies every pending `on_restart` row and recreates once. `scheduled` rows are not part of that call. If none are pending, the task does not recreate, so a `ServerConfig` that was merged earlier by the cron tick stays out of the container environment.

While the server is deep sleeping, the cron tick can still merge `scheduled` rows into the database. The container stays stopped. The next cold wake recreates only when an `on_restart` row is pending (`PrepareDeepSleepWake`). A `scheduled` row that is already `applied` does not make that helper recreate. Freeze wake never calls `PrepareDeepSleepWake`; `HibernationManager.WakeServer` only unpauses.

### 5. How the panel decides a server is idle

There is no `last_join`, `idle_since`, or similar column. A search of `*.go` and `*.proto` for `last_join`, `LastJoin`, `idle_since`, `IdleSince`, and `last_player` hits only `idleTracker.markIdleSince`, whose comment says tests use it to backdate the clock.

`Server.PlayersOnline` is `gorm:"-"`. The idle sweep does not read a stored count. `idleTracker.OnlinePlayers` uses the provider installed in `main.go`: `metrics.Collector.GetMetrics`, or 0 when metrics are missing. The collector's RCON loop (`collectRCONData`) skips containers whose Docker status is not `running`. When a recent SLP sample exists it keeps that player count; otherwise it sends RCON `list`. SLP ping (`collectSLPData`) also writes `PlayersOnline`. `collectLifecycleEvents` emits `PLAYER_JOIN` / `PLAYER_LEAVE` from the metrics roster and clears that baseline when Docker status is not running, unhealthy, or starting. Nothing in the idle managers subscribes to those events. `RecordActivity` callers are the proxy activity handler and the ends of hibernate, freeze-wake, deep-sleep, and deep-wake.

`idleTracker.sweep` forgets the clock unless status is `running`, a container id is set, and the mode's `Eligible` is true. A positive player count also forgets the clock. The first zero-player observation only starts the clock (`idleFor` returns false). A later observation sleeps when the duration reaches the mode timeout. `RecordActivity` deletes the clock. Proxy login intent therefore resets both managers; a status ping does not (`minecraft.go`).

`ServerConfig.PlayerIdleTimeout` is env `PLAYER_IDLE_TIMEOUT` (Minecraft's own kick-idle-players setting, default 0). It is not read by `idleTracker`.

So "idle for X minutes" exists only as this in-memory clock, and only for a running server that opted into freeze or deep sleep. It is not consulted by the scheduler. It does not advance while the server is paused or deeply asleep, because those statuses fail the `StatusRunning` check and the clock is deleted. A missing metrics snapshot counts as zero players, which starts the clock.

### 6. Lifecycle journal and the operation lock

`operation.RunJournaled` (`internal/operation/journal.go`) persists a `ServerOperation` before the mutation. `BeginServerOperation` (`internal/db/server_operation.go`) stores `request_id` plus kind as `RequestKey` (`StorageKey`). A completed key does not run the mutation again. A failed key requires a new id. An empty request id is unique per call. `ActiveServer` is a unique index, so one server has one active journal row. The function comment says that lock is the coordinator: two lifecycle mutations of one world must not interleave, and a failure to store the intent means the mutation must not run.

`ServerService.runJournaledMutation` wraps that. Kinds used from `server.go` include `create`, `start`, `start_unpause`, `stop`, `restart`, `restart_create`, `restart_start`, `delete`, and `migrate`. Backup restore uses kind `restore`. Snapshot rollback uses `snapshot-rollback`.

The staged recreate inside `StartServer` calls `RecreateContainer` and, when the replacement was not running, `StartContainer` without `runJournaledMutation`. The following plain start is journaled only when `stagedContainerStarted` is false. `RestartServer` journals `restart_start` for a staged replacement that was not already running, and journals `restart` for the non-staged `RestartContainer` path. `recreateForPendingStagedConfig`, `applyDueStagedConfigs`, `UpdateServerConfig`'s recreate, and `RecreateServer` do not call `RunJournaled`. They do take the ADR 0005 process-local transition when they run under `beginServerLifecycleTransition` or their own `TryBeginServerTransition` (the scheduled-apply tick and the scheduler start/restart/boot helpers). `ApplyStagedConfigChange` itself is a database transaction with no journal row and no `request_id`.

`knowledge/operations/lifecycle-journal.md` states the same rules: persist intent before Docker, do not release the lock when the container cannot be read, and do not let `-operations resolve` stop or remove containers. It does not mention staged config.

### 8. Minecraft-specific behavior the code and the sleep docs already state

- Mods and plugins are files dropped into a loader-specific directory while the panel does not restart the JVM or send a reload (`ModService`). Datapacks are files dropped into the world directory with the same absence of reload (`DatapackService`). Doing that while the world is running is whatever the write does; this repo does not add a flush or a restart around it.
- Loader split that the code states: Forge, NeoForge, Fabric, and Quilt jars under `mods/`; Paper, Spigot, Bukkit, and the Paper forks under `plugins/`; vanilla has no mods directory. Fabric and Quilt client-only jars are rejected. Forge and NeoForge side metadata is treated as unknown (`InspectModSide`).
- World save before a panel stop is not one shared helper. `applyStopLadder` (`internal/docker/stop.go`) runs `rcon-cli stop` (or the injected game-stop hook), then Docker stop, waiting `StopDuration` seconds (default 60, cap 180) before SIGKILL. It does not send `save-all`. Deep sleep does send RCON `save-all` and then RCON `stop` before the Docker backstop (`SleepServer`). The modpack graceful restart sends `save-all` once, at five seconds remaining. Snapshot quiesce (`internal/snapshot/quiesce.go`) sends `save-off`, `save-all flush`, and later `save-on`. Migrate sends `save-all flush` (`migrate_transfer.go`).
- Freeze is a paused container: RAM stays resident. The September research note and `docs/runbook-sleep-baseline.md` say a freeze makes one tick span the pause, and that Paper/Purpur need `max-tick-time=-1` and `JVM_DD_OPTS=disable.watchdog:true` or the watchdog can kill the server on resume. Those flags are operator `ServerConfig` values, not something `HibernateServer` sets. The runbook also says not to freeze while players are online. The sweep enforces that by player count. `HibernateServer` itself does not send `save-all` before `PauseContainer`.
- Deep sleep is a stopped container. The runbook calls that zero RAM and CPU. `SleepServer` refuses a status other than `running`. The sweep will not select a server with a positive player count. Players already online therefore block the automatic transition. A direct TCP or UDP route does not carry hibernation or the activity callback, so a join that never hits `MinecraftProxy` resets the idle clock only when a later RCON or SLP sample reports players.
- `PAUSE_WHEN_EMPTY_SECONDS` is inside the JVM and is independent of both panel idle modes. The runbook says it pauses ticking about 60 seconds after the last player leaves, keeps RAM, and does not involve the watchdog.

## What is explicitly absent

Searches below were run on this branch. "No matches" means the search returned none.

- No staged record for mods, datapacks, plugins, loose files, modpack file trees, Docker image, host port, `Server.Memory`, mod loader, MC version, Docker overrides, or modules. `StageConfigChange` callers are the HTTP handler and tests. `ModService`, `DatapackService`, `FileService`, `ModuleService`, and `UpdateServer` do not call it.
- No proto RPC for staging. `proto/**/*.proto` has no `staged`.
- `StagedConfigView` is not imported by another Svelte file. The settings screen saves through `UpdateServerConfig`.
- No apply mode other than `on_restart` and `scheduled`. `StageConfigChange` rejects any other mode string (test `"sometimes"`).
- No task type that means "apply staged changes then restart". `runTaskType` has no such case. A restart task only recreates when `on_restart` rows are pending.
- No idle-duration predicate on `ScheduledTask` or on `StagedConfigChange`. No persisted last-join or idle-since column (`Server` struct in `internal/db/models.go`; the `last_join` / `idle_since` search above).
- `scheduled` apply does not recreate. `applyDueStagedConfigs` has no `RecreateContainer` call. `HasPendingRestartStagedConfigRecreate` requires `ApplyMode == on_restart`.
- HTTP apply and HTTP rollback do not recreate. `handleApply` and `handleRollback` call the store only.
- Freeze wake does not apply staged config. `HibernationManager.WakeServer` has no `WakePreparer`.
- `RecreateServer` does not apply `on_restart` rows.
- The modpack manifest is not an apply queue. Nothing reads `staged_manifest.json` except the test that checks it was written. The file sync always follows the write.
- `knowledge/` does not describe staging. A search of `knowledge/**/*.md` for `staged`, `StageConfig`, and `on_restart` returns no matches. `knowledge/architecture/scheduler.md` lists cron and start, stop, restart, and backup. `knowledge/operations/deep-sleep.md` points at `freezer.go`, `sleep.go`, `idle.go`, and the sleep runbooks. `knowledge/operations/lifecycle-journal.md` describes the operation lock and does not mention staged config. `knowledge/glossary/terms.md` does not define a staged change or an idle mode.
- The September research note's "hibernation unwired" and "autopause env still passed through" claims are not true of this branch (`main.go` wiring; no `ENABLE_AUTOPAUSE` in `*.go`). The runbook's "no UI toggle" and "deepsleep status only after exit" sentences do not match `server-settings.svelte` and `SleepServer`.

## Constraints the code already enforces

- A stage insert does not start, stop, unpause, or recreate. `StageConfigChange` only writes a row after checking the server exists.
- Only `ServerConfig` JSON fields can be staged, and not `id` or `serverId`. The diff must differ from the live row.
- `on_restart` rows in `staged` stay staged until start, restart, scheduler start/restart, panel boot, or deep-sleep wake preparation. Those paths recreate when a container id exists, and they roll the newly applied rows back if that recreate or the following start fails.
- `scheduled` rows become live `ServerConfig` at the first cron fire and are not re-applied. They do not by themselves replace the container.
- Container env and `DUMP_SERVER_PROPERTIES` are fixed at `CreateContainer`. `RestartContainer` reuses the container.
- Automatic freeze and deep sleep run only for `StatusRunning`, opted-in servers, with a zero player count from the metrics provider, after a full timeout measured from the second consecutive idle observation.
- Login intent on the Minecraft proxy resets both idle clocks and is the only handshake state that wakes. Status pings do not.
- Deep sleep sets restart policy to `no` before stopping and restores `on-failure:5` only inside `WakeServer`. Other start paths do not call `SetRestartPolicy`.
- One process-local transition per server (`TryBeginServerTransition`). One active database operation row per server (`ActiveServer`). Staged database apply takes the first and not the second. Several container replacements that exist to publish staged config also skip the database journal (listed in section 6).
- The journal's own rule is that a Docker lifecycle mutation persists intent first and holds the per-server lock until the outcome is known (`RunJournaled`).

## Implications for a later plan

These are consequences of the paths above, not a design.

- An admin can record a `ServerConfig` diff during deep sleep or freeze without the stage call waking the server, because that call does not touch Docker. Publishing that diff into the running or stopped container is a different call, and today only the `on_restart` apply sites recreate.
- A cron fire of a `scheduled` row changes the database while the existing container keeps the environment it was created with. A later stop/start restart does not reload that environment. Deep-sleep wake recreates only for pending `on_restart` rows. Freeze wake does not recreate at all.
- Mods, datapacks, plugins, and file edits are already on disk when their RPCs return. There is no holding area that a single later restart could apply as a batch. The modpack task copies first and may restart after, including while a require-online-false task runs against a non-running server.
- `on_restart` rows that are still `staged` are applied together, in `created_at DESC` order, inside one recreate on the start/restart/wake-preparer paths. `scheduled` rows, file writes, and `UpdateServer` recreates are not in that batch. `RecreateServer` can restart once and leave the staged rows for a second recreate.
- There is no stored "idle for X minutes" that remains valid while the server is frozen or deeply asleep. The only duration is the in-memory sweep clock, and the sweep deletes it when status is not `running`. Player-count samples are also not refreshed while Docker status is not `running`.
- A container replacement that publishes config is a lifecycle mutation of the same world the journal comment says must take the active lock. The current staged recreate paths do not all take it. The process-local transition is the lock they do take, and ADR 0005 says that lease does not coordinate multiple controllers.
- Rollback after a successful apply restores `ServerConfig` in the database and leaves the container that was created from the applied values in place, until some other recreate runs.
- `PrepareDeepSleepWake` is the existing hook that runs before a cold start and can replace the container without the stage call itself having woken the server. `HibernationManager.WakeServer` has no equivalent hook. `SleepServer` already issues `save-all` before a deep-sleep stop. `HibernateServer` and the generic stop ladder do not.

## Implementation note

2026-10-28, after the notes above. On `feat/staged-config-batch`, `UpdateServerConfig` with `stage=true` records the diff on one open `ServerUpdateBatch` and does not recreate the container. `at_time` and `idle` rows stay staged across start and restart. `Scheduler.applyDueStagedConfigs` no longer merges a due cron row into the live config. Mods, datapacks, files, and the single restart that publishes a due window are still open (OSS-284 through OSS-287).
