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

func (m *HibernationManager) restoreRunningContainer(ctx context.Context, serverID, containerID string) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	rollbackErr := m.freezer.UnpauseContainer(rollbackCtx, containerID)
	if rollbackErr == nil {
		status, statusErr := m.freezer.GetContainerStatus(rollbackCtx, containerID)
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
	freezer       ContainerFreezer
	store         FreezerStore
	logger        *logger.Logger
	idle          *idleTracker
	wakeCallbacks []func(serverID string)
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc

	hibernatedTotal uint64
	wokenTotal      uint64
}

// NewHibernationManager creates a new HibernationManager
func NewHibernationManager(freezer ContainerFreezer, store FreezerStore, log *logger.Logger) *HibernationManager {
	return &HibernationManager{
		freezer: freezer,
		store:   store,
		logger:  log,
		idle:    newIdleTracker(),
	}
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
	m.mu.Lock()
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(checkInterval)
		defer ticker.Stop()

		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				if err := m.CheckIdleServers(m.ctx); err != nil {
					if m.logger != nil {
						m.logger.Warn("Error checking idle servers for hibernation: %v", err)
					}
				}
			}
		}
	}()
}

// Stop terminates the background idle-checker routine
func (m *HibernationManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
}

// RecordActivity marks a server as recently active, resetting its idle countdown
func (m *HibernationManager) RecordActivity(serverID string) {
	m.idle.RecordActivity(serverID)
}

// CheckIdleServers evaluates all active servers and hibernates idle ones
func (m *HibernationManager) CheckIdleServers(ctx context.Context) error {
	if m.store == nil || m.freezer == nil {
		return nil
	}

	servers, err := m.store.ListServers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list servers: %w", err)
	}

	m.idle.sweep(ctx, servers, idleMode{
		DefaultTimeoutMinutes: 10,
		Eligible:              func(s *db.Server) bool { return s.AutoHibernate },
		TimeoutMinutes:        func(s *db.Server) int { return s.IdleTimeoutMinutes },
		Sleep:                 m.HibernateServer,
		IdleLogFormat:         "Server %s (%s) idle for %v, hibernating container %s via cgroup freezer...",
		ErrorLogFormat:        "Failed to hibernate server %s: %v",
	}, m.logger)

	return nil
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

	if err := m.freezer.PauseContainer(ctx, srv.ContainerID); err != nil {
		return fmt.Errorf("cgroup freeze failed for server %s: %w", serverID, err)
	}
	status, statusErr := m.freezer.GetContainerStatus(ctx, srv.ContainerID)
	if statusErr != nil {
		return errors.Join(
			fmt.Errorf("failed to verify frozen container for server %s: %w", serverID, statusErr),
			m.restoreRunningContainer(ctx, serverID, srv.ContainerID),
		)
	}
	if status != db.StatusPaused {
		return errors.Join(
			fmt.Errorf("container for server %s did not enter paused state (status %s)", serverID, status),
			m.restoreRunningContainer(ctx, serverID, srv.ContainerID),
		)
	}

	if err := m.store.UpdateServerStatus(ctx, serverID, db.StatusPaused); err != nil {
		return errors.Join(
			fmt.Errorf("failed to update server status to paused: %w", err),
			m.restoreRunningContainer(ctx, serverID, srv.ContainerID),
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

	start := time.Now()
	if err := m.freezer.UnpauseContainer(ctx, srv.ContainerID); err != nil {
		failCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		st, statusErr := m.freezer.GetContainerStatus(failCtx, srv.ContainerID)
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
		rollbackErr := m.freezer.PauseContainer(rollbackCtx, srv.ContainerID)
		if rollbackErr == nil {
			status, statusErr := m.freezer.GetContainerStatus(rollbackCtx, srv.ContainerID)
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
