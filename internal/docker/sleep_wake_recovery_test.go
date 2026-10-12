package docker

import (
	"testing"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// driftHarness seeds the deep-sleep harness then drifts the recorded status
// away from StatusDeepSleep, simulating the live Docker refresh reporting
// the exited container as stopped.
func driftHarness(t *testing.T, status db.ServerStatus, autoDeepSleep bool) (*mockSleepController, *mockSleepCommander, *mockFreezerStore) {
	t.Helper()
	controller, commander, store := newDeepSleepHarness()
	srv, err := store.GetServer(t.Context(), "srv-sleep")
	require.NoError(t, err)
	srv.Status = status
	srv.AutoDeepSleep = autoDeepSleep
	require.NoError(t, store.UpdateServer(t.Context(), srv))
	return controller, commander, store
}

func TestDeepSleepManager_WakeRecoversDriftedStoppedStatus(t *testing.T) {
	ctx := t.Context()
	controller, commander, store := driftHarness(t, db.StatusStopped, true)

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	err := mgr.WakeServer(ctx, "srv-sleep")
	if err != nil {
		// The boot path may fail on a fake controller, but it must never be
		// the status-guard refusal: drift is recoverable.
		assert.NotContains(t, err.Error(), "non-deepsleep status")
	}

	srv, err := store.GetServer(ctx, "srv-sleep")
	require.NoError(t, err)
	assert.Equal(t, db.StatusRunning, srv.Status, "drifted wake must proceed to the boot path")
}

func TestDeepSleepManager_WakeStillGuardsWithoutAutoDeepSleep(t *testing.T) {
	ctx := t.Context()
	controller, commander, store := driftHarness(t, db.StatusStopped, false)

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	err := mgr.WakeServer(ctx, "srv-sleep")
	require.Error(t, err, "drifted status without AutoDeepSleep must still be refused")
	assert.Contains(t, err.Error(), "non-deepsleep status")
}

func TestDeepSleepManager_WakeRunningIsIdempotent(t *testing.T) {
	ctx := t.Context()
	controller, commander, store := newDeepSleepHarness()

	mgr := NewDeepSleepManager(controller, store, commander, nil, nil)
	require.NoError(t, mgr.WakeServer(ctx, "srv-sleep"))

	controller.mu.Lock()
	defer controller.mu.Unlock()
	assert.Empty(t, controller.starts, "idempotent wake must not boot an already-running server")
}
