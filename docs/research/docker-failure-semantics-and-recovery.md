# Docker failure semantics and OSS recovery research

**Research date:** 2026-10-22. **Status:** planning input for the OSS robustness Epic; no fixes, tests, deployments or live-failure claims. External sources were accessed on this date. Repository observations refer to Carbon Panel HEAD `4cd35359616ffc58602349a61868f763e27f1fb2` plus the working tree inspected on that date. `internal/rpc/services/server.go` was already modified and was not edited.

## Evidence rules

- **Source fact:** a behavior stated by current official Docker/itzg documentation or first-party implementation.
- **Repository observation:** the inspected code has the described mechanism; this is not proof that a deployment exercised it correctly.
- **Proposed countermeasure:** a design recommendation to evaluate, implement and verify later.
- **Candidate failure:** needs a tight reproduction and regression test before being represented as a confirmed defect.

Do not turn this note into a list of missing features without checking the protections already present. Existing tests and comments identify important seams but do not establish passing or complete integration coverage; no tests were run for this research.

## Existing protections to preserve

| Surface | Inspected mechanism | Remaining qualification question |
| --- | --- | --- |
| `internal/docker/client.go:applyContainerHardening`, `applyServerRootfs` | Drops all capabilities then adds CHOWN/SETGID/SETUID for itzg bootstrap, enables no-new-privileges and pids limit; read-only root, bounded noexec tmpfs, writable /data and SKIP_NSSWITCH_CONF=TRUE | Are supported image/tag, custom UID/GID, rootless/userns and operator override combinations compatible after all effective config merges? |
| `client.go:DefaultRestartPolicy`; `sleep.go` | on-failure with bounded retries, plus explicit restart=no for deep sleep and restoration on wake | Does desired state remain authoritative after daemon/controller restarts, late Docker effects and policy overrides? |
| `client.go:CreateContainer` and identity helpers | Deterministic names, server/node/managed labels, preflight adoption and conflict resolution | Does ownership plus configuration revision match before adoption, especially after unknown outcomes, rollback or another controller? |
| `internal/reconciler/events.go`, `reconciler.go` | Event reconnection/backoff, label filters, periodic full-server sweep and one inspection view of exit/OOM/status | Are event gaps, deleted/replaced node clients and long disconnected windows observable and convergent? |
| `internal/docker/pool.go` | Remote-client fallback disabled by default; strict resolver exists for dangerous operations; health checking | Do every server/module/file/backup/scheduler caller and reconnect path honor the intended daemon identity? |
| `pkg/utils/memory.go`, `client.go` | Explicit JVM/native reserve calculation; custom heap can enlarge container limit; memory-swap equals memory | Does admission account for effective post-override allocation, and does the target daemon actually enforce its cgroups? |
| `client.go`, `module.go` | json-file logging with 10m rotation and 3 files | Are world logs, app/event history, tmpfs, images, backups and upload staging separately bounded and visible? |
| `internal/docker/cleanup.go` | Adoption-first startup reconciliation; refuses orphan purge when DB or managed-container reads fail | Is a genuinely absent row enough authority to delete, given a restored/wrong database or multiple control planes on one daemon? |
| `internal/docker/stop.go` | Graceful game/Docker stop ladder with paused-container handling | Is observed termination confirmed and world save status preserved on timeout, cancellation, forced kill and DB failure? |
| `internal/docker/transition.go` | In-process lifecycle serialization across RPC/sleep/freezer paths | Is operation intent recoverable after process death, and is multi-controller use explicitly unsupported or safely coordinated? |

These mechanisms are source-present, not absent. Follow-on items should strengthen integration, capability truth and recovery rather than replace them blindly.

## R01. Events are hints; reconciliation must survive their loss

