package docker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// SleeperStore defines storage operations needed by DeepSleepManager.
type SleeperStore interface {
	GetServer(ctx context.Context, id string) (*db.Server, error)
	ListServers(ctx context.Context) ([]*db.Server, error)
	UpdateServerStatus(ctx context.Context, id string, status db.ServerStatus) error
}

// SleepController defines container start/stop/status capabilities for deep sleep.
type SleepController interface {
	StartContainer(ctx context.Context, containerID string) error
	// StopContainer stops a container, returning (found, err).
	StopContainer(ctx context.Context, containerID string) (bool, error)
	GetContainerStatus(ctx context.Context, containerID string) (db.ServerStatus, error)
}

// SleepCommander sends in-game console commands (RCON) for graceful shutdown.
type SleepCommander interface {
	SendCommand(ctx context.Context, serverID string, command string) (string, error)
}

// RouteRefresher re-resolves a server's proxy route after a transition.
type RouteRefresher func(serverID string)

// Deep sleep power timings.
const (
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
	controller  SleepController
	commander   SleepCommander
	store       SleeperStore
	refresher   RouteRefresher
	logger      *logger.Logger
	idleTracker map[string]time.Time
	trackerMu   sync.RWMutex
	mu          sync.Mutex
	playerCount func(serverID string) int
	inflight    map[string]struct{}
	inflightMu  sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewDeepSleepManager creates a new DeepSleepManager. commander and refresher
// may be nil (RCON flush and route refresh are then skipped).
func NewDeepSleepManager(controller SleepController, store SleeperStore, commander SleepCommander, refresher RouteRefresher, log *logger.Logger) *DeepSleepManager {
	return &DeepSleepManager{
		controller:  controller,
		commander:   commander,
		store:       store,
		refresher:   refresher,
		logger:      log,
		idleTracker: make(map[string]time.Time),
		inflight:    make(map[string]struct{}),
	}
}

// SetPlayerCountProvider configures the online-player source of truth (same
// rationale as HibernationManager: store rows never persist PlayersOnline).
func (m *DeepSleepManager) SetPlayerCountProvider(provider func(serverID string) int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.playerCount = provider
}

func (m *DeepSleepManager) onlinePlayers(srv *db.Server) int {
	m.mu.Lock()
	provider := m.playerCount
	m.mu.Unlock()
	if provider != nil {
		return provider(srv.ID)
	}
	return srv.PlayersOnline
}

func (m *DeepSleepManager) tryBeginTransition(serverID, op string) bool {
	m.inflightMu.Lock()
	defer m.inflightMu.Unlock()
	if _, busy := m.inflight[serverID]; busy {
		if m.logger != nil {
			m.logger.Debug("Deep sleep: skipping %s for server %s: transition already in flight", op, serverID)
		}
		return false
	}
	m.inflight[serverID] = struct{}{}
	return true
}

func (m *DeepSleepManager) endTransition(serverID string) {
	m.inflightMu.Lock()
	defer m.inflightMu.Unlock()
	delete(m.inflight, serverID)
}

// Start begins the background idle-checker routine.
func (m *DeepSleepManager) Start(ctx context.Context, checkInterval time.Duration) {
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
						m.logger.Warn("Error checking idle servers for deep sleep: %v", err)
					}
				}
			}
		}
	}()
}

// Stop terminates the background idle-checker routine.
func (m *DeepSleepManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
}

// RecordActivity marks a server as recently active, resetting its idle countdown.
func (m *DeepSleepManager) RecordActivity(serverID string) {
	m.trackerMu.Lock()
	defer m.trackerMu.Unlock()
	delete(m.idleTracker, serverID)
}

