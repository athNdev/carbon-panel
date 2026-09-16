// Package reconciler implements the level-triggered, self-healing reconciliation
// engine for Carbon Panel (MINE-107, Layer 2.2 of the Resilient Container
// Orchestrator epic, MINE-102).
//
// The reconciler converges the desired state recorded in the database with the
// observed state of the Docker containers backing each server. It is
// level-triggered rather than edge-triggered: lifecycle events from the MINE-106
// Watcher/Supervisor only schedule a per-server convergence pass (coalesced into
// a quiet window), while a periodic sweep guarantees that a dropped event can
// never leave a server permanently divergent.
//
// Identity comes from MINE-103's deterministic ResolveContainer, so a container
// recreated by Watchtower, docker-compose, Portainer, or force-recreate is
// re-adopted rather than mistaken for a loss.
package reconciler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/internal/events"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// Default tuning for the reconciler.
const (
	// DefaultDebounceWindow coalesces bursts of Docker events for the same
	// server into a single convergence pass. It is the dominant contributor to
	// the "external docker stop/kill is reflected within 500ms" budget.
	DefaultDebounceWindow = 300 * time.Millisecond
	// DefaultWorkers is the number of concurrent convergence goroutines.
	DefaultWorkers = 4
	// DefaultSweepInterval is the safety-net period at which every server is
	// re-checked even in the absence of events.
	DefaultSweepInterval = 30 * time.Second
	// workQueueDepth bounds the number of distinct servers awaiting a pass. The
	// queue only ever holds de-duplicated server IDs, so it cannot grow with
	// event volume.
	workQueueDepth = 256
)

// Container exit codes we give special meaning to.
const (
	// exitCodeOOM is 128 + SIGKILL, Docker's OOM-kill signature.
	exitCodeOOM = 137
	// exitCodeSIGTERM is 128 + SIGTERM, reported for a graceful `docker stop`.
	exitCodeSIGTERM = 143
)

// Store is the narrow database surface the Reconciler needs. *db.Store
// satisfies it.
//
// Every write it uses is a targeted column update (MINE-110): the reconciler
// never calls the full-row UpdateServer, which would clobber a ContainerID
// written concurrently by another subsystem.
type Store interface {
	GetServer(ctx context.Context, id string) (*db.Server, error)
	ListServers(ctx context.Context) ([]*db.Server, error)
	GetServerConfig(ctx context.Context, serverID string) (*db.ServerConfig, error)
	UpdateServerStatus(ctx context.Context, id string, status db.ServerStatus) error
	UpdateServerContainerID(ctx context.Context, id string, containerID string) error
}

// Client is the slice of *docker.Client the Reconciler drives.
type Client interface {
	ResolveContainer(ctx context.Context, serverID string) (*container.Summary, error)
	ObserveContainer(ctx context.Context, containerID string) (*docker.ContainerState, error)
	StartContainer(ctx context.Context, containerID string) error
	RecreateContainer(ctx context.Context, oldContainerID string, server *db.Server, serverConfig *db.ServerConfig) (*docker.RecreateContainerResult, error)
}

// ClientResolver resolves a node's Docker client without falling back to the
// local daemon (MINE-109's GetClientStrict). Returning an explicit error lets
// the reconciler distinguish "node unreachable" (leave the DB alone) from
// "container missing" (converge).
type ClientResolver interface {
	GetClientStrict(nodeID string) (Client, error)
}

// ProxyRouter re-points a server's proxy route at its current backend.
type ProxyRouter interface {
	UpdateServerRoute(server *db.Server) error
}

// EventEmitter publishes lifecycle events on the central bus.
type EventEmitter interface {
	Emit(ctx context.Context, event events.Event)
}

// PoolResolver adapts a *docker.ClientPool to the ClientResolver interface used
// by the Reconciler. Go method signatures are invariant, so the pool's concrete
// (*docker.Client, error) cannot satisfy (Client, error) on its own.
type PoolResolver struct {
	Pool *docker.ClientPool
}

// GetClientStrict implements ClientResolver.
func (p PoolResolver) GetClientStrict(nodeID string) (Client, error) {
	if p.Pool == nil {
		return nil, fmt.Errorf("client pool is nil")
	}
	return p.Pool.GetClientStrict(nodeID)
}

