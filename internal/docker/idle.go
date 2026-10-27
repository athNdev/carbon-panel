// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// idleMode is the mode-specific half of an idle sweep. It says which servers
// opt in, how long they must be idle, and what to do when they are. The
// tracker owns the shared half: the activity clock, the claim and the
// per-server bookkeeping.
type idleMode struct {
	// DefaultTimeoutMinutes applies when the server row has no timeout set.
	DefaultTimeoutMinutes int
	// Eligible reports whether the server opts into this idle mode.
	Eligible func(*db.Server) bool
	// TimeoutMinutes reads the configured timeout from the server row.
	TimeoutMinutes func(*db.Server) int
	// Sleep performs the mode's container action.
	Sleep func(ctx context.Context, serverID string) error
	// Ready reports whether the mode's daemon side is configured. A nil Ready
	// means the mode is always ready to sweep.
	Ready func() bool
	// IdleLogFormat, ErrorLogFormat and SweepLogFormat preserve each mode's
	// operator-facing messages: name, id, idle duration, container id, then
	// name and error, then the sweep error.
	IdleLogFormat  string
	ErrorLogFormat string
	SweepLogFormat string
}

// idleTracker is the per-server state shared by the cgroup-freezer and deep
// sleep managers: who is idle, since when, and who currently owns a transition.
//
// Both modes used to keep their own map, which meant two copies of the same
// eligibility rule and two chances to release a transition the other mode held.
type idleTracker struct {
	mu       sync.RWMutex
	since    map[string]time.Time
	inflight map[string]struct{}
	players  func(serverID string) int
}

func newIdleTracker() *idleTracker {
	return &idleTracker{
		since:    make(map[string]time.Time),
		inflight: make(map[string]struct{}),
	}
}

// SetPlayerCountProvider configures the online-player source of truth. Server
// rows never persist PlayersOnline (gorm:"-"), so without a provider every
// server looks idle (MINE-120).
func (t *idleTracker) SetPlayerCountProvider(provider func(serverID string) int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.players = provider
}

// OnlinePlayers returns the authoritative online-player count for a server.
func (t *idleTracker) OnlinePlayers(srv *db.Server) int {
	t.mu.RLock()
	provider := t.players
	t.mu.RUnlock()
	if provider != nil {
		return provider(srv.ID)
	}
	return srv.PlayersOnline
}

// RecordActivity resets the idle countdown for serverID.
func (t *idleTracker) RecordActivity(serverID string) {
	t.forget(serverID)
}

// forget stops tracking serverID, so the next sweep starts its clock again.
func (t *idleTracker) forget(serverID string) {
	t.mu.Lock()
	delete(t.since, serverID)
	t.mu.Unlock()
}

// idleFor reports how long serverID has been continuously idle. The first
// observation starts the clock and reports false, matching the previous
// behaviour where a newly idle server waits one full interval.
func (t *idleTracker) idleFor(serverID string, now time.Time) (time.Duration, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	since, tracked := t.since[serverID]
	if !tracked {
		t.since[serverID] = now
		return 0, false
	}
	return now.Sub(since), true
}

// tryBegin claims a transition for serverID. It reports false, with a debug
// line, when the server is already transitioning in either mode.
func (t *idleTracker) tryBegin(serverID, op string, log *logger.Logger) bool {
	if !TryBeginServerTransition(serverID) {
		if log != nil {
			log.Debug("Skipping %s for server %s: transition already in flight", op, serverID)
		}
		return false
	}
	t.mu.Lock()
	if _, busy := t.inflight[serverID]; busy {
		t.mu.Unlock()
		EndServerTransition(serverID)
		if log != nil {
			log.Debug("Skipping %s for server %s: transition already in flight", op, serverID)
		}
		return false
	}
	t.inflight[serverID] = struct{}{}
	t.mu.Unlock()
	return true
}

func (t *idleTracker) end(serverID string) {
	t.mu.Lock()
	delete(t.inflight, serverID)
	t.mu.Unlock()
	EndServerTransition(serverID)
}

// transitioning reports whether an idle transition owns serverID.
func (t *idleTracker) transitioning(serverID string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	_, busy := t.inflight[serverID]
	return busy
}

// markIdleSince backdates the activity clock. Tests use it to place a server
// past its timeout without waiting.
func (t *idleTracker) markIdleSince(serverID string, at time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.since[serverID] = at
}

// sweep evaluates servers under mode and sleeps the ones that have been idle
// past their timeout. Eligibility, the timeout fallback and the observation
// order are the same for every mode; only idleMode differs.
func (t *idleTracker) sweep(ctx context.Context, servers []*db.Server, mode idleMode, log *logger.Logger) {
	now := time.Now()
	for _, srv := range servers {
		if !mode.Eligible(srv) || srv.Status != db.StatusRunning || srv.ContainerID == "" {
			t.forget(srv.ID)
			continue
		}
		if t.OnlinePlayers(srv) > 0 {
			t.forget(srv.ID)
			continue
		}

		timeoutMinutes := mode.TimeoutMinutes(srv)
		if timeoutMinutes <= 0 {
			timeoutMinutes = mode.DefaultTimeoutMinutes
		}
		idleFor, tracked := t.idleFor(srv.ID, now)
		if !tracked || idleFor < time.Duration(timeoutMinutes)*time.Minute {
			continue
		}

		if log != nil {
			log.Info(mode.IdleLogFormat, srv.Name, srv.ID, idleFor, srv.ContainerID)
		}
		if err := mode.Sleep(ctx, srv.ID); err != nil {
			if log != nil {
				log.Error(mode.ErrorLogFormat, srv.Name, err)
			}
		}
	}
}

// idleServerStore is the store surface a runner needs. Both FreezerStore and
// SleeperStore satisfy it.
type idleServerStore interface {
	ListServers(ctx context.Context) ([]*db.Server, error)
}

// idleRunner owns the sweep ticker for one idle mode. Before this, each manager
// carried its own copy of Start, Stop and the cancellation pair.
type idleRunner struct {
	store   idleServerStore
	tracker *idleTracker
	mode    idleMode
	logger  *logger.Logger

	mu     sync.Mutex
	cancel context.CancelFunc
}

func newIdleRunner(store idleServerStore, tracker *idleTracker, mode idleMode, log *logger.Logger) *idleRunner {
	return &idleRunner{store: store, tracker: tracker, mode: mode, logger: log}
}

// Check runs one sweep, skipping it while the mode is not configured.
func (r *idleRunner) Check(ctx context.Context) error {
	if r.store == nil || (r.mode.Ready != nil && !r.mode.Ready()) {
		return nil
	}
	servers, err := r.store.ListServers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list servers: %w", err)
	}
	r.tracker.sweep(ctx, servers, r.mode, r.logger)
	return nil
}

// Start sweeps on interval until ctx is cancelled or Stop is called.
func (r *idleRunner) Start(ctx context.Context, interval time.Duration) {
	runCtx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	if r.cancel != nil {
		r.cancel()
	}
	r.cancel = cancel
	r.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				if err := r.Check(runCtx); err != nil && r.logger != nil {
					r.logger.Warn(r.mode.SweepLogFormat, err)
				}
			}
		}
	}()
}

// Stop terminates the sweep loop.
func (r *idleRunner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
}
