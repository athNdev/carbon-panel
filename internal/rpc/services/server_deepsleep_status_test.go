package services

import (
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"github.com/stretchr/testify/assert"
)

// TestPreserveStoredStatus_DeepSleepSurvivesLiveStop is the regression guard for
// MINE-172: a deeply-asleep server's container is EXPECTED to be exited, so a
// live Docker inspect reports "stopped". Without this guard ListServers and
// GetServer overwrite the stored DeepSleep status and the UI renders the server
// as plain OFFLINE with no Wake action — the exact reported symptom.
func TestPreserveStoredStatus_DeepSleepSurvivesLiveStop(t *testing.T) {
	assert.True(t, preserveStoredStatus(storage.StatusDeepSleep, storage.StatusStopped),
		"a deep-sleeping container is exited by design; the live 'stopped' reading must not win")

	// The stored DeepSleep status owns the display value regardless of what
	// Docker reports, including the transient readings during a wake.
	for _, live := range []storage.ServerStatus{
		storage.StatusStopped,
		storage.StatusRunning,
		storage.StatusStarting,
		storage.StatusUnhealthy,
		storage.StatusError,
	} {
		assert.True(t, preserveStoredStatus(storage.StatusDeepSleep, live),
			"stored DeepSleep must be preserved over live status %q", live)
	}
}

// TestPreserveStoredStatus_PausedSurvivesLiveStop covers the freeze case: a
// paused server is cgroup-frozen but its container still exists, so a "stopped"
// reading means the daemon lost the pause state. Keeping PAUSED until the
// reconciler converges avoids flapping the UI.
func TestPreserveStoredStatus_PausedSurvivesLiveStop(t *testing.T) {
	assert.True(t, preserveStoredStatus(storage.StatusPaused, storage.StatusStopped),
		"a lost pause state must not immediately render PAUSED as OFFLINE")
	assert.False(t, preserveStoredStatus(storage.StatusPaused, storage.StatusRunning),
		"a running container is real evidence; PAUSED must yield to it")
}

// TestPreserveStoredStatus_DoesNotMaskOrdinaryStatuses ensures the guard stays
// narrow: a genuinely stopped, running, or errored server must still track
// reality, otherwise the UI would freeze on stale data.
func TestPreserveStoredStatus_DoesNotMaskOrdinaryStatuses(t *testing.T) {
	assert.False(t, preserveStoredStatus(storage.StatusStopped, storage.StatusRunning),
		"an operator-started container must show as running")
	assert.False(t, preserveStoredStatus(storage.StatusRunning, storage.StatusStopped),
		"a genuinely stopped container must not keep showing as running")
	assert.False(t, preserveStoredStatus(storage.StatusError, storage.StatusStopped),
		"error must yield to a live reading like any other status")
	assert.False(t, preserveStoredStatus(storage.StatusStopped, storage.StatusDeepSleep),
		"a live deep-sleep reading still refreshes the display")
}

// TestDBStatusToProto_MapsHibernationStates verifies the deep-sleep and paused
// storage statuses reach the client as distinct enum values. Without these
// cases the server fell through to UNSPECIFIED and the UI had no Deep Sleep or
// Paused tag to render.
func TestDBStatusToProto_MapsHibernationStates(t *testing.T) {
	assert.Equal(t, v1.ServerStatus_SERVER_STATUS_DEEPSLEEP,
		dbStatusToProto(storage.StatusDeepSleep))
	assert.Equal(t, v1.ServerStatus_SERVER_STATUS_PAUSED,
		dbStatusToProto(storage.StatusPaused))
}

// TestDBStatusToProto_DoesNotCollapseOrdinaryStatuses guards the surrounding
// switch against accidental regressions from the hibernation additions.
func TestDBStatusToProto_DoesNotCollapseOrdinaryStatuses(t *testing.T) {
	assert.Equal(t, v1.ServerStatus_SERVER_STATUS_RUNNING, dbStatusToProto(storage.StatusRunning))
	assert.Equal(t, v1.ServerStatus_SERVER_STATUS_STOPPED, dbStatusToProto(storage.StatusStopped))
	assert.Equal(t, v1.ServerStatus_SERVER_STATUS_ERROR, dbStatusToProto(storage.StatusError))
	assert.Equal(t, v1.ServerStatus_SERVER_STATUS_UNHEALTHY, dbStatusToProto(storage.StatusUnhealthy))
}