// Config tunes the reconciler. Zero-valued fields fall back to the defaults.
type Config struct {
	// DebounceWindow is the per-server quiet window that coalesces bursts of
	// Docker events into a single convergence pass.
	DebounceWindow time.Duration
	// Workers is the number of concurrent convergence goroutines.
	Workers int
	// SweepInterval is the safety-net period at which every server is
	// re-checked even without an event.
	SweepInterval time.Duration
	// DisableSelfHeal turns off automatic recreation of a missing container for
	// a server the database still believes is running. Self-heal is ON by
	// default; when disabled the divergence is only reported as an error.
	DisableSelfHeal bool
}

// Reconciler converges desired DB state with observed Docker state.
type Reconciler struct {
	store   Store
	clients ClientResolver
	proxy   ProxyRouter
	bus     EventEmitter
	log     *logger.Logger

	cfg Config
	deb *debouncer

	work   chan string
	queued map[string]bool
	mu     sync.Mutex

	ctx      context.Context
	wg       sync.WaitGroup
	stop     chan struct{}
	stopOnce sync.Once
	started  bool
}

// New constructs a Reconciler. It does not start any goroutines until Start is
// called.
func New(store Store, clients ClientResolver, proxy ProxyRouter, bus EventEmitter, log *logger.Logger, cfg Config) *Reconciler {
	if cfg.DebounceWindow <= 0 {
		cfg.DebounceWindow = DefaultDebounceWindow
	}
	if cfg.Workers <= 0 {
		cfg.Workers = DefaultWorkers
	}
	if cfg.SweepInterval <= 0 {
		cfg.SweepInterval = DefaultSweepInterval
	}

	return &Reconciler{
		store:   store,
		clients: clients,
		proxy:   proxy,
		bus:     bus,
		log:     log,
		cfg:     cfg,
		deb:     newDebouncer(cfg.DebounceWindow),
		work:    make(chan string, workQueueDepth),
		queued:  make(map[string]bool),
		stop:    make(chan struct{}),
	}
}

// Start launches the worker pool, consumes the supervisor's merged event stream
// (which may be nil), and begins the periodic sweep. It returns immediately;
// cancel ctx or call Stop to shut everything down.
func (r *Reconciler) Start(ctx context.Context, eventStream <-chan ContainerEvent) {
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return
	}
	r.started = true
	r.ctx = ctx
	r.mu.Unlock()

	for i := 0; i < r.cfg.Workers; i++ {
		r.wg.Add(1)
		go r.worker()
	}

	if eventStream != nil {
		r.wg.Add(1)
		go r.consume(eventStream)
	}

	r.wg.Add(1)
	go r.sweepLoop()

	// Converge once at startup so drift accumulated while the panel was down is
	// repaired immediately rather than at the first event or sweep.
	r.enqueueAll(ctx)
}

// Stop halts the reconciler. It is safe to call more than once.
func (r *Reconciler) Stop() {
	r.stopOnce.Do(func() {
		r.deb.stop()
		close(r.stop)
	})
}

// Wait blocks until every reconciler goroutine has exited.
func (r *Reconciler) Wait() {
	r.wg.Wait()
}

// request schedules a convergence pass for serverID once the debounce window's
// quiet period elapses. Repeated calls within the window collapse into one.
func (r *Reconciler) request(serverID string) {
	if serverID == "" {
		return
	}
	r.deb.enqueue(serverID, func() { r.push(serverID) })
}

// push hands a server ID to the worker pool, dropping duplicates that are
// already queued so a burst can never enqueue the same server twice.
func (r *Reconciler) push(serverID string) {
	r.mu.Lock()
	ctx := r.ctx
	if r.queued[serverID] {
		r.mu.Unlock()
		return
	}
	r.queued[serverID] = true
	r.mu.Unlock()

	if ctx == nil {
		ctx = context.Background()
	}

	select {
	case r.work <- serverID:
	case <-ctx.Done():
		r.clearQueued(serverID)
	case <-r.stop:
		r.clearQueued(serverID)
	}
}

func (r *Reconciler) clearQueued(serverID string) {
	r.mu.Lock()
	delete(r.queued, serverID)
	r.mu.Unlock()
}

// consume translates the merged event stream into convergence requests.
func (r *Reconciler) consume(eventStream <-chan ContainerEvent) {
	defer r.wg.Done()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.stop:
			return
		case ev, ok := <-eventStream:
			if !ok {
				return
			}
			// Module containers are not yet reconciled by this engine; only
			// server lifecycle events schedule a pass.
			if ev.ServerID != "" {
				r.request(ev.ServerID)
			}
		}
	}
}

// sweepLoop periodically enqueues every known server so that a dropped event
// (or a missed reconnect window) cannot leave a server permanently divergent.
func (r *Reconciler) sweepLoop() {
	defer r.wg.Done()

	ticker := time.NewTicker(r.cfg.SweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.stop:
			return
		case <-ticker.C:
			r.enqueueAll(r.ctx)
		}
	}
}

