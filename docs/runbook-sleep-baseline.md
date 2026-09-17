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
