// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// ContainerFreezer defines the container pause/unpause and status capabilities
type ContainerFreezer interface {
	PauseContainer(ctx context.Context, containerID string) error
	UnpauseContainer(ctx context.Context, containerID string) error
	GetContainerStatus(ctx context.Context, containerID string) (db.ServerStatus, error)
}

// FreezerStore defines storage operations needed by HibernationManager
type FreezerStore interface {
	GetServer(ctx context.Context, id string) (*db.Server, error)
	ListServers(ctx context.Context) ([]*db.Server, error)
	UpdateServer(ctx context.Context, server *db.Server) error
	UpdateServerStatus(ctx context.Context, id string, status db.ServerStatus) error
}

func wrapStatusPersistError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("failed to persist error status after rollback failure: %w", err)
}

func (m *HibernationManager) restoreRunningContainer(ctx context.Context, freezer ContainerFreezer, serverID, containerID string) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	rollbackErr := freezer.UnpauseContainer(rollbackCtx, containerID)
	if rollbackErr == nil {
		status, statusErr := freezer.GetContainerStatus(rollbackCtx, containerID)
		if statusErr != nil {
			rollbackErr = fmt.Errorf("could not verify container after unpause: %w", statusErr)
		} else if status != db.StatusRunning {
			rollbackErr = fmt.Errorf("container remained in status %s after unpause", status)
		}
	}
	if rollbackErr == nil {
		return nil
	}

	statusErr := m.store.UpdateServerStatus(rollbackCtx, serverID, db.StatusError)
	return errors.Join(
		fmt.Errorf("failed to restore running container after hibernation failure: %w", rollbackErr),
		wrapStatusPersistError(statusErr),
	)
}

// HibernationManager manages cgroup freezer sleep/wake lifecycle for idle Minecraft servers
type HibernationManager struct {
	freezer         ContainerFreezer
	freezerResolver func(nodeID string) ContainerFreezer
	store           FreezerStore
	logger          *logger.Logger
	idle            *idleTracker
	runner          *idleRunner
	wakeCallbacks   []func(serverID string)
	mu              sync.Mutex

	hibernatedTotal uint64
	wokenTotal      uint64
}

// NewHibernationManager creates a new HibernationManager
func NewHibernationManager(freezer ContainerFreezer, store FreezerStore, log *logger.Logger) *HibernationManager {
	m := &HibernationManager{
		freezer: freezer,
		store:   store,
		logger:  log,
		idle:    newIdleTracker(),
	}
	m.runner = newIdleRunner(store, m.idle, m.idleMode(), log)
	return m
}

// idleMode is the freeze half of the shared idle sweep.
func (m *HibernationManager) idleMode() idleMode {
	return idleMode{
		DefaultTimeoutMinutes: 10,
		Eligible:              func(s *db.Server) bool { return s.AutoHibernate },
		TimeoutMinutes:        func(s *db.Server) int { return s.IdleTimeoutMinutes },
		Sleep:                 m.HibernateServer,
		Ready:                 func() bool { return m.freezer != nil },
		IdleLogFormat:         "Server %s (%s) idle for %v, hibernating container %s via cgroup freezer...",
		ErrorLogFormat:        "Failed to hibernate server %s: %v",
		SweepLogFormat:        "Error checking idle servers for hibernation: %v",
	}
}

// SetFreezerResolver installs a node-aware freezer resolver. The resolver is
// authoritative for explicit node IDs; the configured freezer remains the local
// fallback for empty/local/default aliases.
func (m *HibernationManager) SetFreezerResolver(resolver func(nodeID string) ContainerFreezer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.freezerResolver = resolver
}

func (m *HibernationManager) hasFreezerSource() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.freezer != nil || m.freezerResolver != nil
}

