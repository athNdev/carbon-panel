// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

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
	// pauseGate, when non-nil, blocks PauseContainer until closed —
	// lets tests hold a transition open to prove single-flighting.
	pauseGate  chan struct{}
	pauseErr   error
	pauseNoop  bool
	unpauseErr error
}

func newMockFreezer() *mockFreezer {
	return &mockFreezer{
		paused: make(map[string]bool),
	}
}

func (m *mockFreezer) PauseContainer(ctx context.Context, containerID string) error {
	if gate := m.pauseGate; gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pauseErr != nil {
		return m.pauseErr
	}
	if !m.pauseNoop {
		m.paused[containerID] = true
	}
	m.pauseCalls = append(m.pauseCalls, containerID)
	return nil
}

func (m *mockFreezer) UnpauseContainer(ctx context.Context, containerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.unpauseCalls = append(m.unpauseCalls, containerID)
	if m.unpauseErr != nil {
		return m.unpauseErr
	}
	m.paused[containerID] = false
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
	mu                 sync.Mutex
	servers            map[string]*db.Server
	updateStatusErr    error
	updateStatusErrFor db.ServerStatus
	updateServerErr    error
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
	if s.updateServerErr != nil {
		return s.updateServerErr
	}
	srvCopy := *server
	s.servers[server.ID] = &srvCopy
	return nil
}

func (s *mockFreezerStore) UpdateServerStatus(ctx context.Context, id string, status db.ServerStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.updateStatusErr != nil && (s.updateStatusErrFor == "" || s.updateStatusErrFor == status) {
		return s.updateStatusErr
	}
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

	assert.Equal(t, uint64(0), mgr.TotalHibernated())
	assert.Equal(t, uint64(0), mgr.TotalWoken())

	// Test HibernateServer
	err := mgr.HibernateServer(ctx, "srv-1")
	require.NoError(t, err)

	assert.Equal(t, uint64(1), mgr.TotalHibernated())
	assert.True(t, freezer.paused["cnt-1"])
	assert.Contains(t, freezer.pauseCalls, "cnt-1")

	srv, err := store.GetServer(ctx, "srv-1")
	require.NoError(t, err)
	assert.Equal(t, db.StatusPaused, srv.Status)
	assert.True(t, mgr.IsHibernated(ctx, "srv-1"))

	// Test WakeServer
	err = mgr.WakeServer(ctx, "srv-1")
	require.NoError(t, err)

	assert.Equal(t, uint64(1), mgr.TotalWoken())

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
	mgr.idle.markIdleSince("srv-idle", time.Now().Add(-2*time.Minute))

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

func TestHibernationManager_PlayerCountProvider(t *testing.T) {
	// Store rows never persist PlayersOnline (gorm:"-"), so without a
	// provider every server looks idle. The provider is authoritative:
	// a server the provider reports players for must not hibernate (MINE-120).
	ctx := context.Background()
	freezer := newMockFreezer()
	store := newMockFreezerStore()

	store.servers["srv-busy"] = &db.Server{
		ID:                 "srv-busy",
		Name:               "Busy Server",
		ContainerID:        "cnt-busy",
		Status:             db.StatusRunning,
		AutoHibernate:      true,
		IdleTimeoutMinutes: 1,
		PlayersOnline:      0, // what the store row always reports
	}

	mgr := NewHibernationManager(freezer, store, nil)
	mgr.SetPlayerCountProvider(func(serverID string) int {
		if serverID == "srv-busy" {
			return 3 // metrics collector sees players
		}
		return 0
	})

	require.NoError(t, mgr.CheckIdleServers(ctx))
	mgr.idle.markIdleSince("srv-busy", time.Now().Add(-2*time.Minute))

	require.NoError(t, mgr.CheckIdleServers(ctx))
	assert.NotContains(t, freezer.pauseCalls, "cnt-busy")

	srv, err := store.GetServer(ctx, "srv-busy")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status)
}

func TestHibernationManager_SingleFlight(t *testing.T) {
	// A hibernate racing an in-flight hibernate for the same server must be
	// rejected instead of double-pausing; the container freezes exactly once
	// (MINE-120). The gate holds the first transition open so the race is
	// deterministic.
	ctx := context.Background()
	freezer := newMockFreezer()
	freezer.pauseGate = make(chan struct{})
	store := newMockFreezerStore()

	store.servers["srv-race"] = &db.Server{
		ID:            "srv-race",
		Name:          "Racy Server",
		ContainerID:   "cnt-race",
		Status:        db.StatusRunning,
		AutoHibernate: true,
	}

	mgr := NewHibernationManager(freezer, store, nil)

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- mgr.HibernateServer(ctx, "srv-race")
	}()

	// Wait until the first transition holds the in-flight slot.
	deadline := time.Now().Add(5 * time.Second)
	for !mgr.idle.transitioning("srv-race") {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for first hibernate to go in-flight")
		}
		time.Sleep(5 * time.Millisecond)
	}

	err := mgr.HibernateServer(ctx, "srv-race")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in flight")

	close(freezer.pauseGate)
	require.NoError(t, <-firstDone)

	freezer.mu.Lock()
	defer freezer.mu.Unlock()
	assert.Len(t, freezer.pauseCalls, 1, "container must be paused exactly once")
}