**Source fact:** Docker events are node-scoped as applicable and expose a bounded recent history of 256 events. Without a since option, the stream observes new events. It is not an infinite durable lifecycle journal. [Docker events](https://docs.docker.com/reference/cli/docker/system/events/)

**Observed:** the OSS Watcher reconnects but starts each stream without a replay cursor; the reconciler's periodic sweep is the existing safety net for missed windows.

**Proposed countermeasure:** retain level-triggered inspection as truth, expose per-node stream connection/last event/last successful sweep, run a scoped resync after reconnection, and track request/operation identities independently. Treat inspect errors as unavailable/unknown, never as missing or permission to recreate/delete. A replay cursor may reduce latency but cannot replace full resync.

**Later fault injection:** disconnect the stream, create more than 256 transitions, restart the daemon, and confirm convergence from actual containers without duplicate creation, lost intentional sleep or orphan deletion. Distinguish an absent container from daemon unreachable.

## R02. Restart policy and intentional state need one authority

**Source facts:** on-failure restarts nonzero exits but does not restart a container merely because the daemon restarted. Always and unless-stopped have different daemon-restart/manual-stop behavior; Docker documents a successful-start monitoring threshold and warns against competing host process managers. [Restart policy semantics](https://docs.docker.com/engine/containers/start-containers-automatically/)

**Observed:** Carbon uses on-failure:5 and temporarily disables restarts for deep sleep. Operator overrides can change policy.

**Proposed countermeasure:** document desired versus observed state, crash budget/backoff/quarantine, whether daemon-restart recovery starts an intentionally running server, and how explicit stop/sleep/cancel survives process death. Never solve recovery by globally replacing policy with always/unless-stopped. Prevent external autohealers from fighting the controller; preserve the existing updater opt-out labels.

**Later fault injection:** combine clean stop, crash, OOM, paused/deep-sleep state, policy override, daemon restart and controller restart. Count actual starts and verify no flapping or permanently stranded desired-running service.

## R03. Live restore is optional resilience, not control-plane availability

**Source facts:** live restore can keep standalone Linux containers alive while the daemon is unavailable. It has upgrade/configuration restrictions; an extended daemon outage can fill log FIFOs and block logging. It is not supported for Windows containers, though Linux containers on Docker Desktop for Windows are supported. [Docker live restore](https://docs.docker.com/engine/daemon/live-restore/)

**Proposed countermeasure:** offer an operator-documented qualification profile, not an automatic daemon mutation. During daemon loss, show management unavailable and game health unknown/last observed while preserving known container/world identity. Reconnect and resync before any destructive recovery. Do not claim live restore preserves every network or log interaction.

**Later fault injection:** compare supported daemon stop/restart with live restore enabled and disabled, including active game clients, long log output and changed network configuration. Keep these drills isolated from real worlds.

## R04. External command uncertainty needs identity and observation

**Observed:** Carbon already creates with deterministic names/labels and adopts live conflicts. The transition lock is in memory; creation and DB updates are separate effects.

**Design inference:** a client deadline/transport error is not enough evidence to declare the requested world/container absent or to authorize a duplicate. The precise cancellation semantics differ by operation and must be tested, not generalized from one API.

**Proposed countermeasure:** persist requested operation/configuration generation, daemon identity, expected old/new container and stable server identity; inspect labels/name/config before retry or adoption. Classify unknown outcomes separately from confirmed failure, not-found, ownership conflict and user cancellation. Require configuration fingerprint/owner match before adopting a live container.

**Later fault injection:** fake a daemon accepting create/start/remove but dropping the response, then crash the controller before the DB commit. Retry through the real caller seam and verify one owned container, one world writer, visible recovery and preserved original data.

## R05. Image lifecycle needs a deliberate cache/update contract

**Source facts:** image tags are convenient mutable references; a digest identifies an immutable image version. Digest pinning also means future security updates require an explicit digest change. Docker documents that losing the initiating pull connection terminates that pull. [Image pull/digests/cancellation](https://docs.docker.com/reference/cli/docker/image/pull/)

**Observed:** CreateContainer inspects the cached tag and only pulls when inspection fails; pullImage consumes the returned body through io.Copy(io.Discard). This deliberately avoids registry latency for warm starts. The current Docker CLI consumes pulls through a JSON message decoder even in quiet mode. [Docker CLI pull implementation](https://raw.githubusercontent.com/docker/cli/master/cli/command/image/pull.go), [Docker CLI JSON stream adapter](https://raw.githubusercontent.com/docker/cli/master/internal/jsonstream/display.go)

**Candidate failure:** embedded stream error frames may not be surfaced by byte-copying alone; reproduce against the pinned SDK/engine rather than relying on an old issue report.

**Proposed countermeasure:** separate restart-existing, create-from-pinned/cached artifact, explicit update/check and fallback policy. Record resolved image digest/platform/runtime/config revision for rollback. Deduplicate concurrent pulls per daemon/image, decode progress/error frames, bound stalls and preserve useful sanitized progress. An inspection permission/transport failure is not equivalent to image missing.

**Later fault injection:** HTTP-success pull stream containing an error frame, truncated stream, registry rate limit/auth failure, wrong architecture, daemon disconnect and interrupted update. Warm starts should continue under the accepted offline policy; failed updates must not discard the last working identity/world.

## R06. Mount paths, ownership and bootstrap are environment capabilities

**Source facts:** bind mounts refer to paths on the daemon host, not the API client. Desktop runs its daemon in a Linux VM and supplies translation; a remote daemon cannot mount the controller's local files. Missing source-path handling differs by bind syntax/options. [Bind mounts](https://docs.docker.com/engine/storage/bind-mounts/)

User namespaces require host ownership arrangements for writable bind mounts. [User namespace remapping](https://docs.docker.com/engine/security/userns-remap/)

The current upstream itzg entrypoint may change UID/GID, chown /data, optionally modify nsswitch, and then drop to its Minecraft user; a supplied non-root user skips the root branch. [itzg start source](https://raw.githubusercontent.com/itzg/docker-minecraft-server/master/scripts/start), [itzg alternate user guidance](https://docker-minecraft-server.readthedocs.io/en/latest/configuration/misc-options/)

**Proposed countermeasure:** maintain per-node controller-path versus daemon-path mapping and a positive data-path ownership identity. Before creation/recreation, validate the intended host path, read/write/rename behavior, free space and expected server marker through the appropriate daemon-host mechanism. Do not auto-create an empty replacement world when a remote mount or path mapping is missing.

Define a qualified image/UID/GID/read-only-root/capability policy. Preserve least privilege; solve explicit bootstrap requirements instead of falling back to privileged containers or blanket writable roots. Default UID/GID qualification does not prove custom IDs work with a read-only /etc.

**Later fault injection:** missing/wrong remote path, rootless/userns ownership, network-share chown denial, readonly data, custom UID/GID, symlink substitution and Desktop translation. Errors must identify the actionable capability safely without starting an empty world.

## R07. Configured limits are not evidence of effective limits

**Source facts:** memory and swap settings interact; equal memory and memory-swap prevents swapping where supported. CPU caps/shares express limits or relative weight rather than a universal performance reservation. [Docker resource constraints](https://docs.docker.com/engine/containers/resource_constraints/)

Rootless cgroup resource flags require supported cgroup v2/systemd/delegation; under unsupported conditions flags may be ignored. [Rootless resource limitations](https://docs.docker.com/engine/security/rootless/tips/)

**Observed:** Carbon computes heap/native headroom and container memory, then applies operator overrides. Headroom helpers can increase the effective container limit. Daemon-host memory is queried for placement.

**Proposed countermeasure:** derive one effective post-merge specification, use that exact resource vector for admission and creation, and verify the daemon's observed config/cgroup capability. Distinguish requested heap, container memory, host reserve, swap policy and tmpfs consumption. Expose unsupported limits as unsupported rather than successful configuration.

**Later fault injection:** override memory below/above safe heap reserve, low available daemon-host memory, unsupported rootless cgroups, OOM, pids exhaustion and sidecars. Verify truthful OOM/exit classification and bounded crash handling; do not disable OOM protection to hide the symptom.

## R08. World disk, writable layer, logs and backups are separate capacity classes

**Source facts:** Docker storage accounting is not one total: writable layers, volumes/bind data, logs and other artifacts can consume additional space. [Storage drivers and accounting](https://docs.docker.com/engine/storage/drivers/)

Container storage-opt size is driver-dependent; overlay2 requires XFS/pquota. [Container storage option](https://docs.docker.com/reference/cli/docker/container/run/)

**Observed:** ApplyDiskQuota sets StorageOpt[size], but no caller was found by scoped source search. Server world data is a /data bind mount. Metrics measure server-directory usage.

**Design implication:** storage-opt is not a portable world-data quota. Do not advertise it as such. Separate measured directory usage, daemon/host free-space reserve, enforcement capability and transfer/backup growth.

**Proposed countermeasure:** per-node storage capability/pressure reporting; admission and export/backup/update staging reserves; quota enforcement only when the actual world filesystem supports it; bounded uploads/downloads and operation preflight. Avoid “fixing disk pressure” by removing stopped servers, worlds or rollback artifacts.

**Later fault injection:** data filesystem full, daemon image store full, inode exhaustion, backup destination full and staging exhaustion. Existing worlds/config/DB should remain recoverable, and partial outputs must not masquerade as complete.

## R09. Rotation and log consumers need supported Docker behavior

**Source facts:** Docker's json-file logging options bound rotation; changing daemon defaults does not automatically update existing containers. Docker cautions against external tools editing its driver-owned log files. [json-file driver](https://docs.docker.com/engine/logging/drivers/json-file/)

**Observed:** server and module HostConfig already request rotated json-file logs. Application streams, game log files, event/audit data and support bundles are separate layers.

**Proposed countermeasure:** preserve driver rotation; reconnect streams with bounded replay/buffers and container-generation binding; make gaps visible. Prefer application-level clear/view semantics over direct truncation of Docker-private files. Handle unsupported log drivers honestly and avoid letting slow UI consumers block the runtime reader.

**Later fault injection:** slow/disconnected browser, container replacement, daemon restart, huge log line, unsupported driver and exhausted disk. Verify bounded memory, redaction and continued recovery diagnostics.

## R10. Network mode, health and readiness must remain distinct

**Source facts:** host networking shares the host network namespace, so published-port mappings have no effect; platform support differs, including opt-in Desktop support. [Host networking](https://docs.docker.com/engine/network/drivers/host/)

Docker healthchecks produce starting/healthy/unhealthy states under configured timing/probes; container state and health state are distinct. [Container healthchecks](https://docs.docker.com/engine/containers/run/)

**Observed:** Carbon maps Docker running with missing/unknown health information to running. The proxy and direct-network modes use different endpoints and bindings.

**Proposed countermeasure:** expose runtime-running, Minecraft-ready, ingress-reachable and telemetry-fresh separately. Define startup grace and probe policy per runtime/image/version. Validate network mode before promising proxy/direct/additional-port behavior; retain private management/RCON binding. Effective inspect output plus actual protocol connection is stronger evidence than a requested port map.

**Later fault injection:** slow world generation, wrong health port, missing healthcheck, RCON unavailable, host-mode override, bridge deletion, port conflict, IPv4/IPv6 mismatch and remote-node ingress. A panel HTTP 200, TCP listener or live container alone cannot satisfy game-readiness acceptance.

## R11. Docker volumes and prune commands do not implement customer-data retention

**Source facts:** volumes persist independently of containers until removed, while tmpfs data is ephemeral. [Docker storage mounts](https://docs.docker.com/engine/storage/), [Volumes](https://docs.docker.com/engine/storage/volumes/)

Docker prune deliberately removes classes of unused resources; adding flags can expand deletion to volume/image types. Stopped containers are eligible for pruning, so “unused” does not mean “unowned by a stopped Minecraft server.” [Pruning semantics](https://docs.docker.com/engine/manage-resources/pruning/)

**Proposed countermeasure:** explicit ownership and retention metadata, dry-run inventory, strict daemon selection, quarantine and auditable separately named destructive operations. A missing DB row after restore/wrong DB must not automatically imply consent to destroy a managed container. Container cleanup, game data deletion, backup retention and image rollback retention need separate decisions.

Backup/export must have application consistency, manifest/integrity, verification and restore evidence. Docker mount/volume archive recipes are storage primitives rather than a guarantee that live Minecraft files are consistent. Keep world and DB backup procedures coordinated.

**Later fault injection:** wrong/restored/partially available DB, stopped server, detached server, orphan candidate, absent remote node, interrupted backup/restore, corrupted archive and missing encryption key. Prove restoration of gameplay with the intended world, not just archive download or healthy service process.

## Proposed portability qualification profiles

| Profile | Required evidence before support claim |
| --- | --- |
| Native Linux Docker | Negotiated API, daemon identity, cgroup/controller support, storage/mount ownership, required network mode, effective config, graceful stop and restart recovery |
| Rootless/userns Linux | UID/GID mapping and bind permissions, actual delegated resource enforcement, supported network/port behavior and rootless daemon lifetime |
| Docker Desktop / WSL Linux containers | Explicit daemon/context/path identity, Desktop VM resource ceiling and disk, native path translation, sleep/reconnect/restart and actual game ingress |
| Remote Docker endpoint | Authenticated daemon identity, strict no-local-fallback for destructive paths, remote mount/filesystem actions, topology-specific published/game endpoints, reconnect/resync |
| Native Windows containers | No support inferred from Windows controller compatibility or Desktop's Linux-container mode; qualify separately or report unsupported clearly |
| Nested containerized Docker hosts | Qualification against effective cgroup, namespace, ownership, filesystem and capability behavior; do not repair with broad privilege assumptions |

The product should publish a supported/unsupported/degraded capability view for these profiles. It should not infer capability from an endpoint URL or successful Ping alone.

## Implementation handoff requirements

Each later work item should specify: exact source seam/callers, preserved existing behavior, candidate failure trigger, deterministic red-capable reproduction, expected state/resource/data outcome, typed errors, retry/timeout/cancellation policy, operation identity, permissions, telemetry, portability profile, nearest meaningful regression seam, rollout/rollback and required documentation.

A useful integration fixture needs a disposable daemon and explicitly disposable world data. It must support response loss after accepted requests, daemon/event disconnect, slow pulls/start/stop, config/DB failure, denied mounts, OOM/disk/network pressure, concurrent actions and restored-world verification. Production/practical worlds must never be reused as fault-injection fixtures.

Do not merge every countermeasure into one generic retry helper. Safe retry differs across observation, pull, create, start, stop, delete, file transfer and restore; a small runtime interface should hide those distinctions from callers while preserving structured outcomes and ownership.