func (r *Reconciler) enqueueAll(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	servers, err := r.store.ListServers(ctx)
	if err != nil {
		if r.log != nil {
			r.log.Error("reconciler: failed to list servers for sweep: %v", err)
		}
		return
	}
	for _, s := range servers {
		if s == nil {
			continue
		}
		r.request(s.ID)
	}
}

func (r *Reconciler) worker() {
	defer r.wg.Done()

	for {
		select {
		case <-r.ctx.Done():
			return
		case <-r.stop:
			return
		case serverID := <-r.work:
			r.clearQueued(serverID)
			r.safeReconcile(serverID)
		}
	}
}

// safeReconcile isolates a single convergence pass so a panic in one server
// cannot take down the worker pool.
func (r *Reconciler) safeReconcile(serverID string) {
	defer func() {
		if rec := recover(); rec != nil && r.log != nil {
			r.log.Error("reconciler: panic while reconciling server %s: %v", serverID, rec)
		}
	}()

	if err := r.ReconcileServer(r.ctx, serverID); err != nil && r.log != nil {
		r.log.Error("reconciler: reconcile of server %s failed: %v", serverID, err)
	}
}

// observedState is the reconciler's view of a server's container.
type observedState struct {
	// Exists is false when no container matching the server could be resolved.
	Exists      bool
	ContainerID string
	Status      db.ServerStatus
	ExitCode    int
	OOMKilled   bool
}

// observe resolves the server's container and inspects its runtime state. A
// missing container yields Exists=false and a nil error; a transient Docker or
// node failure is returned as an error so the caller leaves the DB alone.
func (r *Reconciler) observe(ctx context.Context, cli Client, serverID string) (observedState, error) {
	summary, err := cli.ResolveContainer(ctx, serverID)
	if err != nil {
		if errors.Is(err, docker.ErrContainerNotResolved) {
			return observedState{Exists: false}, nil
		}
		return observedState{}, fmt.Errorf("resolve container: %w", err)
	}
	if summary == nil || summary.ID == "" {
		return observedState{Exists: false}, nil
	}

	st, err := cli.ObserveContainer(ctx, summary.ID)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return observedState{Exists: false}, nil
		}
		return observedState{}, fmt.Errorf("observe container: %w", err)
	}

	return observedState{
		Exists:      true,
		ContainerID: st.ContainerID,
		Status:      st.Status,
		ExitCode:    st.ExitCode,
		OOMKilled:   st.OOMKilled,
	}, nil
}

// ReconcileServer converges a single server's DB status with its container's
// observed state, adopting a drifted container ID, self-healing a missing
// container, and emitting lifecycle events on real transitions.
func (r *Reconciler) ReconcileServer(ctx context.Context, serverID string) error {
	server, err := r.store.GetServer(ctx, serverID)
	if err != nil {
		// Deleted, or a transient DB read failure; either way there is nothing
		// safe to converge.
		return nil
	}

	cli, err := r.clients.GetClientStrict(server.NodeID)
	if err != nil {
		if r.log != nil {
			r.log.Warn("reconciler: node %q unavailable for server %s: %v", server.NodeID, server.Name, err)
		}
		return nil
	}

	obs, err := r.observe(ctx, cli, server.ID)
	if err != nil {
		return err
	}

	dec := decide(server.Status, obs, server.Detached, !r.cfg.DisableSelfHeal)

	// Adopt a container whose ID drifted (or was never recorded) before acting,
	// so the self-heal step below targets the container that actually exists.
	// This is the MINE-103 identity contract in action: the container is cattle,
	// its deterministic name/labels are the identity, and a recreated container
	// is re-adopted rather than mistaken for a loss.
	adopted := false
	if obs.Exists && obs.ContainerID != "" && obs.ContainerID != server.ContainerID {
		if err := r.store.UpdateServerContainerID(ctx, server.ID, obs.ContainerID); err != nil {
			return fmt.Errorf("adopt container id: %w", err)
		}
		if r.log != nil {
			r.log.Info("reconciler: adopted container %s for server %s (was %q)", shortContainerID(obs.ContainerID), server.Name, server.ContainerID)
		}
		server.ContainerID = obs.ContainerID
		adopted = true
	}

	selfHealed := false
	if dec.action == actRecreate {
		if err := r.selfHealRecreate(ctx, cli, server); err != nil {
			if r.log != nil {
				r.log.Error("reconciler: self-heal recreate failed for server %s: %v", server.Name, err)
			}
			// Surface the failure instead of silently flapping.
			dec.status = db.StatusError
			dec.reason = "self-heal recreate failed: " + err.Error()
		} else {
			selfHealed = true
		}
	}

	prev := server.Status
	statusChanged := dec.status != "" && dec.status != prev
	if statusChanged {
		if err := r.store.UpdateServerStatus(ctx, server.ID, dec.status); err != nil {
			return fmt.Errorf("update status: %w", err)
		}
		server.Status = dec.status
		r.emitTransition(ctx, server, prev, dec.status, dec.reason)
	}

	// Refresh the proxy route whenever the backend may have moved: a status
	// transition changes whether the route should exist at all, and adopting or
	// recreating a container means the container's IP has changed.
	if statusChanged || adopted || selfHealed || dec.reconcileRoute {
		r.updateRoute(ctx, server)
	}

	return nil
}