func TestHibernationManager_WakeUnpauseFailure_MarksError(t *testing.T) {
	ctx := context.Background()
	freezer := newMockFreezer()
	store := newMockFreezerStore()

	store.servers["srv-1"] = &db.Server{
		ID:          "srv-1",
		Name:        "Test Server",
		ContainerID: "cnt-1",
		Status:      db.StatusPaused,
	}
	freezer.paused["cnt-1"] = true

	mgr := NewHibernationManager(freezer, store, nil)

	freezer.mu.Lock()
	freezer.unpauseErr = fmt.Errorf("cgroup unfreeze device busy")
	freezer.mu.Unlock()

	err := mgr.WakeServer(ctx, "srv-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cgroup unfreeze failed")

	srv, err := store.GetServer(ctx, "srv-1")
	require.NoError(t, err)
	assert.Equal(t, db.StatusError, srv.Status, "server status must be marked error when unfreeze fails")
}

func TestHibernationManager_HibernateUpdateStatusFails_RollsBackUnpause(t *testing.T) {
	ctx := context.Background()
	freezer := newMockFreezer()
	store := newMockFreezerStore()

	store.servers["srv-1"] = &db.Server{
		ID:          "srv-1",
		Name:        "Test Server",
		ContainerID: "cnt-1",
		Status:      db.StatusRunning,
	}

	mgr := NewHibernationManager(freezer, store, nil)

	store.mu.Lock()
	store.updateStatusErr = fmt.Errorf("database transaction error")
	store.mu.Unlock()

	err := mgr.HibernateServer(ctx, "srv-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update server status to paused")

	freezer.mu.Lock()
	defer freezer.mu.Unlock()
	assert.Contains(t, freezer.unpauseCalls, "cnt-1", "must roll back and unpause container if status update fails")
}

func TestHibernationManager_HibernateUpdateStatusAndRollbackFail_ReturnsBothErrors(t *testing.T) {
	ctx := context.Background()
	freezer := newMockFreezer()
	store := newMockFreezerStore()
	store.servers["srv-1"] = &db.Server{ID: "srv-1", ContainerID: "cnt-1", Status: db.StatusRunning}
	store.updateStatusErr = fmt.Errorf("database transaction error")
	freezer.unpauseErr = fmt.Errorf("cgroup unfreeze device busy")

	mgr := NewHibernationManager(freezer, store, nil)
	err := mgr.HibernateServer(ctx, "srv-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database transaction error")
	assert.Contains(t, err.Error(), "cgroup unfreeze device busy", "rollback failure must not be discarded")

	freezer.mu.Lock()
	assert.True(t, freezer.paused["cnt-1"], "failed compensation leaves the container frozen and must be reported")
	freezer.mu.Unlock()
}

func TestHibernationManager_DoesNotPersistPausedWhenContainerDidNotFreeze(t *testing.T) {
	ctx := context.Background()
	freezer := newMockFreezer()
	freezer.pauseNoop = true
	store := newMockFreezerStore()
	store.servers["srv-1"] = &db.Server{ID: "srv-1", ContainerID: "cnt-1", Status: db.StatusRunning}

	mgr := NewHibernationManager(freezer, store, nil)
	err := mgr.HibernateServer(ctx, "srv-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not enter paused state")

	srv, err := store.GetServer(ctx, "srv-1")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status, "must not claim hibernation without observed cgroup freeze")
	freezer.mu.Lock()
	assert.Contains(t, freezer.unpauseCalls, "cnt-1", "compensating unpause should establish a known running state")
	freezer.mu.Unlock()
}

func TestHibernationManager_WakeStatusUpdateFails_RepausesContainer(t *testing.T) {
	ctx := context.Background()
	freezer := newMockFreezer()
	freezer.paused["cnt-1"] = true
	store := newMockFreezerStore()
	store.servers["srv-1"] = &db.Server{ID: "srv-1", ContainerID: "cnt-1", Status: db.StatusPaused}
	store.updateStatusErr = fmt.Errorf("database transaction error")

	mgr := NewHibernationManager(freezer, store, nil)
	err := mgr.WakeServer(ctx, "srv-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database transaction error")

	freezer.mu.Lock()
	assert.True(t, freezer.paused["cnt-1"], "container must be re-paused when the DB cannot persist the running state")
	freezer.mu.Unlock()
	srv, err := store.GetServer(ctx, "srv-1")
	require.NoError(t, err)
	assert.Equal(t, db.StatusPaused, srv.Status, "DB and cgroup state must remain aligned after compensation")
}

func TestHibernationManager_WakeUnpauseAndStatusPersistenceFail_ReportsBothErrors(t *testing.T) {
	ctx := context.Background()
	freezer := newMockFreezer()
	freezer.paused["cnt-1"] = true
	freezer.unpauseErr = fmt.Errorf("cgroup unfreeze device busy")
	store := newMockFreezerStore()
	store.servers["srv-1"] = &db.Server{ID: "srv-1", ContainerID: "cnt-1", Status: db.StatusPaused}
	store.updateStatusErr = fmt.Errorf("database transaction error")

	mgr := NewHibernationManager(freezer, store, nil)
	err := mgr.WakeServer(ctx, "srv-1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cgroup unfreeze device busy")
	assert.Contains(t, err.Error(), "database transaction error", "status persistence failure must not be discarded")
}
