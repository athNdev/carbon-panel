// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

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
	"strings"
	"sync"
	"time"

	dockerevents "github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"

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
	Events(ctx context.Context, options client.EventsListOptions) client.EventsResult
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
	filterArgs := make(client.Filters)
	filterArgs.Add("type", string(dockerevents.ContainerEventType))
	filterArgs.Add("label", labelManaged+"=true")

	eventsResult := w.client.Events(ctx, client.EventsListOptions{Filters: filterArgs})
	msgCh, errCh := eventsResult.Messages, eventsResult.Err

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

// eventWatcher is the stream a Supervisor runs for one node.
type eventWatcher interface {
	Run(ctx context.Context)
	Out() <-chan ContainerEvent
}

// WatchedNode is a node the supervisor should watch, with the endpoint
// generation the client was resolved for. A later generation replaces the
// watcher so it does not keep a client from the previous daemon endpoint.
type WatchedNode struct {
	ID         string
	Generation int64
}

// supervisedNode is one node's watcher and the cancel that stops it.
type supervisedNode struct {
	cancel     context.CancelFunc
	generation int64
}

// Supervisor manages one Watcher per node and fans their output into a
// single merged event channel. Sync and WatchMembership add and remove
// nodes while the supervisor is running.
type Supervisor struct {
	log *logger.Logger

	nodeIDs []string
	factory func(nodeID string) (eventWatcher, error)

	merged chan ContainerEvent

	mu          sync.Mutex
	reconcileMu sync.Mutex
	started     bool
	parent      context.Context
	running     map[string]*supervisedNode
	// stale is the last resolve error for a desired node that has no watcher.
	stale map[string]string
	wg    sync.WaitGroup
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
		log:     log,
		nodeIDs: ids,
		merged:  make(chan ContainerEvent, 256),
		running: map[string]*supervisedNode{},
		factory: func(nodeID string) (eventWatcher, error) {
			cli, err := resolve(nodeID)
			if err != nil {
				return nil, err
			}
			return NewWatcher(nodeID, cli, log), nil
		},
	}
}

// NewSupervisorFromNodeStore constructs a Supervisor that auto-discovers
// its node set from store.ListNodes (the same NodeStore backing pool; see
// internal/docker.NodeStore) and resolves each node's client via
// pool.GetClientStrict. Only enabled nodes are included. The initial set is
// read at call time; WatchMembership picks up nodes added or disabled later.
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
// Events(). Sync may change membership until then.
func (s *Supervisor) Start(ctx context.Context) error {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = true
	s.parent = ctx
	if s.running == nil {
		s.running = map[string]*supervisedNode{}
	}
	ids := append([]string(nil), s.nodeIDs...)
	s.mu.Unlock()

	go s.closeWhenDone(ctx)
	return s.apply(watchedFromIDs(ids), false)
}

// Sync sets the watched node set. Before Start it only records the set.
// After Start it starts watchers for new nodes and stops watchers for
// nodes that disappeared. A node that fails to resolve is skipped and the
// first such error is returned; nodes that are already running stay up.
func (s *Supervisor) Sync(nodeIDs []string) error {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if !started {
		s.mu.Lock()
		s.nodeIDs = uniqueIDs(nodeIDs)
		s.mu.Unlock()
		return nil
	}
	return s.apply(watchedFromIDs(nodeIDs), false)
}

// SyncGenerations sets the watched set and replaces a running watcher when
// its endpoint generation changed. A node that fails to resolve is recorded
// as stale and does not stop the other nodes.
func (s *Supervisor) SyncGenerations(nodes []WatchedNode) error {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if !started {
		s.mu.Lock()
		s.nodeIDs = idsFromWatched(uniqueWatched(nodes))
		s.mu.Unlock()
		return nil
	}
	return s.apply(nodes, true)
}

// StaleNodes returns the desired nodes whose Docker client could not be
// resolved. A later successful resolve removes the node from this list.
func (s *Supervisor) StaleNodes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.stale))
	for id := range s.stale {
		out = append(out, id)
	}
	return out
}

