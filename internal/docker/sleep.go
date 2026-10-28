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
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
	"github.com/moby/moby/api/types/container"
)

// SleeperStore defines storage operations needed by DeepSleepManager.
type SleeperStore interface {
	GetServer(ctx context.Context, id string) (*db.Server, error)
	ListServers(ctx context.Context) ([]*db.Server, error)
	UpdateServer(ctx context.Context, server *db.Server) error
	UpdateServerStatus(ctx context.Context, id string, status db.ServerStatus) error
}

// WakePreparer applies any configuration that requires container replacement
// before the sleeping container is started. It may update server.ContainerID
// after persisting the replacement identity.
type WakePreparer func(ctx context.Context, server *db.Server) error

// SleepController defines container start/stop/status capabilities for deep sleep.
type SleepController interface {
	StartContainer(ctx context.Context, containerID string) error
	// StopContainer stops a container, returning (found, err).
	StopContainer(ctx context.Context, containerID string) (bool, error)
	GetContainerStatus(ctx context.Context, containerID string) (db.ServerStatus, error)
	// SetRestartPolicy changes the container restart policy without recreating it.
	SetRestartPolicy(ctx context.Context, containerID string, policy container.RestartPolicy) error
}

// SleepCommander sends in-game console commands (RCON) for graceful shutdown.
type SleepCommander interface {
	SendCommand(ctx context.Context, serverID string, command string) (string, error)
}

// RouteRefresher re-resolves a server's proxy route after a transition.
type RouteRefresher func(serverID string)

// SleepControllerResolver resolves a node-specific SleepController.
type SleepControllerResolver func(nodeID string) SleepController

// Deep sleep power timings (vars to allow fast test execution).
var (
	deepSleepStopPollInterval = 2 * time.Second
	deepSleepStopTimeout      = 90 * time.Second
	deepSleepStartTimeout     = 120 * time.Second
)

// DeepSleepManager stops idle Minecraft servers entirely (zero RAM/CPU) and
// boots them on login intent via the proxy (MINE-121). Unlike the cgroup
// freezer (HibernationManager), nothing of the server stays resident: wake
// is a full container start, so the proxy must hold the client until the
// backend answers (see internal/proxy down-route handling).
type DeepSleepManager struct {
	controller         SleepController
	controllerResolver SleepControllerResolver
	commander          SleepCommander
	store              SleeperStore
	refresher          RouteRefresher
	logger             *logger.Logger
	idle               *idleTracker
	runner             *idleRunner
	mu                 sync.Mutex
	wakePreparer       WakePreparer
}

// NewDeepSleepManager creates a new DeepSleepManager. commander and refresher
// may be nil (RCON flush and route refresh are then skipped).
func NewDeepSleepManager(controller SleepController, store SleeperStore, commander SleepCommander, refresher RouteRefresher, log *logger.Logger) *DeepSleepManager {
	m := &DeepSleepManager{
		controller: controller,
		commander:  commander,
		store:      store,
		refresher:  refresher,
		logger:     log,
		idle:       newIdleTracker(),
	}
	m.runner = newIdleRunner(store, m.idle, m.idleMode(), log)
	return m
}

// idleMode is the deep-sleep half of the shared idle sweep.
func (m *DeepSleepManager) idleMode() idleMode {
	return idleMode{
		DefaultTimeoutMinutes: 30,
		Eligible:              func(s *db.Server) bool { return s.AutoDeepSleep },
		TimeoutMinutes:        func(s *db.Server) int { return s.DeepSleepTimeoutMinutes },
		Sleep:                 m.SleepServer,
		Ready: func() bool {
			m.mu.Lock()
			defer m.mu.Unlock()
			return m.controller != nil || m.controllerResolver != nil
		},
		IdleLogFormat:  "Server %s (%s) idle for %v, deep-sleeping container %s (full stop)...",
		ErrorLogFormat: "Failed to deep-sleep server %s: %v",
		SweepLogFormat: "Error checking idle servers for deep sleep: %v",
	}
}

// SetControllerResolver installs a multi-node controller resolver so deep sleep
// operations target the Docker daemon on the server's assigned node.
func (m *DeepSleepManager) SetControllerResolver(resolver SleepControllerResolver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.controllerResolver = resolver
}

