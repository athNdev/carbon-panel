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

type mockSleepController struct {
	mu        sync.Mutex
	status    map[string]db.ServerStatus
	starts    []string
	stops     []string
	startGate chan struct{}
	startErr  error
	stopErr   error
	startHook func(containerID string)
}

func newMockSleepController() *mockSleepController {
	return &mockSleepController{status: make(map[string]db.ServerStatus)}
}

func (m *mockSleepController) StartContainer(ctx context.Context, containerID string) error {
	if gate := m.startGate; gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.startErr != nil {
		return m.startErr
	}
	if m.startHook != nil {
		m.startHook(containerID)
		m.starts = append(m.starts, containerID)
		return nil
	}
	m.status[containerID] = db.StatusRunning
	m.starts = append(m.starts, containerID)
	return nil
}

func (m *mockSleepController) StopContainer(ctx context.Context, containerID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopErr != nil {
		return false, m.stopErr
	}
	if _, ok := m.status[containerID]; !ok {
		return false, nil
	}
	m.status[containerID] = db.StatusStopped
	m.stops = append(m.stops, containerID)
	return true, nil
}

func (m *mockSleepController) GetContainerStatus(ctx context.Context, containerID string) (db.ServerStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.status[containerID]
	if !ok {
		return "", fmt.Errorf("no such container")
	}
	return st, nil
}

type mockSleepCommander struct {
	mu        sync.Mutex
	commands  []string
	onCommand func(serverID, command string)
}

type blockingSleepCommander struct {
	delegate *mockSleepCommander
	entered  chan struct{}
	release  chan struct{}
}

func (c *blockingSleepCommander) SendCommand(ctx context.Context, serverID string, command string) (string, error) {
	if command == "save-all" {
		close(c.entered)
		select {
		case <-c.release:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return c.delegate.SendCommand(ctx, serverID, command)
}

func (m *mockSleepCommander) SendCommand(ctx context.Context, serverID string, command string) (string, error) {
	m.mu.Lock()
	m.commands = append(m.commands, serverID+":"+command)
	cb := m.onCommand
	m.mu.Unlock()
	if cb != nil {
		cb(serverID, command)
	}
	return "", nil
}

func newDeepSleepHarness() (*mockSleepController, *mockSleepCommander, *mockFreezerStore) {
	controller := newMockSleepController()
	controller.status["cnt-sleep"] = db.StatusRunning
	store := newMockFreezerStore()
	store.servers["srv-sleep"] = &db.Server{
		ID:                      "srv-sleep",
		Name:                    "Sleepy Server",
		ContainerID:             "cnt-sleep",
		Status:                  db.StatusRunning,
		AutoDeepSleep:           true,
		DeepSleepTimeoutMinutes: 1,
	}
	// Simulate the JVM exiting on RCON stop (releases session.lock).
	commander := &mockSleepCommander{}
	commander.onCommand = func(serverID, command string) {
		if command != "stop" {
			return
		}
		store.mu.Lock()
		srv, ok := store.servers[serverID]
		store.mu.Unlock()
		if !ok {
			return
		}
		controller.mu.Lock()
		controller.status[srv.ContainerID] = db.StatusStopped
		controller.mu.Unlock()
	}
	return controller, commander, store
}

func TestDeepSleepManager_SleepAndWake(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()

	var refreshed []string
	mgr := NewDeepSleepManager(controller, store, commander, func(id string) {
		refreshed = append(refreshed, id)
	}, nil)

	// Idle past timeout → graceful stop + deepsleep status.
	require.NoError(t, mgr.CheckIdleServers(ctx))
	mgr.trackerMu.Lock()
	mgr.idleTracker["srv-sleep"] = time.Now().Add(-2 * time.Minute)
	mgr.trackerMu.Unlock()
	require.NoError(t, mgr.CheckIdleServers(ctx))

	commander.mu.Lock()
	assert.Contains(t, commander.commands, "srv-sleep:save-all")
	assert.Contains(t, commander.commands, "srv-sleep:stop")
	commander.mu.Unlock()

	controller.mu.Lock()
	assert.Empty(t, controller.stops, "graceful RCON stop must exit before the force-stop backstop")
	controller.mu.Unlock()

	srv, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusDeepSleep, srv.Status)
	assert.True(t, mgr.IsAsleep(ctx, "srv-sleep"))
	assert.Contains(t, refreshed, "srv-sleep")

	// Wake boots the container and restores running.
	require.NoError(t, mgr.WakeServer(ctx, "srv-sleep"))
	controller.mu.Lock()
	assert.Contains(t, controller.starts, "cnt-sleep")
	controller.mu.Unlock()

	srv, err = store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status)
	assert.False(t, mgr.IsAsleep(ctx, "srv-sleep"))

	// Waking a running server is a no-op (shared boot, no error).
	require.NoError(t, mgr.WakeServer(ctx, "srv-sleep"))
}