// selfHealRecreate replaces a missing container for a server the DB still
// believes is running. It records the new container ID and starts it, since
// RecreateContainer only auto-starts when the previous container was running —
// which is never the case in this path.
func (r *Reconciler) selfHealRecreate(ctx context.Context, cli Client, server *db.Server) error {
	cfg, err := r.store.GetServerConfig(ctx, server.ID)
	if err != nil {
		return fmt.Errorf("load server config: %w", err)
	}

	res, err := cli.RecreateContainer(ctx, server.ContainerID, server, cfg)
	if err != nil {
		return err
	}
	if res == nil || res.NewContainerID == "" {
		return fmt.Errorf("recreate returned no container id")
	}

	if err := r.store.UpdateServerContainerID(ctx, server.ID, res.NewContainerID); err != nil {
		return fmt.Errorf("record recreated container id: %w", err)
	}
	server.ContainerID = res.NewContainerID

	if err := cli.StartContainer(ctx, res.NewContainerID); err != nil {
		return fmt.Errorf("start recreated container: %w", err)
	}

	if r.log != nil {
		r.log.Warn("reconciler: self-healed server %s by recreating missing container %s", server.Name, shortContainerID(res.NewContainerID))
	}
	return nil
}

// updateRoute re-points the proxy at the server's current backend.
func (r *Reconciler) updateRoute(ctx context.Context, server *db.Server) {
	if r.proxy == nil || server.ProxyHostname == "" {
		return
	}
	if err := r.proxy.UpdateServerRoute(server); err != nil && r.log != nil {
		r.log.Error("reconciler: failed to update proxy route for %s: %v", server.Name, err)
	}
}

// emitTransition publishes the lifecycle event corresponding to a status change.
//
// The RPC service already emits SERVER_START / SERVER_STOP when it initiates a
// start, stop, or restart, so the reconciler deliberately does not re-announce
// those: it only reports transitions it discovered itself (external
// `docker start|stop`, crashes, self-heal recreation). SERVER_HEALTHY is
// reserved for a container that has actually become available.
func (r *Reconciler) emitTransition(ctx context.Context, server *db.Server, from, to db.ServerStatus, reason string) {
	if r.log != nil {
		r.log.Info("reconciler: server %s transitioned %s -> %s%s", server.Name, from, to, reasonSuffix(reason))
	}

	if r.bus == nil {
		return
	}

	var eventType v1.TriggeredEventType
	switch to {
	case db.StatusStarting, db.StatusCreating:
		if from == db.StatusStarting {
			// Already announced by the initiating RPC call.
			return
		}
		eventType = v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START
	case db.StatusRunning:
		if from == db.StatusStarting || from == db.StatusUnhealthy {
			eventType = v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_HEALTHY
		} else {
			eventType = v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START
		}
	case db.StatusStopped, db.StatusError:
		if from == db.StatusStopping {
			// Already announced by the initiating RPC call.
			return
		}
		eventType = v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP
	default:
		return
	}

	data := map[string]any{
		"previous_status": string(from),
		"status":          string(to),
	}
	if reason != "" {
		data["reason"] = reason
	}

	r.bus.Emit(ctx, events.Event{Type: eventType, ServerID: server.ID, Data: data})
}

// reconcileAction is the self-heal action a decision calls for.
type reconcileAction int

const (
	// actNone means the observed state needs no corrective action.
	actNone reconcileAction = iota
	// actRecreate means the container is missing and should be recreated.
	actRecreate
)