// getController resolves the sleep controller for nodeID. A remote node must be
// served by the resolver: returning the default controller would start or stop
// a container on the wrong host. Only the local/default node may fall back to
// the default controller.
func (m *DeepSleepManager) getController(nodeID string) (SleepController, error) {
	m.mu.Lock()
	resolver := m.controllerResolver
	defaultCtrl := m.controller
	m.mu.Unlock()
	if resolver != nil {
		if ctrl := resolver(nodeID); ctrl != nil {
			return ctrl, nil
		}
	}
	if nodeID == "" || nodeID == "local" || nodeID == "default" {
		if defaultCtrl == nil {
			return nil, fmt.Errorf("no docker controller available for node %q", nodeID)
		}
		return defaultCtrl, nil
	}
	return nil, fmt.Errorf("no docker controller available for node %q", nodeID)
}

// SetPlayerCountProvider configures the online-player source of truth (same
// rationale as HibernationManager: store rows never persist PlayersOnline).
func (m *DeepSleepManager) SetPlayerCountProvider(provider func(serverID string) int) {
	m.idle.SetPlayerCountProvider(provider)
}

// SetWakePreparer installs the staged-configuration preflight for cold wakes.
func (m *DeepSleepManager) SetWakePreparer(preparer WakePreparer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wakePreparer = preparer
}

func (m *DeepSleepManager) tryBeginTransition(serverID, op string) bool {
	return m.idle.tryBegin(serverID, op, m.logger)
}

func (m *DeepSleepManager) endTransition(serverID string) {
	m.idle.end(serverID)
}

// IsTransitioning returns whether the server is currently transitioning (sleeping or waking).
func (m *DeepSleepManager) IsTransitioning(serverID string) bool {
	return m.idle.transitioning(serverID)
}

// Start begins the background idle-checker routine.
func (m *DeepSleepManager) Start(ctx context.Context, checkInterval time.Duration) {
	m.runner.Start(ctx, checkInterval)
}

// Stop terminates the background idle-checker routine.
func (m *DeepSleepManager) Stop() {
	m.runner.Stop()
}

// RecordActivity marks a server as recently active, resetting its idle countdown.
func (m *DeepSleepManager) RecordActivity(serverID string) {
	m.idle.RecordActivity(serverID)
}

// CheckIdleServers evaluates all running deep-sleep-enabled servers and stops idle ones.
func (m *DeepSleepManager) CheckIdleServers(ctx context.Context) error {
	return m.runner.Check(ctx)
}

// rollbackSleep restores a status that matches the observed container after a
// sleep attempt aborts. Unknown state or a failed restoration is recorded as
// ERROR so the reconciler and proxy do not trust stale DeepSleep intent.
func (m *DeepSleepManager) rollbackSleep(ctx context.Context, serverID, containerID string) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	var nodeID string
	if m.store != nil {
		if srv, err := m.store.GetServer(rollbackCtx, serverID); err == nil && srv != nil {
			nodeID = srv.NodeID
		}
	}
	ctrl, ctrlErr := m.getController(nodeID)

	target := db.StatusError
	var observeErr error
	if ctrlErr != nil {
		observeErr = fmt.Errorf("no docker controller available for server %s on node %q during deep-sleep rollback: %w", serverID, nodeID, ctrlErr)
	} else {
		status, err := ctrl.GetContainerStatus(rollbackCtx, containerID)
		if err != nil {
			observeErr = fmt.Errorf("failed to observe container during deep-sleep rollback: %w", err)
		} else {
			switch status {
			case db.StatusRunning, db.StatusStarting, db.StatusUnhealthy:
				target = status
			case db.StatusStopped:
				target = db.StatusDeepSleep
			default:
				observeErr = fmt.Errorf("container reported unexpected status %s during deep-sleep rollback", status)
			}
		}
	}

	var stateErr error
	if m.store != nil {
		stateErr = m.store.UpdateServerStatus(rollbackCtx, serverID, target)
		if stateErr != nil && target != db.StatusError {
			stateErr = errors.Join(stateErr, m.store.UpdateServerStatus(rollbackCtx, serverID, db.StatusError))
		}
		if stateErr != nil && m.logger != nil {
			m.logger.Error("Deep sleep: failed to persist rollback state for server %s: %v", serverID, stateErr)
		}
	}
	if m.refresher != nil {
		m.refresher(serverID)
	}
	return errors.Join(observeErr, stateErr)
}

