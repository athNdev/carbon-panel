package docker

import (
	"context"
	"testing"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeepSleepManager_WakeRecoversDriftedStoppedAutoDeepSleep verifies that
// if a server has AutoDeepSleep enabled and a knock-to-wake arrives while its
// DB status drifted to StatusStopped (or StatusCreating), WakeServer still
// boots the container instead of failing with a non-deepsleep error (MINE-172).
func TestDeepSleepManager_WakeRecoversDriftedStoppedAutoDeepSleep(t *testing.T) {
	ctx := context.Background()
	controller, commander, store := newDeepSleepHarness()
	controller.status["cnt-sleep"] = db.StatusStopped
	store.servers["srv-sleep"].Status = db.StatusStopped
	store.servers["srv-sleep"].AutoDeepSleep = true

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	require.NoError(t, mgr.WakeServer(ctx, "srv-sleep"))

	srv, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status)
}

// TestDeepSleepManager_MultiNodeResolver verifies that SleepServer and WakeServer
// route container operations to the node-specific controller when a resolver is
// configured.
func TestDeepSleepManager_MultiNodeResolver(t *testing.T) {
	ctx := context.Background()
	defaultCtrl := newMockSleepController()
	remoteCtrl := newMockSleepController()
	remoteCtrl.status["cnt-remote"] = db.StatusRunning

	store := newMockFreezerStore()
	store.servers["srv-remote"] = &db.Server{
		ID:                      "srv-remote",
		Name:                    "Remote Sleepy",
		NodeID:                  "node-remote",
		ContainerID:             "cnt-remote",
		Status:                  db.StatusRunning,
		AutoDeepSleep:           true,
		DeepSleepTimeoutMinutes: 1,
	}

	commander := &mockSleepCommander{
		onCommand: func(serverID, command string) {
			if command == "stop" {
				remoteCtrl.mu.Lock()
				remoteCtrl.status["cnt-remote"] = db.StatusStopped
				remoteCtrl.mu.Unlock()
			}
		},
	}

	mgr := NewDeepSleepManager(defaultCtrl, store, commander, nil, nil)
	mgr.SetControllerResolver(func(nodeID string) SleepController {
		if nodeID == "node-remote" {
			return remoteCtrl
		}
		return defaultCtrl
	})

	require.NoError(t, mgr.SleepServer(ctx, "srv-remote"))
	srv, err := store.GetServer(ctx, "srv-remote")
	require.NoError(t, err)
	assert.Equal(t, db.StatusDeepSleep, srv.Status)

	require.NoError(t, mgr.WakeServer(ctx, "srv-remote"))
	remoteCtrl.mu.Lock()
	assert.Contains(t, remoteCtrl.starts, "cnt-remote")
	remoteCtrl.mu.Unlock()
	defaultCtrl.mu.Lock()
	assert.Empty(t, defaultCtrl.starts)
	defaultCtrl.mu.Unlock()
}