func TestDeepSleepManager_SleepRollbackPersistenceFailureMarksError(t *testing.T) {
	controller, commander, store := newDeepSleepHarness()
	ctx, cancel := context.WithCancel(context.Background())
	commander.mu.Lock()
	commander.onCommand = func(_, command string) {
		if command == "stop" {
			cancel()
		}
	}
	commander.mu.Unlock()

	store.mu.Lock()
	store.updateStatusErr = fmt.Errorf("injected running status persistence failure")
	store.updateStatusErrFor = db.StatusRunning
	store.mu.Unlock()

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	err := mgr.SleepServer(ctx, "srv-sleep")
	require.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "injected running status persistence failure")

	controller.mu.Lock()
	containerStatus := controller.status["cnt-sleep"]
	controller.mu.Unlock()
	assert.Equal(t, db.StatusRunning, containerStatus)

	server, err := store.GetServer(context.Background(), "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusError, server.Status, "failed rollback persistence must not leave DeepSleep recorded over a running container")
}

func TestDeepSleepManager_PreparesReplacementBeforeWakeAndRecordsStart(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()
	store.servers["srv-sleep"].Status = db.StatusDeepSleep

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	mgr.SetWakePreparer(func(ctx context.Context, server *db.Server) error {
		server.ContainerID = "replacement-container"
		return store.UpdateServer(ctx, server)
	})

	require.NoError(t, mgr.WakeServer(ctx, "srv-sleep"))
	controller.mu.Lock()
	starts := append([]string(nil), controller.starts...)
	controller.mu.Unlock()
	assert.Equal(t, []string{"replacement-container"}, starts)

	server, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, "replacement-container", server.ContainerID)
	assert.Equal(t, db.StatusRunning, server.Status)
	assert.NotNil(t, server.LastStarted)
}

func TestDeepSleepManager_PlayersBlockSleep(t *testing.T) {
	// Authoritative player count wins over the always-zero store row.
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	mgr.SetPlayerCountProvider(func(serverID string) int { return 2 })

	require.NoError(t, mgr.CheckIdleServers(ctx))
	mgr.trackerMu.Lock()
	mgr.idleTracker["srv-sleep"] = time.Now().Add(-2 * time.Minute)
	mgr.trackerMu.Unlock()
	require.NoError(t, mgr.CheckIdleServers(ctx))

	srv, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status)
	controller.mu.Lock()
	defer controller.mu.Unlock()
	assert.Empty(t, controller.stops)
}

func TestDeepSleepManager_WakeSingleFlight(t *testing.T) {
	// Concurrent wakes wait for one boot to finish; the container starts once.
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()
	controller.startGate = make(chan struct{})

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	require.NoError(t, mgr.SleepServer(ctx, "srv-sleep"))

	firstDone := make(chan error, 1)
	go func() {
		firstDone <- mgr.WakeServer(ctx, "srv-sleep")
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		mgr.inflightMu.Lock()
		_, busy := mgr.inflight["srv-sleep"]
		mgr.inflightMu.Unlock()
		if busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for wake to go in-flight")
		}
		time.Sleep(5 * time.Millisecond)
	}

	secondDone := make(chan error, 1)
	go func() {
		secondDone <- mgr.WakeServer(ctx, "srv-sleep")
	}()
	secondReturnedEarly := false
	select {
	case <-secondDone:
		secondReturnedEarly = true
	case <-time.After(50 * time.Millisecond):
	}

	close(controller.startGate)
	require.NoError(t, <-firstDone)
	if secondReturnedEarly {
		t.Fatal("concurrent wake returned before the in-flight boot completed")
	}
	require.NoError(t, <-secondDone)

	controller.mu.Lock()
	defer controller.mu.Unlock()
	assert.Len(t, controller.starts, 1, "container must start exactly once")
}

func TestDeepSleepManager_WakeWaitsForConcurrentSleep(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()
	blockingCommander := &blockingSleepCommander{
		delegate: commander,
		entered:  make(chan struct{}),
		release:  make(chan struct{}),
	}
	mgr := NewDeepSleepManager(controller, store, blockingCommander, nil, nil)

	sleepDone := make(chan error, 1)
	go func() { sleepDone <- mgr.SleepServer(ctx, "srv-sleep") }()
	<-blockingCommander.entered // Sleep has set deepsleep and owns the transition.

	wakeDone := make(chan error, 1)
	go func() { wakeDone <- mgr.WakeServer(ctx, "srv-sleep") }()

	wakeReturnedEarly := false
	select {
	case <-wakeDone:
		wakeReturnedEarly = true
	case <-time.After(50 * time.Millisecond):
	}

	close(blockingCommander.release)
	require.NoError(t, <-sleepDone)
	if wakeReturnedEarly {
		assert.NoError(t, mgr.WakeServer(ctx, "srv-sleep"), "clean up the expected pre-fix state")
		t.Fatal("wake returned before the in-flight sleep completed")
	}
	require.NoError(t, <-wakeDone)

	srv, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status)
	controller.mu.Lock()
	assert.Len(t, controller.starts, 1, "wake must start the container after sleep completes")
	controller.mu.Unlock()
}

