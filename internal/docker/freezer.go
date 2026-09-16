package docker

import (
	"context"
	"fmt"
	"sync"
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

// HibernationManager manages cgroup freezer sleep/wake lifecycle for idle Minecraft servers
type HibernationManager struct {
	freezer       ContainerFreezer
	store         FreezerStore
	logger        *logger.Logger
	idleTracker   map[string]time.Time
	trackerMu     sync.RWMutex
	wakeCallbacks []func(serverID string)
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
}

// NewHibernationManager creates a new HibernationManager
func NewHibernationManager(freezer ContainerFreezer, store FreezerStore, log *logger.Logger) *HibernationManager {
	return &HibernationManager{
		freezer:     freezer,
		store:       store,
		logger:      log,
		idleTracker: make(map[string]time.Time),
	}
}

// RegisterWakeCallback registers a callback fired when a server is woken
func (m *HibernationManager) RegisterWakeCallback(cb func(serverID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wakeCallbacks = append(m.wakeCallbacks, cb)
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
	m.trackerMu.Lock()
	defer m.trackerMu.Unlock()
	delete(m.idleTracker, serverID)
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

	now := time.Now()

	for _, srv := range servers {
		if !srv.AutoHibernate || srv.Status != db.StatusRunning || srv.ContainerID == "" {
			m.trackerMu.Lock()
			delete(m.idleTracker, srv.ID)
			m.trackerMu.Unlock()
			continue
		}

		// Only hibernate if no players are online
		if srv.PlayersOnline > 0 {
			m.trackerMu.Lock()
			delete(m.idleTracker, srv.ID)
			m.trackerMu.Unlock()
			continue
		}

		timeoutMinutes := srv.IdleTimeoutMinutes
		if timeoutMinutes <= 0 {
			timeoutMinutes = 10
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
				m.logger.Info("Server %s (%s) idle for %v, hibernating container %s via cgroup freezer...",
					srv.Name, srv.ID, now.Sub(idleSince), srv.ContainerID)
			}
			if err := m.HibernateServer(ctx, srv.ID); err != nil {
				if m.logger != nil {
					m.logger.Error("Failed to hibernate server %s: %v", srv.Name, err)
				}
			}
		}
	}

	return nil
}

// HibernateServer freezes a server container using cgroup freezer and updates status
func (m *HibernationManager) HibernateServer(ctx context.Context, serverID string) error {
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

	if err := m.store.UpdateServerStatus(ctx, serverID, db.StatusPaused); err != nil {
		return fmt.Errorf("failed to update server status to paused: %w", err)
	}

	m.trackerMu.Lock()
	delete(m.idleTracker, serverID)
	m.trackerMu.Unlock()

	if m.logger != nil {
		m.logger.Info("Successfully hibernated server %s (%s)", srv.Name, serverID)
	}

	return nil
}

// WakeServer unfreezes a server container using cgroup freezer and restores running status
func (m *HibernationManager) WakeServer(ctx context.Context, serverID string) error {
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
		return fmt.Errorf("cgroup unfreeze failed for server %s: %w", serverID, err)
	}

	if err := m.store.UpdateServerStatus(ctx, serverID, db.StatusRunning); err != nil {
		return fmt.Errorf("failed to update server status to running: %w", err)
	}

	m.trackerMu.Lock()
	delete(m.idleTracker, serverID)
	m.trackerMu.Unlock()

	if m.logger != nil {
		m.logger.Info("Successfully woke server %s (%s) from hibernation in %v", srv.Name, serverID, time.Since(start))
	}

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