// CheckIdleServers evaluates all running deep-sleep-enabled servers and stops idle ones.
func (m *DeepSleepManager) CheckIdleServers(ctx context.Context) error {
	if m.store == nil || m.controller == nil {
		return nil
	}

	servers, err := m.store.ListServers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list servers: %w", err)
	}

	now := time.Now()

	for _, srv := range servers {
		if !srv.AutoDeepSleep || srv.Status != db.StatusRunning || srv.ContainerID == "" {
			m.trackerMu.Lock()
			delete(m.idleTracker, srv.ID)
			m.trackerMu.Unlock()
			continue
		}

		if m.onlinePlayers(srv) > 0 {
			m.trackerMu.Lock()
			delete(m.idleTracker, srv.ID)
			m.trackerMu.Unlock()
			continue
		}

		timeoutMinutes := srv.DeepSleepTimeoutMinutes
		if timeoutMinutes <= 0 {
			timeoutMinutes = 30
		}
		idleDuration := time.Duration(timeoutMinutes) * time.Minute

		m.trackerMu.Lock()
		idleSince, tracked := m.idleTracker[srv.ID]
		if !tracked {
			m.idleTracker[srv.ID] = now
			m.trackerMu.Unlock()
			continue
		}
		m.trackerMu.Unlock()

		if now.Sub(idleSince) >= idleDuration {
			if m.logger != nil {
				m.logger.Info("Server %s (%s) idle for %v, deep-sleeping container %s (full stop)...",
					srv.Name, srv.ID, now.Sub(idleSince), srv.ContainerID)
			}
			if err := m.SleepServer(ctx, srv.ID); err != nil {
				if m.logger != nil {
					m.logger.Error("Failed to deep-sleep server %s: %v", srv.Name, err)
				}
			}
		}
	}

	return nil
}

// SleepServer gracefully stops a server container and marks it deeply asleep.
// The container must exit (releasing session.lock) before the status flips,
// so the reconciler never observes a running container under a sleep status.
func (m *DeepSleepManager) SleepServer(ctx context.Context, serverID string) error {
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

	// 1. Flush world and ask the JVM to exit cleanly via RCON. Best effort:
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

	// 2. Wait for natural exit (session.lock released by the JVM itself).
	deadline := time.Now().Add(deepSleepStopTimeout)
	for {
		status, err := m.controller.GetContainerStatus(ctx, srv.ContainerID)
		if err != nil || status != db.StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(deepSleepStopPollInterval):
		}
	}

	// 3. Backstop: force the container down if it is still alive.
	if status, err := m.controller.GetContainerStatus(ctx, srv.ContainerID); err == nil && status == db.StatusRunning {
		if m.logger != nil {
			m.logger.Warn("Deep sleep: server %s did not exit on RCON stop, forcing container stop", srv.Name)
		}
		if _, err := m.controller.StopContainer(ctx, srv.ContainerID); err != nil {
			return fmt.Errorf("container stop failed for server %s: %w", serverID, err)
		}
	}

	if err := m.store.UpdateServerStatus(ctx, serverID, db.StatusDeepSleep); err != nil {
		return fmt.Errorf("failed to update server status to deepsleep: %w", err)
	}

	m.trackerMu.Lock()
	delete(m.idleTracker, serverID)
	m.trackerMu.Unlock()

	if m.refresher != nil {
		m.refresher(serverID)
	}

	if m.logger != nil {
		m.logger.Info("Server %s (%s) is now deeply asleep (zero RAM/CPU)", srv.Name, serverID)
	}

	return nil
}

// WakeServer boots a deeply asleep server. Idempotent: an already-running
// server, or a boot already in flight, returns nil so concurrent joiners
// share one boot instead of erroring (the proxy holds them meanwhile).
func (m *DeepSleepManager) WakeServer(ctx context.Context, serverID string) error {
	srv, err := m.store.GetServer(ctx, serverID)
	if err != nil {
		return fmt.Errorf("server %s not found: %w", serverID, err)
	}

	if srv.ContainerID == "" {
		return fmt.Errorf("server %s has no container", serverID)
	}

	if srv.Status == db.StatusRunning {
		return nil
	}

	if srv.Status != db.StatusDeepSleep {
		return fmt.Errorf("cannot wake server %s with non-deepsleep status %s", serverID, srv.Status)
	}

	if !m.tryBeginTransition(serverID, "wake") {
		return nil // boot already in flight; shared by concurrent joiners
	}
	defer m.endTransition(serverID)

	start := time.Now()
	if err := m.controller.StartContainer(ctx, srv.ContainerID); err != nil {
		return fmt.Errorf("container start failed for server %s: %w", serverID, err)
	}

	// Wait for the container to report running (Docker-level readiness).
	// JVM-level readiness is gated by the proxy's SLP health check before
	// the held client is relayed.
	deadline := time.Now().Add(deepSleepStartTimeout)
	for {
		status, err := m.controller.GetContainerStatus(ctx, srv.ContainerID)
		if err == nil && status == db.StatusRunning {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("container for server %s did not reach running in time", serverID)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}

	if err := m.store.UpdateServerStatus(ctx, serverID, db.StatusRunning); err != nil {
		return fmt.Errorf("failed to update server status to running: %w", err)
	}

	m.trackerMu.Lock()
	delete(m.idleTracker, serverID)
	m.trackerMu.Unlock()

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