func (m *HibernationManager) freezerFor(nodeID string) (ContainerFreezer, error) {
	m.mu.Lock()
	resolver := m.freezerResolver
	local := m.freezer
	m.mu.Unlock()
	if resolver != nil {
		if freezer := resolver(nodeID); freezer != nil {
			return freezer, nil
		}
	}
	if nodeID == "" || nodeID == "local" || nodeID == "default" {
		if local != nil {
			return local, nil
		}
	}
	return nil, fmt.Errorf("no docker freezer available for node %q", nodeID)
}

// SetPlayerCountProvider configures the online-player source of truth used
// by CheckIdleServers instead of the (never persisted) row field (MINE-120).
func (m *HibernationManager) SetPlayerCountProvider(provider func(serverID string) int) {
	m.idle.SetPlayerCountProvider(provider)
}

// tryBeginTransition single-flights hibernate/wake per server: returns false
// (and logs) if a transition for serverID is already running.
func (m *HibernationManager) tryBeginTransition(serverID, op string) bool {
	return m.idle.tryBegin(serverID, op, m.logger)
}

func (m *HibernationManager) endTransition(serverID string) {
	m.idle.end(serverID)
}

// RegisterWakeCallback registers a callback fired when a server is woken
func (m *HibernationManager) RegisterWakeCallback(cb func(serverID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wakeCallbacks = append(m.wakeCallbacks, cb)
}

// TotalHibernated returns the cumulative number of server hibernations performed (SCH-6).
func (m *HibernationManager) TotalHibernated() uint64 {
	return atomic.LoadUint64(&m.hibernatedTotal)
}

// TotalWoken returns the cumulative number of server wakeups performed (SCH-6).
func (m *HibernationManager) TotalWoken() uint64 {
	return atomic.LoadUint64(&m.wokenTotal)
}

// Start begins the background idle-checker routine
func (m *HibernationManager) Start(ctx context.Context, checkInterval time.Duration) {
	m.runner.Start(ctx, checkInterval)
}

// Stop terminates the background idle-checker routine
func (m *HibernationManager) Stop() {
	m.runner.Stop()
}

// RecordActivity marks a server as recently active, resetting its idle countdown
func (m *HibernationManager) RecordActivity(serverID string) {
	m.idle.RecordActivity(serverID)
}

// CheckIdleServers evaluates all active servers and hibernates idle ones
func (m *HibernationManager) CheckIdleServers(ctx context.Context) error {
	if m.store == nil || !m.hasFreezerSource() {
		return nil
	}
	return m.runner.Check(ctx)
}

// HibernateServer freezes a server container using cgroup freezer and updates status
func (m *HibernationManager) HibernateServer(ctx context.Context, serverID string) error {
	if !m.tryBeginTransition(serverID, "hibernate") {
		return fmt.Errorf("hibernate already in flight for server %s", serverID)
	}
	defer m.endTransition(serverID)

	srv, err := m.store.GetServer(ctx, serverID)
	if err != nil {
		return fmt.Errorf("server %s not found: %w", serverID, err)
	}

	if srv.ContainerID == "" {
		return fmt.Errorf("server %s has no container", serverID)
	}

	if srv.Status != db.StatusRunning {
		return fmt.Errorf("cannot hibernate server %s in status %s", serverID, srv.Status)
	}

	freezer, err := m.freezerFor(srv.NodeID)
	if err != nil {
		return err
	}

	if err := freezer.PauseContainer(ctx, srv.ContainerID); err != nil {
		return fmt.Errorf("cgroup freeze failed for server %s: %w", serverID, err)
	}
	status, statusErr := freezer.GetContainerStatus(ctx, srv.ContainerID)
	if statusErr != nil {
		return errors.Join(
			fmt.Errorf("failed to verify frozen container for server %s: %w", serverID, statusErr),
			m.restoreRunningContainer(ctx, freezer, serverID, srv.ContainerID),
		)
	}
	if status != db.StatusPaused {
		return errors.Join(
			fmt.Errorf("container for server %s did not enter paused state (status %s)", serverID, status),
			m.restoreRunningContainer(ctx, freezer, serverID, srv.ContainerID),
		)
	}

	if err := m.store.UpdateServerStatus(ctx, serverID, db.StatusPaused); err != nil {
		return errors.Join(
			fmt.Errorf("failed to update server status to paused: %w", err),
			m.restoreRunningContainer(ctx, freezer, serverID, srv.ContainerID),
		)
	}

	m.idle.RecordActivity(serverID)

	if m.logger != nil {
		m.logger.Info("Successfully hibernated server %s (%s)", srv.Name, serverID)
	}

	atomic.AddUint64(&m.hibernatedTotal, 1)
	return nil
}

// WakeServer unfreezes a server container using cgroup freezer and restores running status
func (m *HibernationManager) WakeServer(ctx context.Context, serverID string) error {
	if !m.tryBeginTransition(serverID, "wake") {
		return fmt.Errorf("wake already in flight for server %s", serverID)
	}
	defer m.endTransition(serverID)

	srv, err := m.store.GetServer(ctx, serverID)
	if err != nil {
		return fmt.Errorf("server %s not found: %w", serverID, err)
	}

	if srv.ContainerID == "" {
		return fmt.Errorf("server %s has no container", serverID)
	}

	// If already running, nothing to do
	if srv.Status == db.StatusRunning {
		return nil
	}

	if srv.Status != db.StatusPaused {
		return fmt.Errorf("cannot wake server %s with non-paused status %s", serverID, srv.Status)
	}

	freezer, err := m.freezerFor(srv.NodeID)
	if err != nil {
		return err
	}

	start := time.Now()
	if err := freezer.UnpauseContainer(ctx, srv.ContainerID); err != nil {
		failCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		st, statusErr := freezer.GetContainerStatus(failCtx, srv.ContainerID)
		persistStatus := db.StatusError
		if statusErr == nil && st != db.StatusPaused {
			persistStatus = st
		}
		persistErr := m.store.UpdateServerStatus(failCtx, serverID, persistStatus)
		return errors.Join(
			fmt.Errorf("cgroup unfreeze failed for server %s: %w", serverID, err),
			statusErr,
			wrapStatusPersistError(persistErr),
		)
	}

	if err := m.store.UpdateServerStatus(ctx, serverID, db.StatusRunning); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		rollbackErr := freezer.PauseContainer(rollbackCtx, srv.ContainerID)
		if rollbackErr == nil {
			status, statusErr := freezer.GetContainerStatus(rollbackCtx, srv.ContainerID)
			if statusErr != nil {
				rollbackErr = fmt.Errorf("could not verify container after re-pause: %w", statusErr)
			} else if status != db.StatusPaused {
				rollbackErr = fmt.Errorf("container remained in status %s after re-pause", status)
			}
		}
		if rollbackErr != nil {
			statusErr := m.store.UpdateServerStatus(rollbackCtx, serverID, db.StatusError)
			return errors.Join(
				fmt.Errorf("failed to update server status to running: %w", err),
				fmt.Errorf("failed to restore paused container after status update failure: %w", rollbackErr),
				wrapStatusPersistError(statusErr),
			)
		}
		return fmt.Errorf("failed to update server status to running: %w", err)
	}

	m.idle.RecordActivity(serverID)

	if m.logger != nil {
		m.logger.Info("Successfully woke server %s (%s) from hibernation in %v", srv.Name, serverID, time.Since(start))
	}

	atomic.AddUint64(&m.wokenTotal, 1)

	m.mu.Lock()
	callbacks := append([]func(string){}, m.wakeCallbacks...)
	m.mu.Unlock()

	for _, cb := range callbacks {
		cb(serverID)
	}

	return nil
}

// IsHibernated returns whether the server is currently hibernated/paused
func (m *HibernationManager) IsHibernated(ctx context.Context, serverID string) bool {
	srv, err := m.store.GetServer(ctx, serverID)
	if err != nil || srv == nil {
		return false
	}
	return srv.Status == db.StatusPaused
}
