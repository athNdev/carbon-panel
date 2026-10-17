package docker

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/events"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
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
	bus           *events.Bus
	idleTracker   map[string]time.Time
	trackerMu     sync.RWMutex
	wakeCallbacks []func(serverID string)
	mu            sync.Mutex
	ctx           context.Context
	cancel        context.CancelFunc
	// playerCount, when set, is the source of truth for online players
	// (MINE-120). Server rows from the store always report PlayersOnline==0
	// (gorm:"-"), so without a provider every server looks idle. Production
	// wires this to the metrics collector.
	playerCount func(serverID string) int
	// inflight guards against concurrent hibernate/wake transitions for the
	// same server (e.g. a wake racing the idle loop's hibernate).
	inflight   map[string]struct{}
	inflightMu sync.Mutex
}

// NewHibernationManager creates a new HibernationManager
func NewHibernationManager(freezer ContainerFreezer, store FreezerStore, log *logger.Logger) *HibernationManager {
	return &HibernationManager{
		freezer:     freezer,
		store:       store,
		logger:      log,
		idleTracker: make(map[string]time.Time),
		inflight:    make(map[string]struct{}),
	}
}

// SetEventBus configures the event bus for broadcasting hibernation lifecycle events.
func (m *HibernationManager) SetEventBus(bus *events.Bus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bus = bus
}

// SetPlayerCountProvider configures the online-player source of truth used
// by CheckIdleServers instead of the (never persisted) row field (MINE-120).
func (m *HibernationManager) SetPlayerCountProvider(provider func(serverID string) int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.playerCount = provider
}

// onlinePlayers returns the authoritative online-player count for a server.
func (m *HibernationManager) onlinePlayers(srv *db.Server) int {
	m.mu.Lock()
	provider := m.playerCount
	m.mu.Unlock()
	if provider != nil {
		return provider(srv.ID)
	}
	return srv.PlayersOnline
}

// tryBeginTransition single-flights hibernate/wake per server: returns false
// (and logs) if a transition for serverID is already running.
func (m *HibernationManager) tryBeginTransition(serverID, op string) bool {
	m.inflightMu.Lock()
	defer m.inflightMu.Unlock()
	if _, busy := m.inflight[serverID]; busy {
		if m.logger != nil {
			m.logger.Debug("Skipping %s for server %s: transition already in flight", op, serverID)
		}
		return false
	}
	m.inflight[serverID] = struct{}{}
	return true
}

func (m *HibernationManager) endTransition(serverID string) {
	m.inflightMu.Lock()
	defer m.inflightMu.Unlock()
	delete(m.inflight, serverID)
}

// Start starts the background idle checker loop
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
						m.logger.Error("Error checking idle servers: %v", err)
					}
				}
			}
		}
	}()

	if m.logger != nil {
		m.logger.Info("Hibernation manager started with check interval %v", checkInterval)
	}
}

// Stop stops the background idle checker loop
func (m *HibernationManager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.logger != nil {
		m.logger.Info("Hibernation manager stopped")
	}
}

// RegisterWakeCallback registers a callback to be called when a server wakes from hibernation
func (m *HibernationManager) RegisterWakeCallback(cb func(serverID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.wakeCallbacks = append(m.wakeCallbacks, cb)
}

// RecordActivity records activity for a server, resetting its idle timer
func (m *HibernationManager) RecordActivity(serverID string) {
	m.trackerMu.Lock()
	defer m.trackerMu.Unlock()
	m.idleTracker[serverID] = time.Now()
}

// CheckIdleServers checks all servers and hibernates any that have been idle longer than their threshold
func (m *HibernationManager) CheckIdleServers(ctx context.Context) error {
	servers, err := m.store.ListServers(ctx)
	if err != nil {
		return fmt.Errorf("failed to list servers: %w", err)
	}

	now := time.Now()
	for _, srv := range servers {
		if !srv.AutoHibernate {
			// Clean up tracker if auto-hibernate is disabled
			m.trackerMu.Lock()
			delete(m.idleTracker, srv.ID)
			m.trackerMu.Unlock()
			continue
		}

		// Only running servers can be hibernated
		if srv.Status != db.StatusRunning {
			m.trackerMu.Lock()
			delete(m.idleTracker, srv.ID)
			m.trackerMu.Unlock()
			continue
		}

		timeoutMin := srv.IdleTimeoutMinutes
		if timeoutMin <= 0 {
			timeoutMin = 10 // default 10 minutes
		}
		idleThreshold := time.Duration(timeoutMin) * time.Minute

		// If server has online players (from provider, fallback to row), reset idle timer
		if m.onlinePlayers(srv) > 0 {
			m.trackerMu.Lock()
			m.idleTracker[srv.ID] = now
			m.trackerMu.Unlock()
			continue
		}

		m.trackerMu.Lock()
		idleSince, tracked := m.idleTracker[srv.ID]
		if !tracked {
			// First time seeing 0 players, start tracking
			m.idleTracker[srv.ID] = now
			m.trackerMu.Unlock()
			continue
		}
		m.trackerMu.Unlock()

		if now.Sub(idleSince) >= idleThreshold {
			if m.logger != nil {
				m.logger.Info("Server %s (%s) has been idle for %v, hibernating container %s...",
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

// HibernateServer pauses a server container using cgroup freezer and marks it as paused
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
		return fmt.Errorf("cannot hibernate server %s with status %s (must be running)", serverID, srv.Status)
	}

	if err := m.freezer.PauseContainer(ctx, srv.ContainerID); err != nil {
		return fmt.Errorf("cgroup freeze failed for server %s: %w", serverID, err)
	}

	if err := m.store.UpdateServerStatus(ctx, serverID, db.StatusPaused); err != nil {
		// Try to unpause if DB update fails to avoid inconsistent state
		_ = m.freezer.UnpauseContainer(ctx, srv.ContainerID)
		return fmt.Errorf("failed to update server status to paused: %w", err)
	}

	m.trackerMu.Lock()
	delete(m.idleTracker, serverID)
	m.trackerMu.Unlock()

	// Emit lifecycle event
	m.mu.Lock()
	bus := m.bus
	m.mu.Unlock()
	if bus != nil {
		bus.Emit(ctx, events.Event{
			Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP,
			ServerID: serverID,
			Data:     map[string]any{"status": string(db.StatusPaused)},
		})
	}

	if m.logger != nil {
		m.logger.Info("Successfully hibernated server %s (%s)", srv.Name, serverID)
	}

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
		return fmt.Errorf("cgroup unfreeze failed for server %s: %w", serverID, err)
	}

	if err := m.store.UpdateServerStatus(ctx, serverID, db.StatusRunning); err != nil {
		return fmt.Errorf("failed to update server status to running: %w", err)
	}

	m.trackerMu.Lock()
	delete(m.idleTracker, serverID)
	m.trackerMu.Unlock()

	// Emit lifecycle event
	m.mu.Lock()
	bus := m.bus
	m.mu.Unlock()
	if bus != nil {
		bus.Emit(ctx, events.Event{
			Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START,
			ServerID: serverID,
			Data:     map[string]any{"status": string(db.StatusRunning)},
		})
	}

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