func (m *DeepSleepManager) rollbackWake(ctx context.Context, serverID string) {
	failCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if m.store != nil {
		if err := m.store.UpdateServerStatus(failCtx, serverID, db.StatusError); err != nil && m.logger != nil {
			m.logger.Error("Wake: failed to update status to error for server %s: %v", serverID, err)
		}
	}
	if m.refresher != nil {
		m.refresher(serverID)
	}
}

// rollbackWakePersistenceFailure restores the stopped state that backs the
// persisted deep-sleep intent when Docker started successfully but the RUNNING
// write failed. If Docker cannot be confirmed stopped, persist ERROR instead.
func (m *DeepSleepManager) rollbackWakePersistenceFailure(ctx context.Context, ctrl SleepController, serverID, containerID string) error {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	var stopErr error
	if ctrl == nil {
		stopErr = fmt.Errorf("no docker controller available during wake persistence failure rollback")
	} else {
		found, err := ctrl.StopContainer(rollbackCtx, containerID)
		if err == nil && found {
			status, statusErr := ctrl.GetContainerStatus(rollbackCtx, containerID)
			if statusErr == nil && status == db.StatusStopped {
				if m.refresher != nil {
					m.refresher(serverID)
				}
				return nil
			}
			if statusErr != nil {
				stopErr = fmt.Errorf("failed to verify stopped container: %w", statusErr)
			} else {
				stopErr = fmt.Errorf("container reported %s after stop", status)
			}
		} else if err != nil {
			stopErr = err
		} else {
			stopErr = fmt.Errorf("container disappeared before stopped state could be confirmed")
		}
	}

	var stateErr error
	if m.store != nil {
		stateErr = m.store.UpdateServerStatus(rollbackCtx, serverID, db.StatusError)
		if stateErr != nil && m.logger != nil {
			m.logger.Error("Wake: failed to persist error state after start rollback failed for server %s: %v", serverID, stateErr)
		}
	}
	if m.refresher != nil {
		m.refresher(serverID)
	}
	return errors.Join(fmt.Errorf("failed to stop container after wake status persistence failure: %w", stopErr), stateErr)
}

