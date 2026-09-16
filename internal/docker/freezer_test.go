package docker

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockFreezer struct {
	mu           sync.Mutex
	paused       map[string]bool
	pauseCalls   []string
	unpauseCalls []string
}

func newMockFreezer() *mockFreezer {
	return &mockFreezer{
		paused: make(map[string]bool),
	}
}

func (m *mockFreezer) PauseContainer(ctx context.Context, containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused[containerID] = true
	m.pauseCalls = append(m.pauseCalls, containerID)
	return nil
}

func (m *mockFreezer) UnpauseContainer(ctx context.Context, containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused[containerID] = false
	m.unpauseCalls = append(m.unpauseCalls, containerID)
	return nil
}

func (m *mockFreezer) GetContainerStatus(ctx context.Context, containerID string) (db.ServerStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.paused[containerID] {
		return db.StatusPaused, nil
	}
	return db.StatusRunning, nil
}

type mockFreezerStore struct {
	mu      sync.Mutex
	servers map[string]*db.Server
}

func newMockFreezerStore() *mockFreezerStore {
	return &mockFreezerStore{
		servers: make(map[string]*db.Server),
	}
}

func (s *mockFreezerStore) GetServer(ctx context.Context, id string) (*db.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	srv, ok := s.servers[id]
	if !ok {
		return nil, fmt.Errorf("server not found")
	}
	// Return a copy
	srvCopy := *srv
	return &srvCopy, nil
}

func (s *mockFreezerStore) ListServers(ctx context.Context) ([]*db.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var list []*db.Server
	for _, srv := range s.servers {
		srvCopy := *srv
		list = append(list, &srvCopy)
	}
	return list, nil
}

func (s *mockFreezerStore) UpdateServer(ctx context.Context, server *db.Server) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	srvCopy := *server
	s.servers[server.ID] = &srvCopy
	return nil
}

func (s *mockFreezerStore) UpdateServerStatus(ctx context.Context, id string, status db.ServerStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	srv, ok := s.servers[id]
	if !ok {
		return fmt.Errorf("server not found")
	}
	srv.Status = status
	return nil
}

func TestHibernationManager_HibernateAndWake(t *testing.T) {
	ctx := context.Background()
	freezer := newMockFreezer()
	store := newMockFreezerStore()

	store.servers["srv-1"] = &db.Server{
		ID:            "srv-1",
		Name:          "Survival",
		ContainerID:   "cnt-1",
		Status:        db.StatusRunning,
		AutoHibernate: true,
	}

	mgr := NewHibernationManager(freezer, store, nil)

	var wokenServer string
	mgr.RegisterWakeCallback(func(serverID string) {
		wokenServer = serverID
	})

	// Test HibernateServer
	err := mgr.HibernateServer(ctx, "srv-1")
	require.NoError(t, err)

	assert.True(t, freezer.paused["cnt-1"])
	assert.Contains(t, freezer.pauseCalls, "cnt-1")

	srv, err := store.GetServer(ctx, "srv-1")
	require.NoError(t, err)
	assert.Equal(t, db.StatusPaused, srv.Status)
	assert.True(t, mgr.IsHibernated(ctx, "srv-1"))

	// Test WakeServer
	err = mgr.WakeServer(ctx, "srv-1")
	require.NoError(t, err)

	assert.False(t, freezer.paused["cnt-1"])
	assert.Contains(t, freezer.unpauseCalls, "cnt-1")

	srv, err = store.GetServer(ctx, "srv-1")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status)
	assert.False(t, mgr.IsHibernated(ctx, "srv-1"))
	assert.Equal(t, "srv-1", wokenServer)
}

func TestHibernationManager_CheckIdleServers(t *testing.T) {
	ctx := context.Background()
	freezer := newMockFreezer()
	store := newMockFreezerStore()

	store.servers["srv-active"] = &db.Server{
		ID:                 "srv-active",
		Name:               "Active Server",
		ContainerID:        "cnt-active",
		Status:             db.StatusRunning,
		AutoHibernate:      true,
		IdleTimeoutMinutes: 1,
		PlayersOnline:      2,
	}

	store.servers["srv-idle"] = &db.Server{
		ID:                 "srv-idle",
		Name:               "Idle Server",
		ContainerID:        "cnt-idle",
		Status:             db.StatusRunning,
		AutoHibernate:      true,
		IdleTimeoutMinutes: 1,
		PlayersOnline:      0,
	}

	mgr := NewHibernationManager(freezer, store, nil)

	// First pass: idle server is tracked
	err := mgr.CheckIdleServers(ctx)
	require.NoError(t, err)
	assert.Empty(t, freezer.pauseCalls)

	// Artificially age the idle tracker entry
	mgr.trackerMu.Lock()
	mgr.idleTracker["srv-idle"] = time.Now().Add(-2 * time.Minute)
	mgr.trackerMu.Unlock()

	// Second pass: idle server should be paused, active server should remain running
	err = mgr.CheckIdleServers(ctx)
	require.NoError(t, err)

	assert.Contains(t, freezer.pauseCalls, "cnt-idle")
	assert.NotContains(t, freezer.pauseCalls, "cnt-active")

	idleSrv, _ := store.GetServer(ctx, "srv-idle")
	assert.Equal(t, db.StatusPaused, idleSrv.Status)

	activeSrv, _ := store.GetServer(ctx, "srv-active")
	assert.Equal(t, db.StatusRunning, activeSrv.Status)
}
