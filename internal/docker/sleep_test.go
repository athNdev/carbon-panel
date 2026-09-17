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
	m.status[containerID] = db.StatusRunning
	m.starts = append(m.starts, containerID)
	return nil
}

func (m *mockSleepController) StopContainer(ctx context.Context, containerID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
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
	// Concurrent wakes share one boot: the loser returns nil (not an
	// error) and the container starts exactly once.
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

	require.NoError(t, mgr.WakeServer(ctx, "srv-sleep"), "concurrent wake must share the in-flight boot")

	close(controller.startGate)
	require.NoError(t, <-firstDone)

	controller.mu.Lock()
	defer controller.mu.Unlock()
	assert.Len(t, controller.starts, 1, "container must start exactly once")
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