// SleepServer gracefully stops a server container and marks it deeply asleep.
// The status is pre-set to StatusDeepSleep and proxy route updated before stopping
// the container, so the reconciler never misclassifies the container exit as an
// unexpected stop or clobbers proxy routes (MINE-121, MINE-172).
func (m *DeepSleepManager) SleepServer(ctx context.Context, serverID string) error {
	if m.store == nil {
		return fmt.Errorf("store is nil")
	}
	if !m.tryBeginTransition(serverID, "sleep") {
		return fmt.Errorf("sleep already in flight for server %s", serverID)
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
		return fmt.Errorf("cannot deep-sleep server %s in status %s", serverID, srv.Status)
	}

	ctrl, err := m.getController(srv.NodeID)
	if err != nil {
		return fmt.Errorf("cannot deep-sleep server %s on node %q: %w", serverID, srv.NodeID, err)
	}

	// 1. Pre-set status to db.StatusDeepSleep and refresh routes before stopping the container.
	// This ensures that when the JVM exits and Docker emits the container die/stop event,
	// the Reconciler observes desired == StatusDeepSleep and never clobbers the status
	// to StatusStopped or tears down the proxy routes (MINE-121, MINE-172).
	if err := m.store.UpdateServerStatus(ctx, serverID, db.StatusDeepSleep); err != nil {
		return fmt.Errorf("failed to pre-set server status to deepsleep: %w", err)
	}
	if m.refresher != nil {
		m.refresher(serverID)
	}

	// Docker's on-failure policy would bring the JVM back after the stop.
	// Pin restart to "no" before the stop so a surprise start is not a wake.
	if err := ctrl.SetRestartPolicy(ctx, srv.ContainerID, container.RestartPolicy{Name: "no"}); err != nil {
		rollbackErr := m.rollbackSleep(ctx, serverID, srv.ContainerID)
		return errors.Join(fmt.Errorf("failed to disable restart policy for server %s: %w", serverID, err), rollbackErr)
	}

	// 2. Flush world and ask the JVM to exit cleanly via RCON. Best effort:
	// without RCON the docker stop backstop below still applies.
	if m.commander != nil {
		if _, err := m.commander.SendCommand(ctx, serverID, "save-all"); err != nil {
			if m.logger != nil {
				m.logger.Warn("Deep sleep: save-all failed for server %s: %v", srv.Name, err)
			}
		}
		if _, err := m.commander.SendCommand(ctx, serverID, "stop"); err != nil {
			if m.logger != nil {
				m.logger.Warn("Deep sleep: RCON stop failed for server %s: %v", srv.Name, err)
			}
		}
	}

	// 3. Wait for natural exit (session.lock released by the JVM itself).
	deadline := time.Now().Add(deepSleepStopTimeout)
	for {
		status, err := ctrl.GetContainerStatus(ctx, srv.ContainerID)
		if err != nil {
			rollbackErr := m.rollbackSleep(ctx, serverID, srv.ContainerID)
			return errors.Join(fmt.Errorf("failed to verify container state while deep-sleeping server %s: %w", serverID, err), rollbackErr)
		}
		if status == db.StatusStopped {
			break
		}
		if status != db.StatusRunning {
			rollbackErr := m.rollbackSleep(ctx, serverID, srv.ContainerID)
			return errors.Join(fmt.Errorf("cannot deep-sleep server %s while container is in status %s", serverID, status), rollbackErr)
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			// The container is still alive, so the pre-set DeepSleep status is
			// now a lie. Undo it or the server is wedged: reconciler refuses to
			// heal a DeepSleep server and the proxy route stays down, so nothing
			// can bring it back without a manual edit (MINE-172).
			rollbackErr := m.rollbackSleep(ctx, serverID, srv.ContainerID)
			return errors.Join(ctx.Err(), rollbackErr)
		case <-time.After(deepSleepStopPollInterval):
		}
	}

	// 4. Backstop: force the container down if it is still alive.
	if status, err := ctrl.GetContainerStatus(ctx, srv.ContainerID); err != nil {
		rollbackErr := m.rollbackSleep(ctx, serverID, srv.ContainerID)
		return errors.Join(fmt.Errorf("failed to verify container state before deep-sleep stop for server %s: %w", serverID, err), rollbackErr)
	} else if status == db.StatusRunning {
		if m.logger != nil {
			m.logger.Warn("Deep sleep: server %s did not exit on RCON stop, forcing container stop", srv.Name)
		}
		found, err := ctrl.StopContainer(ctx, srv.ContainerID)
		if err != nil {
			rollbackErr := m.rollbackSleep(ctx, serverID, srv.ContainerID)
			return errors.Join(fmt.Errorf("container stop failed for server %s: %w", serverID, err), rollbackErr)
		}
		if !found {
			rollbackErr := m.rollbackSleep(ctx, serverID, srv.ContainerID)
			return errors.Join(fmt.Errorf("container for server %s disappeared before deep-sleep stop completed", serverID), rollbackErr)
		}
	} else if status != db.StatusStopped {
		rollbackErr := m.rollbackSleep(ctx, serverID, srv.ContainerID)
		return errors.Join(fmt.Errorf("cannot confirm deep sleep for server %s in container status %s", serverID, status), rollbackErr)
	}

	m.idle.RecordActivity(serverID)

	if m.refresher != nil {
		m.refresher(serverID)
	}

	if m.logger != nil {
		m.logger.Info("Server %s (%s) is now deeply asleep (zero RAM/CPU)", srv.Name, serverID)
	}

	return nil
}

// refuseActiveOperation stops a wake or unfreeze when a journal row still owns
// the world. Stores that do not record a journal, including test doubles, are
// treated as unlocked.
func refuseActiveOperation(ctx context.Context, store any, serverID string) error {
	type operationGuard interface {
		AssertNoServerOperation(ctx context.Context, serverID string) error
	}
	guard, ok := store.(operationGuard)
	if !ok {
		return nil
	}
	if err := guard.AssertNoServerOperation(ctx, serverID); err != nil {
		return fmt.Errorf("operation owns server %s: %w", serverID, err)
	}
	return nil
}