// WatchMembership refreshes the watched set on every interval until ctx is
// cancelled. A failed listing keeps the previous watchers.
func (s *Supervisor) WatchMembership(ctx context.Context, every time.Duration, list func(context.Context) ([]string, error)) {
	if every <= 0 {
		every = 15 * time.Second
	}
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ids, err := list(ctx)
			if err != nil {
				if s.log != nil {
					s.log.Warn("reconciler: node membership refresh failed: %v", err)
				}
				continue
			}
			if err := s.Sync(ids); err != nil && s.log != nil {
				s.log.Warn("reconciler: node membership update failed: %v", err)
			}
		}
	}
}

func (s *Supervisor) closeWhenDone(ctx context.Context) {
	<-ctx.Done()
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()
	s.mu.Lock()
	for _, rw := range s.running {
		rw.cancel()
	}
	s.mu.Unlock()
	s.wg.Wait()
	close(s.merged)
}

func (s *Supervisor) apply(nodes []WatchedNode, replaceOnGeneration bool) error {
	if s.parent != nil && s.parent.Err() != nil {
		return s.parent.Err()
	}
	ordered := uniqueWatched(nodes)
	desired := make(map[string]WatchedNode, len(ordered))
	for _, node := range ordered {
		desired[node.ID] = node
	}

	s.mu.Lock()
	if s.running == nil {
		s.running = map[string]*supervisedNode{}
	}
	if s.stale == nil {
		s.stale = map[string]string{}
	}
	var missing []WatchedNode
	for id := range s.stale {
		if _, ok := desired[id]; !ok {
			delete(s.stale, id)
		}
	}
	for id, rw := range s.running {
		want, ok := desired[id]
		if !ok {
			rw.cancel()
			delete(s.running, id)
			delete(s.stale, id)
			continue
		}
		if replaceOnGeneration && rw.generation != want.Generation {
			rw.cancel()
			delete(s.running, id)
		}
	}
	for _, node := range ordered {
		if _, ok := s.running[node.ID]; !ok {
			missing = append(missing, node)
		}
	}
	factory := s.factory
	s.nodeIDs = idsFromWatched(ordered)
	s.mu.Unlock()

	var firstErr error
	for _, node := range missing {
		if s.parent != nil && s.parent.Err() != nil {
			if firstErr == nil {
				firstErr = s.parent.Err()
			}
			break
		}
		w, err := factory(node.ID)
		if err != nil {
			s.noteStale(node.ID, err)
			if s.log != nil {
				s.log.Error("reconciler: failed to resolve Docker client for node %q: %v", node.ID, err)
			}
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if s.parent != nil && s.parent.Err() != nil {
			if firstErr == nil {
				firstErr = s.parent.Err()
			}
			break
		}
		s.startWatcher(node, w)
	}
	return firstErr
}

func (s *Supervisor) noteStale(id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stale == nil {
		s.stale = map[string]string{}
	}
	if err == nil {
		delete(s.stale, id)
		return
	}
	s.stale[id] = err.Error()
}

func (s *Supervisor) startWatcher(node WatchedNode, w eventWatcher) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.parent != nil && s.parent.Err() != nil {
		return
	}
	if _, exists := s.running[node.ID]; exists {
		return
	}
	nodeCtx, cancel := context.WithCancel(s.parent)
	s.running[node.ID] = &supervisedNode{cancel: cancel, generation: node.Generation}
	delete(s.stale, node.ID)
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		w.Run(nodeCtx)
	}()
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
				case <-nodeCtx.Done():
					return
				default:
					// A full merged buffer must not stall this node's event
					// stream. The periodic sweep remains the status source.
				}
			case <-nodeCtx.Done():
				return
			}
		}
	}()
}

func watchedFromIDs(ids []string) []WatchedNode {
	out := make([]WatchedNode, 0, len(ids))
	for _, id := range ids {
		out = append(out, WatchedNode{ID: id})
	}
	return out
}

func idsFromWatched(nodes []WatchedNode) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, node.ID)
	}
	return out
}

func uniqueWatched(nodes []WatchedNode) []WatchedNode {
	index := make(map[string]int, len(nodes))
	out := make([]WatchedNode, 0, len(nodes))
	for _, node := range nodes {
		id := strings.TrimSpace(node.ID)
		if id == "" {
			continue
		}
		node.ID = id
		if at, ok := index[id]; ok {
			out[at] = node
			continue
		}
		index[id] = len(out)
		out = append(out, node)
	}
	return out
}

func uniqueIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
