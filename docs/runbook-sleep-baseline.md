# Sleep baseline runbook — pause-when-empty + freeze watchdog (MINE-119)

## Sleep baseline (default on for new servers)

- New servers get `PAUSE_WHEN_EMPTY_SECONDS=60` (`internal/db/store.go`
  `CreateDefaultServerConfig`, UI default in `internal/db/models.go`).
  Vanilla 1.21.2+ then stops ticking ~60s after the last player leaves:
  ~0 CPU, full RAM, instant wake on join, no watchdog involvement
  (cooperative pause, not a freeze).
- Override per server (`pauseWhenEmptySeconds`) or fleet-wide via global
  settings; `0` disables native pause.
- Keep `-Xms` low (e.g. 512M) with `-Xmx` high so the JVM can uncommit idle
  heap; `Xms == Xmx` disables shrinking by construction.

## Freeze path watchdog (only if cgroup-freeze hibernation is used)

Freezing stops the JVM clock while wall-clock advances, so on resume the
tick loop observes one giant delta. Without these, the watchdog treats the
freeze as a crashed tick and force-kills the server (exit 1) — surfacing as
`Can't keep up! … ticks behind` followed by restarts:

- `MAX_TICK_TIME=-1` (`max-tick-time` in `server.properties`).
- Paper/Purpur: `-Ddisable.watchdog=true` (`JVM_DD_OPTS=disable.watchdog:true`
  on itzg images).
- Never freeze while players are online; never run freeze timers and native
  pause against each other — native pause is the primary, freeze is CPU garnish.

Background: `docs/research/minecraft-sleep-deep-sleep.md`.

## Deep sleep (MINE-121, opt-in per server)

Zero RAM/CPU idle: the container is fully stopped, the proxy retains the
route and serves a cached MOTD (`Server is asleep - join to wake it up!`,
`Server is waking up - join to connect!` while booting). Status pings never
boot; a real join boots the container (single-flight, concurrent joiners
share one boot), holds the client up to ~25s, and relays once the SLP health
gate passes. Past the budget the client is hung up and retries (pings show
the loading MOTD meanwhile).

- Opt in: `auto_deep_sleep=true`, `deep_sleep_timeout_minutes` (default 30).
  Like `auto_hibernate`, these are operator/DB-set (no UI toggle yet).
- Sleep is graceful: RCON `save-all` → RCON `stop` → wait for JVM exit
  (`session.lock` released) → docker-stop backstop. Status becomes
  `deepsleep` only after the container exits.
- Wake: container start → Docker running → status `running` → route refresh
  → proxy SLP gate → relay. The reconciler treats `deepsleep` as stable and
  never heals it; `stopped` still means "operator-stopped, no route".
- Slow/modded boots past ~25s need Tier-B limbo hold (MINE-123); Tier A
  covers vanilla/Paper. On hold timeout the client gets a Login Disconnect
  message ("still waking up, join again") instead of a bare hangup.

## Tier-B limbo: deferred, with trigger criteria (MINE-123)

A Velocity+Limbo sidecar (LimboAutoServer pattern) would hold logins past
the ~30s client timeout for slow boots, but costs an always-on JVM (~300MB
shared), forwarding-secret trust changes on every backend
(`online-mode=false` behind the proxy), and a new container type to operate.
Deferred: no measured slow-boot need in this fleet, and Tier A (25s hold +
loading MOTD + messaged retry) covers vanilla/Paper boots. Revisit when
p95 cold boots exceed ~20s for two consecutive releases or a modded fleet
lands — the proxy's `SleepWakeHandler` seam is where a limbo backend would
plug in (hold there instead of hang up).

## Anti-flap (MINE-122)

- **Restart policy is `on-failure:5`, not `unless-stopped`.** Docker
  resurrects every clean exit (code 0) under `unless-stopped` — operator
  stops, RCON stops, itzg autostop, deep-sleep stops all came back as
  stop→boot→stop loops. Now Docker restarts only real crashes (≤5 tries);
  intentional states belong to the panel: the reconciler self-heals
  *missing* containers and reports exited ones. Override per server via
  DockerOverrides if you truly need different semantics.
- **Legacy itzg timers are gated by native pause.** If
  `PAUSE_WHEN_EMPTY_SECONDS > 0`, the panel forces
  `ENABLE_AUTOPAUSE=false` + `ENABLE_AUTOSTOP=false` in the built container
  env even when explicitly enabled — one layer must own idleness. To use the
  legacy itzg layer instead, set pause-when-empty to `0` first.
- **Asleep is stable, not dead.** `paused` (freezer) and `deepsleep`
  (stopped) never trigger restart/heal: the reconciler treats `deepsleep`
  as terminal-by-design, and the metrics collector skips non-running
  containers instead of marking them failed.