// WakeServer boots a deeply asleep server. Concurrent callers wait for the
// current lifecycle transition, then re-read state; a completed wake is
// idempotent while an in-flight sleep is followed by a wake.
func (m *DeepSleepManager) WakeServer(ctx context.Context, serverID string) error {
	if m.store == nil {
		return fmt.Errorf("store is nil")
	}
	for !m.tryBeginTransition(serverID, "wake") {
		if err := WaitForServerTransition(ctx, serverID); err != nil {
			return fmt.Errorf("waiting for existing server transition: %w", err)
		}
	}
	defer m.endTransition(serverID)

	srv, err := m.store.GetServer(ctx, serverID)
	if err != nil {
		return fmt.Errorf("server %s not found: %w", serverID, err)
	}
	if err := refuseActiveOperation(ctx, m.store, serverID); err != nil {
		return err
	}

	if srv.Status == db.StatusRunning {
		return nil
	}

	// Allow waking a server with AutoDeepSleep enabled whose persisted status
	// drifted to Stopped or Creating (e.g. if a down-route remained armed after
	// a status drift or failed transition, MINE-172).
	if srv.Status != db.StatusDeepSleep {
		if !srv.AutoDeepSleep || (srv.Status != db.StatusStopped && srv.Status != db.StatusCreating) {
			return fmt.Errorf("cannot wake server %s with non-deepsleep status %s", serverID, srv.Status)
		}
		if m.logger != nil {
			m.logger.Warn("Deep sleep: waking server %s (%s) from drifted status %s (auto_deepsleep=true)", srv.Name, serverID, srv.Status)
		}
	}

	m.mu.Lock()
	prepareWake := m.wakePreparer
	m.mu.Unlock()
	if prepareWake != nil {
		if err := prepareWake(ctx, srv); err != nil {
			m.rollbackWake(ctx, serverID)
			return fmt.Errorf("failed to prepare server %s for wake: %w", serverID, err)
		}
	}
	if srv.ContainerID == "" {
		m.rollbackWake(ctx, serverID)
		return fmt.Errorf("server %s has no container", serverID)
	}

	ctrl, err := m.getController(srv.NodeID)
	if err != nil {
		m.rollbackWake(ctx, serverID)
		return fmt.Errorf("cannot wake server %s on node %q: %w", serverID, srv.NodeID, err)
	}

	if err := ctrl.SetRestartPolicy(ctx, srv.ContainerID, DefaultRestartPolicy); err != nil {
		m.rollbackWake(ctx, serverID)
		return fmt.Errorf("failed to restore restart policy for server %s: %w", serverID, err)
	}

	start := time.Now()
	if err := ctrl.StartContainer(ctx, srv.ContainerID); err != nil {
		m.rollbackWake(ctx, serverID)
		return fmt.Errorf("container start failed for server %s: %w", serverID, err)
	}

	// Wait for the container to report running (Docker-level readiness).
	// JVM-level readiness is gated by the proxy's SLP health check before
	// the held client is relayed.
	deadline := time.Now().Add(deepSleepStartTimeout)
	for {
		status, err := ctrl.GetContainerStatus(ctx, srv.ContainerID)
		if err == nil && status == db.StatusRunning {
			break
		}
		// If the container stopped or failed immediately on start (e.g. JVM crash,
		// bad flags, missing jar), abort immediately instead of stalling for 120s.
		if err == nil && (status == db.StatusStopped || status == db.StatusError) {
			m.rollbackWake(ctx, serverID)
			return fmt.Errorf("container for server %s exited unexpectedly (%s) during wake", serverID, status)
		}
		if time.Now().After(deadline) {
			m.rollbackWake(ctx, serverID)
			return fmt.Errorf("container for server %s did not reach running in time", serverID)
		}
		select {
		case <-ctx.Done():
			m.rollbackWake(ctx, serverID)
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}

	srv.Status = db.StatusRunning
	now := time.Now()
	srv.LastStarted = &now
	if err := m.store.UpdateServer(ctx, srv); err != nil {
		persistErr := fmt.Errorf("failed to update server status to running: %w", err)
		rollbackErr := m.rollbackWakePersistenceFailure(ctx, ctrl, serverID, srv.ContainerID)
		return errors.Join(persistErr, rollbackErr)
	}

	m.idle.RecordActivity(serverID)

	if m.refresher != nil {
		m.refresher(serverID)
	}

	if m.logger != nil {
		m.logger.Info("Server %s (%s) woke from deep sleep in %v", srv.Name, serverID, time.Since(start))
	}

	return nil
}

// IsAsleep returns whether the server is currently deeply asleep.
func (m *DeepSleepManager) IsAsleep(ctx context.Context, serverID string) bool {
	srv, err := m.store.GetServer(ctx, serverID)
	if err != nil || srv == nil {
		return false
	}
	return srv.Status == db.StatusDeepSleep
}
