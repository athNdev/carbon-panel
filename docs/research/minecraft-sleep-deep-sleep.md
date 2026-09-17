# Sleep vs Deep Sleep for Minecraft servers — research + proposal

Date: 2026-09-17. Context: DiscoPanel `autopause` wake-loop log + carbon-panel `MINE-18` cgroup-freezer hibernation (`internal/docker/freezer.go`, `internal/proxy/minecraft.go`).

## TL;DR proposal

- **Sleep = stay in RAM, burn ~0 CPU, wake in ms.** Implement as: vanilla `pause-when-empty-seconds` (cooperative tick-freeze, free) + optional cgroup-freeze **only** as a CPU saver, woken **only on real login intent** (handshake `next-state=2` / Login Start), never on status pings. Do not sell freezer as RAM saving — it saves no RAM.
- **Deep sleep = zero RAM, wake in seconds from disk.** Implement as: full graceful `docker stop` + always-on tiny Go proxy on :25565 that answers pings from cache, starts the container on login intent (single-flight), and holds/kicks the client with a "starting…" message until the backend health-gates green. This is the only design that hits "ideally not use any RAM".
- Drop the `knockd`-on-any-SYN wake model (the root cause of the log loop). Discriminate at L7 in the proxy you already own.
- Skip CRIU checkpoint/restore for HotSpot Minecraft (experimental, no RAM/latency win in practice). Treat JVM heap-shrink flags as hygiene, not a sleep mechanism.

## 1. What the DiscoPanel log is actually showing

Repeating 2-minute cycle, verbatim pattern from the report:

```
[Autopause loop] Server was knocked - waiting for clients or timeout
[Autopause loop] No client connected since startup / knocked - pausing
[Server thread/INFO]: System chat: [Rcon: Saved the game]
[Autopause] Pausing Java process
[Server thread/WARN]: Can't keep up! Is the server overloaded? Running 12275ms or 245 ticks behind
```

Plus the screenshot: `MEMORY 1.92 / 2.0 GB, 96.0% used` while "paused", `CPU 0.2%`.

Diagnosis, confirmed against itzg upstream docs/discussions:

1. **Wake happens on any TCP knock, including server-list pings and internet scanners.** itzg autopause wakes via `knockd` at the network-interface level (`AUTOPAUSE_KNOCK_INTERFACE`, default `eth0`); the knocked timeout (`AUTOPAUSE_TIMEOUT_KN`, default 120s) exists precisely because "knocking of the port (e.g. by the main menu ping)" is a known false-wake source. A public 25565 receives constant background scanning, so pause → knock → pause loops every ~2 min forever. A parallel upstream report (#3873, GTNH + `itzg/minecraft-server:java25`) shows the identical loop shape and concludes autopause "wakes the server on any inbound TCP activity, including unauthenticated status/handshake probes, even when no player actually joins", making it "ineffective for publicly exposed servers".
2. **`Can't keep up! N ticks behind` on every resume is inherent to STOP/freeze.** itzg docs state pausing "causes a single tick to take as long as the process is stopped", so the watchdog must be disabled (`MAX_TICK_TIME=-1`, and `-Ddisable.watchdog=true` / `JVM_DD_OPTS=disable.watchdog:true` on Paper). The shrinking-then-growing behind-values in the log (12275ms → 9641ms → 14379ms sawtooth) are the wall-clock debt accumulated while frozen being observed on unfreeze.
3. **RAM never drops because freezer/SIGSTOP cannot free RAM.** `docker pause` (cgroup freezer v1 `freezer.subsystem` / v2 `cgroup.freeze`) only stops scheduling threads so quiesced tasks can be checkpointed; pages stay resident. Swap-out happens only under host memory pressure, slowly. So `1.92/2.0 GB` while paused is expected, not a bug.
4. **The pre-pause `Rcon: Saved the game` line is the autopause flush, not the waker.** In #3873 the maintainer clarifies "Rcon is used immediately before pausing to flush the world data to disk" — the reporter saw `Rcon connection from: /::1` + `Saved the world` immediately before each pause. If panel-side RCON polling (`list`, `save-all`) runs against a frozen JVM it can additionally look like "activity" to naive idle detectors.
5. **`pause-when-empty` deprecation context.** itzg docs now state: "As of 1.21.2 it is not recommended to use this feature since Minecraft server natively auto-pauses when the server is empty… configured via `PAUSE_WHEN_EMPTY_SECONDS`". Carbon-panel still passes the old `ENABLE_AUTOPAUSE/AUTOSTOP` env set through (`internal/db/models.go:261-274`, `internal/rpc/services/config.go:609-614`) — running both layers invites stop→start→freeze flapping ("auto-stop servers keep restarting" symptom: a stopped container that still has a proxy route + a watchdog that sees "no ping response" as "should start", or overlapping autostop/autopause timers fighting).

## 2. Why the same symptoms transfer to carbon-panel's current code

From in-repo inspection (no changes made):

- **Wake on any handshake.** `internal/proxy/minecraft.go:235-251` fires the wake callback for any valid handshake with a matching `ServerAddress` **before any `NextState` check**, and the hibernation test itself wakes on `NextState: 1` (status ping). So server-list refreshes and scanners unfreeze the container — same flaw as `knockd`, one layer up the stack.
- **Idle signal is not yet trustworthy.** `HibernationManager.CheckIdleServers` (`freezer.go:99-157`) gates on `srv.PlayersOnline == 0`, but `PlayersOnline` is `gorm:"-"` (never persisted) and the freezer's `ListServers` path returns raw DB rows; real counts live in `internal/metrics/collector.go` (RCON `list` + SLP ping) and are overlaid only in the RPC layer. `RecordActivity()` has zero callers and `NewHibernationManager` has no production caller outside tests — the loop is scaffolded but unwired, which is actually good news: fix the policy *before* wiring it.
- **Only `MinecraftProxy` honors hibernation.** `TCPProxy`/`UDPProxy.SetRouteHibernated` are no-ops; direct-port connections neither track nor trigger wake. Any deep-sleep design must route everything through the hostname-aware proxy or track TCP connection counts per backend.

## 3. Surveyed approaches (with RAM / wake / stability)

### 3a. itzg autopause/autostop (STOP/CONT + knockd) — the status quo to move off

- RAM while paused: **~100% retained**. Wake: ms. Requires `max-tick-time=-1` + Paper watchdog disable + `AUTOPAUSE_KNOCK_INTERFACE` correct + `CAP_NET_RAW` when rootless.
- Verdict: CPU saver only; deprecated upstream for ≥1.21.2 in favor of native pause-when-empty. Keep the env passthrough for legacy images, but do not build new sleep on it.

### 3b. Vanilla `pause-when-empty-seconds` (1.21.2+, default 60) — the free Sleep baseline

- Mojang-native: after N s with zero players the server stops ticking (entities/redstone/daylight) but stays up, keeps answering pings/joins; first join resumes in a tick or two. No watchdog issue (cooperative, not a freeze). `server.properties` default file confirms `pause-when-empty-seconds=60`.
- RAM: unchanged. CPU: ~idle. Wake: ~instant. Verdict: **enable everywhere** (`PAUSE_WHEN_EMPTY_SECONDS`); it composes with everything below and is the only "sleep" that needs no proxy work.

### 3c. `itzg/mc-router` autoscale (Go, closest prior art to carbon-panel's proxy)

- Raw TCP proxy routing by handshake hostname; `IN_DOCKER` discovery via the Docker event stream (`mc-router.host` label); `-auto-scale-up` starts/unpauses stopped containers on access, `-auto-scale-down` gracefully stops after `-auto-scale-down-after` (default 10m); `-auto-scale-asleep-motd` / `-auto-scale-loading-motd` answer pings while down/booting; `-auto-scale-wait-timeout` default 60s **with the explicit caveat "Since the Minecraft Java client has a strict connection timeout of 30 seconds, configuring this value above 30s is not recommended for player join connections"**.
- Webhook scaler mode (`-auto-scale-webhook-url`, `{"action":"up/down"}`, optional `{"backend":...}` override) lets a **separate control plane own Docker** while the proxy stays least-privilege (no socket/mount) — the exact shape carbon-panel wants (proxy → Go backend → Docker). `-webhook-require-user` = scale only on real logins, not pings; `-webhook-wake-timeout` default 60s; `-connection-rate-limit` default 1/s; allow/deny lists per server.
- RAM scaled-to-zero: **0**. Wake: full cold boot. Verdict: **copy this contract** (asleep/loading MOTD, wait-timeout ≤30s for joins, webhook split, rate-limit, require-user).

### 3d. `timvisee/lazymc` (Rust, second-closest prior art) — the hold-pattern reference

- Proxy binds the public port, "handles all incoming status connections until the server is started and then transparently relays/proxies the rest. All without them noticing"; ~3 KB idle; client-occupation modes **Hold** (hold clients while the server starts, relay when ready), Kick (starting message), Forward, experimental Lobby; graceful sleep via RCON/`SIGTERM`; `PROXY`-header real-IP support.
- Verdict: **read its protocol handling before writing Go equivalents**; Hold ≤30s covers vanilla/Paper boots with zero extra RAM.

### 3e. Velocity + Limbo hold (for slow/modded boots past the 30s client timeout)

- `LimboAutoServer` (Velocity plugin on `LimboAPI`): on login, ping backend; if offline, park the fully-logged-in player in a proxy-side virtual world ("waiting room"), run the backend `start` command, release all waiters together when responsive; `autoShutdownDelay` stops the backend after last leave. Fixes `AutoServer`'s flaw of denying the first connection (client stuck in login timeout on slow Fabric starts).
- Independent field report (spelk.de "Minecraft Server on Demand", 2025-09): always-on Limbo lobby + Velocity `try=[limbo, main]` + 1s transfer check + Python `mcstatus`/`mcrcon` power loop with `save-all` → `stop` → power-off and grace periods — seamless to players, hardware fully off when idle.
- Cost: one shared always-on proxy JVM (~200–400 MB), not per-server. Per-backend idle RAM: **0**. Verdict: **optional Tier-B hold** for modpacks; not needed for vanilla/Paper ≤30s.

### 3f. Distinguishing ping from login (the core proxy policy fix)

- Java handshake carries `protocol version, server address, port, next-state`: **1 = status, 2 = login**. List refresh = handshake(1) + Status Request (+Ping/Pong), then disconnect. Join = handshake(2) → Login Start (username/UUID → encryption/profile).
- Policy every reference implementation converges on: **next-state 1 → answer from cache/config, never touch Docker; next-state 2 / Login Start → start/unpause, hold, forward when healthy.** mc-router (`-webhook-require-user`), lazymc (status handled while down), LimboAutoServer (act on login path) all do this. Carbon-panel's proxy already parses the handshake — the work is purely this branch + a ping cache.

### 3g. JVM heap shrink (hygiene, not sleep)

- Collectors can uncommit free heap after GC (G1 periodic GC/JEP 346 Java 12+, ZGC/JEP 351, Shenandoah; Serial needs full GCs; Parallel reluctant). Knobs: `-XX:MaxHeapFreeRatio/MinHeapFreeRatio`, `-XX:-ShrinkHeapInSteps`, `G1PeriodicGCInterval`, or `jcmd <pid> GC.run`. Native `malloc_trim`/jemalloc threads are marginal (heap dominates).
- Hard limits: only heap uncommits (Metaspace/stacks/code-cache/direct buffers stay); **if `-Xms == -Xmx`, shrinking is disabled by construction** — set `-Xms` low (e.g. 512M) with `-Xmx` high. Realistic idle floor for Paper: still hundreds of MB. Verdict: combine with pause-when-empty for running servers; never call it sleep.

### 3h. CRIU checkpoint/restore ("wake from disk") — not viable for MC Java now

- CRIU dumps anon memory/fds/threads to image files; `docker checkpoint create / docker start --checkpoint` remains **experimental** (daemon `experimental:true`, host CRIU, `--security-opt seccomp:unconfined`, no TTY, established-TCP restore needs out-of-band `tcp-established` config; Podman is the recommended frontend). The freezer's documented purpose is exactly to quiesce tasks *so checkpoint code can walk /proc* — freeze and checkpoint are companions, not alternatives.
- HotSpot reality: fails on eventpoll/timers/sockets/JNI-mapped natives; images ≈ heap size (GBs) so restore page-faults everything back in — barely faster than boot. The viable branch is **CRaC** (checkpoint at a defined point, e.g. post-boot pre-accept; cooperative socket close/reopen) on CRaC-enabled JDKs — no mainstream Paper/Purpur support. Player TCP cannot usefully survive across stop/start anyway (new netns/ports); the proxy must re-accept regardless. Verdict: **skip**; full-stop + proxy-hold dominates on simplicity.

### 3i. Full-stop + on-demand start behind a holding proxy (the Deep Sleep)

- Idle RAM: **0** (no process; only world files + image layers on disk). CPU 0. Cold start reality (vanilla/Paper, SSD, warm image): **10–30s** JVM + world + plugins; heavy modpacks 1–3+ min; first-join chunk I/O adds seconds.
- Two hold tiers (from §3c–3e): **Tier A** (Go proxy, zero extra RAM): asleep/loading MOTD + hold-or-kick-with-retry for ≤30s boots. **Tier B** (Velocity+Limbo, ~300 MB shared): completes login at proxy, parks players, transfers on ready — only tier that hides slow boots.
- Stability non-negotiables: graceful stop (`save-all` → `stop` via RCON or `docker stop` with adequate `stop_grace_period`; verify `session.lock` release) or trade RAM for corruption; never auto-stop with any connection (including RCON/query) active; single-flight start per server (concurrent joiners share one boot); forward only on SLP/health success, not TCP-open; serialize start/stop; cap wait-timeout at 30s for Tier A.

## 4. Recommended architecture for carbon-panel

```
                    ┌─ ping (next-state 1) → cached MOTD, never wakes ─┐
Internet → :25565 → Go proxy ─┤                                         ├→ per-server backend
 (single port)      (handshake parse,   └─ join (next-state 2/Login) → control plane ─┘   (docker run/
                     rate-limit, ping                               single-flight docker start,    pause/full-stop,
                     cache, hold/kick)                              health-gate, then relay)       RCON save+stop)
```

- **Sleep (default on, per server):** `pauseWhenEmptySeconds=60` on every server + `-Xms` low hygiene. Optionally cgroup-freeze after N min idle for CPU, woken only by §3f login intent. Expected: CPU ~0, RAM full, wake ms, no `Can't keep up` if native pause is the primary (freeze still needs `max-tick-time=-1` + Paper watchdog off while frozen).
- **Deep sleep (opt-in per server):** `docker stop` (graceful) after M min with zero *join-intent* connections; proxy serves asleep MOTD ("Server is asleep — join to wake"); on login intent: `docker start` (single-flight), loading MOTD, hold ≤30s (Tier A). Optional Tier-B Velocity+Limbo sidecar for modded fleets.
- **Idle source of truth:** proxy connection/join counts + RCON `list`, not container pingability. A frozen/stopped server being unreachable must mean "asleep", never "dead → restart" (kills the auto-stop restart loop). Remove or gate the legacy itzg `AUTOPAUSE/AUTOSTOP` env passthrough so only one layer owns lifecycle.
- **Stop/start serialization:** one in-flight transition per server; joiners during boot share it; stop requires zero players *and* zero in-flight logins *and* expired grace; health-gate = successful SLP status (or RCON `list`) before relay.
- **What not to build:** RAM-saving freezer, HotSpot CRIU, heap-shrink-as-sleep, knockd/SYN wake, wake-on-ping.

## 5. Suggested build order (small, stable increments)

1. Proxy policy (§3f): ping-cache + wake-only-on-login-intent + rate-limit/debounce. Fixes the log loop even before any lifecycle change.
2. Baseline Sleep: default `pauseWhenEmptySeconds`, `-Xms` guidance, `max-tick-time`/watchdog runbook for any remaining freeze path.
3. Deep sleep Tier A: graceful stop + single-flight start + asleep/loading MOTD + 30s hold/kick + health-gate. One server type first (vanilla/Paper).
4. Idle-truth + anti-flap: proxy/RCON-sourced idleness, stopped ≠ dead, legacy env passthrough gated off.
5. Tier-B limbo sidecar only if modded boots exceed 30s in practice. Revisit CRaC only when Paper-family ships support.

## Sources (primary first)

- itzg autopause doc (timeouts KN/EST/INIT, knock interface, watchdog `MAX_TICK_TIME=-1`, Paper `-Ddisable.watchdog=true`, 1.21.2 native-pause note): https://github.com/itzg/docker-minecraft-server/blob/master/docs/misc/autopause-autostop/autopause.md
- itzg discussion #3873 (pause/knock loop, RCON flush-before-pause, any-inbound-TCP wakes, public-server ineffectiveness): https://github.com/itzg/docker-minecraft-server/discussions/3873
- itzg/mc-router (autoscale up/down, asleep/loading MOTD, wait-timeout + 30s client-timeout caveat, webhook scaler `{"action":"up/down"}`, `-webhook-require-user`, rate-limit, Docker event discovery): https://github.com/itzg/mc-router
- timvisee/lazymc (status-handling while down, Hold/Kick/Forward/Lobby occupation, RCON/SIGTERM graceful sleep): https://github.com/timvisee/lazymc
- LimboAutoServer (login-path lazy start, limbo waiting room, shared startup, `autoShutdownDelay`): https://modrinth.com/plugin/limboautoserver
- Velocity on-demand field report (limbo → main transfer, `try=[limbo, main]`, mcstatus/RCON power loop, save-all → stop → power-off): https://blog.spelk.de/posts/2025-09-22-minecraft-mcboot/
- `server.properties` / `pause-when-empty-seconds=60` default: https://minecraft.wiki/w/Server.properties
- Kernel cgroup freezer purpose (quiesce tasks so checkpoint code can inspect them; SIGSTOP sequences "not always sufficient"): https://www.kernel.org/doc/Documentation/cgroup-v1/freezer-subsystem.txt
- CRIU checkpoint/restore design + Docker-experimental status (host CRIU, unconfined seccomp, no TTY, established-TCP caveats): https://criu.org/Checkpoint/Restore and https://forums.docker.com/t/using-criu-for-checkpointing-docker-containers/149448