func TestDeepSleepManager_SleepRejectsNonRunning(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	require.NoError(t, store.UpdateServerStatus(ctx, "srv-sleep", db.StatusStopped))

	err := mgr.SleepServer(ctx, "srv-sleep")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "in status stopped")
}

func TestDeepSleepManager_WakeContainerStartFailure_RollsBackToError(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()

	var refreshed []string
	refresher := func(serverID string) {
		refreshed = append(refreshed, serverID)
	}

	mgr := NewDeepSleepManager(controller, store, commander, refresher, nil)
	require.NoError(t, mgr.SleepServer(ctx, "srv-sleep"))

	controller.mu.Lock()
	controller.startErr = fmt.Errorf("port already in use")
	controller.mu.Unlock()

	err := mgr.WakeServer(ctx, "srv-sleep")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "container start failed")

	srv, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusError, srv.Status, "server status must be rolled back to error")
	assert.Contains(t, refreshed, "srv-sleep", "proxy route refresher must be invoked on wake failure")
}

func TestDeepSleepManager_WakePersistenceFailureStopsContainer(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()
	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	require.NoError(t, mgr.SleepServer(ctx, "srv-sleep"))

	store.mu.Lock()
	store.updateServerErr = fmt.Errorf("injected server update failure")
	store.mu.Unlock()

	err := mgr.WakeServer(ctx, "srv-sleep")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update server status to running")

	controller.mu.Lock()
	status := controller.status["cnt-sleep"]
	stops := append([]string(nil), controller.stops...)
	controller.mu.Unlock()
	if status != db.StatusStopped {
		t.Fatalf("container status = %q after persistence failure, want stopped compensation", status)
	}
	assert.Contains(t, stops, "cnt-sleep", "wake compensation must stop the container")

	server, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusDeepSleep, server.Status, "failed wake must retain the stored deep-sleep intent after confirmed stop")
}

func TestDeepSleepManager_WakePersistenceAndRollbackFailureMarksError(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()
	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	require.NoError(t, mgr.SleepServer(ctx, "srv-sleep"))

	store.mu.Lock()
	store.updateServerErr = fmt.Errorf("injected server update failure")
	store.mu.Unlock()
	controller.mu.Lock()
	controller.stopErr = fmt.Errorf("injected container stop failure")
	controller.mu.Unlock()

	err := mgr.WakeServer(ctx, "srv-sleep")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stop container after wake status persistence failure")

	server, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusError, server.Status, "unknown live state after failed compensation must be reported as error")
}

func TestDeepSleepManager_WakeContainerPrematureExit_AbortsFast(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()

	var refreshed []string
	refresher := func(serverID string) {
		refreshed = append(refreshed, serverID)
	}

	mgr := NewDeepSleepManager(controller, store, commander, refresher, nil)
	require.NoError(t, mgr.SleepServer(ctx, "srv-sleep"))

	controller.mu.Lock()
	controller.startHook = func(containerID string) {
		// Simulate immediate crash after start (e.g. JVM exit / out-of-memory / bad config)
		controller.status[containerID] = db.StatusStopped
	}
	controller.mu.Unlock()

	start := time.Now()
	err := mgr.WakeServer(ctx, "srv-sleep")
	duration := time.Since(start)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "exited unexpectedly")
	assert.Less(t, duration, 2*time.Second, "must abort immediately rather than waiting for 120s timeout")

	srv, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusError, srv.Status, "server status must be marked as error")
	assert.Contains(t, refreshed, "srv-sleep", "proxy route refresher must be invoked")
}

func TestDeepSleepManager_RollbackSleep_NilControllerMarksError(t *testing.T) {
	ctx := context.Background()
	_, commander, store := newDeepSleepHarness()

	var refreshed []string
	refresher := func(serverID string) {
		refreshed = append(refreshed, serverID)
	}

	// Create manager with nil controller and resolver returning nil
	mgr := NewDeepSleepManager(nil, store, commander, refresher, nil)
	mgr.SetControllerResolver(func(nodeID string) SleepController {
		return nil
	})

	// Pre-condition: server is in DeepSleep status before rollback
	require.NoError(t, store.UpdateServerStatus(ctx, "srv-sleep", db.StatusDeepSleep))

	// Calling rollbackSleep directly with nil controller must not panic, and must mark status as error
	err := mgr.rollbackSleep(ctx, "srv-sleep", "cnt-sleep")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no docker controller available")

	srv, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusError, srv.Status, "server status must be marked as error on nil controller during rollback")
	assert.Contains(t, refreshed, "srv-sleep", "proxy route refresher must be invoked on rollback")
}

func TestDeepSleepManager_NilStoreGuards(t *testing.T) {
	ctx := context.Background()
	controller := newMockSleepController()
	mgr := NewDeepSleepManager(controller, nil, nil, nil, nil)

	err := mgr.SleepServer(ctx, "srv-sleep")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store is nil")

	err = mgr.WakeServer(ctx, "srv-sleep")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "store is nil")
}
