// Package reconciler contains the event-source foundation for a future
// self-healing reconciler (MINE-107). This file (events.go) implements a
// multi-node Docker Events API watcher: it connects to each node's Docker
// daemon, subscribes to container lifecycle events for containers managed
// by Carbon Panel, and translates them into a normalized ContainerEvent
// stream that a downstream consumer can act on.
//
// This ticket (MINE-106) intentionally stops at the event source: no
// reconciliation logic, debouncing, or work queue is built here.
package reconciler

import (
	"context"
	"strconv"
	"sync"
	"time"

	dockerevents "github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"

	"github.com/athNdev/carbon-panel/internal/docker"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// Label keys used by Carbon Panel to tag containers it manages.
const (
	labelManaged  = "carbon-panel.managed"
	labelServerID = "carbon-panel.server.id"
	labelModuleID = "carbon-panel.module.id"
)

// Container actions we forward to consumers. Anything else observed on the
// stream (e.g. exec_create, top, archive-path) is discarded.
var forwardedActions = map[dockerevents.Action]bool{
	dockerevents.ActionCreate:       true,
	dockerevents.ActionStart:        true,
	dockerevents.ActionDie:          true,
	dockerevents.ActionDestroy:      true,
	dockerevents.ActionPause:        true,
	dockerevents.ActionUnPause:      true,
	dockerevents.ActionRename:       true,
	dockerevents.ActionHealthStatus: true,
	dockerevents.ActionOOM:          true,
}

// ContainerEvent is the normalized representation of a Docker container
// lifecycle event, translated from the raw Docker Events API message. It is
// the contract consumed by the (future) reconciler.
type ContainerEvent struct {
	// NodeID identifies which node in the ClientPool this event came from.
	NodeID string
	// ServerID is populated from the carbon-panel.server.id label, if
	// present. Empty when the event is for a module container.
	ServerID string
	// ModuleID is populated from the carbon-panel.module.id label, if
	// present. Empty when the event is for a server container.
	ModuleID string
	// ContainerID is the Docker container ID (Actor.ID).
	ContainerID string
	// Action is the raw Docker event action string (e.g. "create",
	// "start", "die", "destroy", "pause", "unpause", "rename",
	// "health_status", "oom").
	Action string
	// ExitCode is parsed from Actor.Attributes["exitCode"] when Action is
	// "die". Nil for all other actions, or if the attribute was missing
	// or unparsable.
	ExitCode *int
	// Timestamp is when the event occurred, per the Docker daemon.
	Timestamp time.Time
	// Attributes carries the raw Actor.Attributes map for anything else a
	// consumer might need (e.g. image name, container name).
	Attributes map[string]string
}

// translateEvent converts a raw Docker events.Message into a ContainerEvent.
// It returns ok=false if the message's action is not one we forward (see
// forwardedActions), or if it is not a container-type event.
//
// This is a pure function so it can be unit tested without a live Docker
// connection.
func translateEvent(nodeID string, msg dockerevents.Message) (ContainerEvent, bool) {
	if msg.Type != dockerevents.ContainerEventType {
		return ContainerEvent{}, false
	}
	if !forwardedActions[msg.Action] {
		return ContainerEvent{}, false
	}

	attrs := msg.Actor.Attributes

	var exitCode *int
	if msg.Action == dockerevents.ActionDie {
		if raw, ok := attrs["exitCode"]; ok {
			if v, err := strconv.Atoi(raw); err == nil {
				exitCode = &v
			}
		}
	}

	ts := time.Unix(msg.Time, 0).UTC()
	if msg.TimeNano != 0 {
		ts = time.Unix(0, msg.TimeNano).UTC()
	}

	return ContainerEvent{
		NodeID:      nodeID,
		ServerID:    attrs[labelServerID],
		ModuleID:    attrs[labelModuleID],
		ContainerID: msg.Actor.ID,
		Action:      string(msg.Action),
		ExitCode:    exitCode,
		Timestamp:   ts,
		Attributes:  attrs,
	}, true
}

// Backoff tuning for reconnect attempts.
const (
	backoffBase       = 1 * time.Second
	backoffMax        = 30 * time.Second
	backoffResetAfter = 10 * time.Second
)

// computeBackoff returns the delay to wait before the (retries+1)th
// reconnect attempt: min(1s * 2^retries, 30s). retries is expected to be
// >= 0.
func computeBackoff(retries int) time.Duration {
	if retries <= 0 {
		return backoffBase
	}
	// Cap the shift to avoid overflow for large retry counts; anything
	// beyond a handful of doublings already exceeds backoffMax.
	shift := retries
	if shift > 10 {
		shift = 10
	}
	d := backoffBase << uint(shift)
	if d > backoffMax || d <= 0 {
		return backoffMax
	}
	return d
}

// eventsSource is the narrow slice of the Docker SDK client that Watcher
// depends on. docker.Client exposes the underlying *client.Client via
// GetDockerClient(), which itself satisfies this interface (its Events
// method matches this signature).
type eventsSource interface {
	Events(ctx context.Context, options dockerevents.ListOptions) (<-chan dockerevents.Message, <-chan error)
}

// Watcher streams container lifecycle events for a single node, translating
// them into ContainerEvent values sent on Out(). It reconnects with
// exponential backoff on stream errors/disconnects and respects context
// cancellation for clean shutdown.
type Watcher struct {
	nodeID string
	client eventsSource
	log    *logger.Logger

	out chan ContainerEvent
}

// NewWatcher constructs a Watcher for a single node's Docker client. The
// returned Watcher does not start streaming until Run is called.
func NewWatcher(nodeID string, cli *docker.Client, log *logger.Logger) *Watcher {
	return &Watcher{
		nodeID: nodeID,
		client: cli.GetDockerClient(),
		log:    log,
		out:    make(chan ContainerEvent, 256),
	}
}

// Out returns the channel Watcher sends translated events on. Callers
// should drain it; Run closes it when it returns.
func (w *Watcher) Out() <-chan ContainerEvent {
	return w.out
}

// Run streams events until ctx is cancelled, reconnecting on error with
// exponential backoff. It blocks until ctx is done, closing Out() before
// returning.
func (w *Watcher) Run(ctx context.Context) {
	defer close(w.out)

	retries := 0
	for {
		if ctx.Err() != nil {
			return
		}

		connectedAt := time.Now()
		streamErr := w.stream(ctx)

		if ctx.Err() != nil {
			return
		}

		if streamErr != nil && w.log != nil {
			w.log.Warn("reconciler: events stream for node %q disconnected: %v", w.nodeID, streamErr)
		}

		if time.Since(connectedAt) >= backoffResetAfter {
			retries = 0
		} else {
			retries++
		}

		delay := computeBackoff(retries)
		if w.log != nil {
			w.log.Info("reconciler: reconnecting events stream for node %q in %s (retry %d)", w.nodeID, delay, retries)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

// stream opens a single Events API connection and forwards messages until
// the stream ends (error, EOF, or ctx cancellation). It returns the error
// that ended the stream, or nil if it ended solely because ctx was
// cancelled.
func (w *Watcher) stream(ctx context.Context) error {
	filterArgs := filters.NewArgs()
	filterArgs.Add("type", string(dockerevents.ContainerEventType))
	filterArgs.Add("label", labelManaged+"=true")

	msgCh, errCh := w.client.Events(ctx, dockerevents.ListOptions{Filters: filterArgs})

	for {
		select {
		case <-ctx.Done():
			return nil
		case err, ok := <-errCh:
			if !ok {
				return nil
			}
			return err
		case msg, ok := <-msgCh:
			if !ok {
				return nil
			}
			ev, forward := translateEvent(w.nodeID, msg)
			if !forward {
				continue
			}
			select {
			case w.out <- ev:
			case <-ctx.Done():
				return nil
			}
		}
	}
}

// Supervisor manages one Watcher per node and fans their output into a
// single merged event channel.
//
// Known limitation: the set of nodes watched is fixed at Start() time.
// Nodes added to or removed from the ClientPool while the Supervisor is
// running are not picked up dynamically — restart the Supervisor to observe
// membership changes. Dynamic membership tracking is left as follow-up
// scope (expected to land alongside or after MINE-107).
type Supervisor struct {
	log *logger.Logger

	nodeIDs  []string
	resolver func(nodeID string) (*docker.Client, error)

	merged chan ContainerEvent

	mu       sync.Mutex
	started  bool
	watchers []*Watcher
	wg       sync.WaitGroup
}

// NewSupervisor constructs a Supervisor for an explicit set of node IDs,
// using resolve to obtain a *docker.Client for each node ID. This is the
// most explicit constructor: callers who already know their node set (or
// who want to wire node discovery themselves, e.g. via a NodeStore's
// ListNodes) should use this directly.
func NewSupervisor(nodeIDs []string, resolve func(nodeID string) (*docker.Client, error), log *logger.Logger) *Supervisor {
	ids := make([]string, len(nodeIDs))
	copy(ids, nodeIDs)
	return &Supervisor{
		log:      log,
		nodeIDs:  ids,
		resolver: resolve,
		merged:   make(chan ContainerEvent, 256),
	}
}

// NewSupervisorFromNodeStore constructs a Supervisor that auto-discovers
// its node set from store.ListNodes (the same NodeStore backing pool; see
// internal/docker.NodeStore) and resolves each node's client via
// pool.GetClientStrict. Only enabled nodes are included. Discovery happens
// once, at call time — see the Supervisor doc comment for the static
// node-set limitation.
func NewSupervisorFromNodeStore(ctx context.Context, pool *docker.ClientPool, store docker.NodeStore, log *logger.Logger) (*Supervisor, error) {
	nodes, err := store.ListNodes(ctx)
	if err != nil {
		return nil, err
	}

	nodeIDs := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n == nil || !n.Enabled {
			continue
		}
		nodeIDs = append(nodeIDs, n.ID)
	}

	resolve := func(nodeID string) (*docker.Client, error) {
		return pool.GetClientStrict(nodeID)
	}

	return NewSupervisor(nodeIDs, resolve, log), nil
}

// Events returns the merged, fanned-in channel of ContainerEvent values
// across all watched nodes. Valid after Start has been called.
func (s *Supervisor) Events() <-chan ContainerEvent {
	return s.merged
}

// Start resolves a client for each configured node, launches one Watcher
// per node, and begins fanning their output into Events(). It returns an
// error if any node's client could not be resolved; nodes that resolved
// successfully before the failure are still started (best-effort startup).
//
// Start returns once all watchers are launched; it does not block for the
// lifetime of the supervisor. Cancel ctx to stop all watchers and close
// Events().
func (s *Supervisor) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	s.mu.Unlock()

	var firstErr error
	for _, nodeID := range s.nodeIDs {
		cli, err := s.resolver(nodeID)
		if err != nil {
			if s.log != nil {
				s.log.Error("reconciler: failed to resolve Docker client for node %q: %v", nodeID, err)
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}

		w := NewWatcher(nodeID, cli, s.log)
		s.mu.Lock()
		s.watchers = append(s.watchers, w)
		s.mu.Unlock()

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			w.Run(ctx)
		}()

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			for {
				select {
				case ev, ok := <-w.Out():
					if !ok {
						return
					}
					select {
					case s.merged <- ev:
					case <-ctx.Done():
						return
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}

	// Close the merged channel once every watcher and fan-in goroutine
	// has exited (i.e. once ctx is cancelled and everything drains).
	go func() {
		s.wg.Wait()
		close(s.merged)
	}()

	return firstErr
}