// decision is the outcome of the pure convergence policy.
type decision struct {
	// status is the status to persist, or "" to leave the DB unchanged.
	status db.ServerStatus
	// action is the self-heal action to perform, if any.
	action reconcileAction
	// reason explains a non-obvious status (e.g. an OOM kill) for logs/events.
	reason string
	// reconcileRoute requests a proxy-route refresh even without a status
	// change (used when reality has converged but the backend may have moved).
	reconcileRoute bool
}

// decide is the pure convergence policy: given the desired status recorded in
// the database and the container state observed from Docker, it returns what (if
// anything) must change. Keeping it pure makes the policy — including the
// OOM/exit-code rules — unit-testable without a live Docker daemon.
//
// Deliberate boundary: a container that exists but has exited is reported as it
// is (stopped, or error for a crash/OOM). Only a container that is entirely
// *missing* is recreated, because an exited container may have been stopped on
// purpose (e.g. `/stop` in-game) and silently restarting it would fight the
// operator and risk a crash loop.
func decide(desired db.ServerStatus, obs observedState, detached, selfHeal bool) decision {
	if !obs.Exists {
		if desired == db.StatusRunning {
			// The catastrophic-loss case this epic targets: Watchtower,
			// force-recreate, or an over-eager purge removed the container while
			// the DB still believes the server is up.
			if selfHeal && !detached {
				return decision{
					status: db.StatusStarting,
					action: actRecreate,
					reason: "container missing; recreating",
				}
			}
			return decision{status: db.StatusError, reason: "container missing"}
		}

		switch desired {
		case db.StatusStopping, db.StatusPaused:
			return decision{status: db.StatusStopped, reconcileRoute: true}
		default:
			// Starting/Creating may be mid-create, and Stopped/Error are already
			// consistent with a missing container.
			return decision{}
		}
	}

	switch obs.Status {
	case db.StatusRunning, db.StatusUnhealthy:
		if desired == db.StatusStopping {
			// A stop is in flight; let the RPC service own the transition
			// instead of flapping the status back to running.
			return decision{}
		}
		if desired == db.StatusUnhealthy {
			// Preserve the explicit unhealthy signal.
			return decision{}
		}
		if obs.Status == desired {
			return decision{}
		}
		return decision{status: obs.Status, reconcileRoute: true}

	case db.StatusPaused:
		if desired == db.StatusPaused {
			return decision{}
		}
		return decision{status: db.StatusPaused}

	case db.StatusStarting:
		// Docker is still restarting the container; let it settle.
		return decision{}

	case db.StatusStopped, db.StatusError:
		if desired == db.StatusStopping {
			// The stop the operator asked for has completed.
			return decision{status: db.StatusStopped, reconcileRoute: true}
		}
		status, reason := classifyExit(obs)
		return decision{status: status, reason: reason, reconcileRoute: true}
	}

	return decision{}
}

// classifyExit maps a non-running container's exit information onto a panel
// status. An OOM kill or a non-zero crash is surfaced as an error with an
// explanatory reason; a clean exit (or the SIGTERM of a graceful `docker stop`)
// is a normal stop.
func classifyExit(obs observedState) (db.ServerStatus, string) {
	if obs.OOMKilled || obs.ExitCode == exitCodeOOM {
		return db.StatusError, fmt.Sprintf("container was OOM-killed (exit code %d)", obs.ExitCode)
	}
	switch obs.ExitCode {
	case 0, exitCodeSIGTERM:
		return db.StatusStopped, ""
	default:
		return db.StatusError, fmt.Sprintf("container exited with code %d", obs.ExitCode)
	}
}

func reasonSuffix(reason string) string {
	if reason == "" {
		return ""
	}
	return " (" + reason + ")"
}

func shortContainerID(id string) string {
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

// debouncer coalesces rapid enqueues for the same key into a single callback
// fired once the key has been quiet for the configured window. Each call to
// enqueue resets the pending timer, so only the last call in a burst fires.
type debouncer struct {
	window time.Duration

	mu     sync.Mutex
	timers map[string]*time.Timer
}

func newDebouncer(window time.Duration) *debouncer {
	return &debouncer{
		window: window,
		timers: make(map[string]*time.Timer),
	}
}

func (d *debouncer) enqueue(key string, fire func()) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if t, ok := d.timers[key]; ok {
		t.Stop()
	}
	d.timers[key] = time.AfterFunc(d.window, func() {
		d.mu.Lock()
		delete(d.timers, key)
		d.mu.Unlock()
		fire()
	})
}

func (d *debouncer) stop() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for key, t := range d.timers {
		t.Stop()
		delete(d.timers, key)
	}
}
