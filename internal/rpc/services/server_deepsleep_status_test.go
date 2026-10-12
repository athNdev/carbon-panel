package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
	storage "github.com/athNdev/carbon-panel/internal/db"
)

// TestPreserveStoredStatus_DeepSleepSurvivesRefresh covers the MINE-172
// follow-up: Docker reports a deep-asleep container as merely gone
// (exited/dead/created all map to StatusStopped/StatusCreating), which is
// indistinguishable from an ordinary stop. The stored deep-sleep/paused
// status must survive the live refresh; only a live container (a wake in
// flight) overwrites it.
func TestPreserveStoredStatus_DeepSleepSurvivesRefresh(t *testing.T) {
	tests := []struct {
		name     string
		stored   storage.ServerStatus
		observed storage.ServerStatus
		want     storage.ServerStatus
	}{
		{
			name:     "deepsleep survives stopped refresh (the bug)",
			stored:   storage.StatusDeepSleep,
			observed: storage.StatusStopped,
			want:     storage.StatusDeepSleep,
		},
		{
			name:     "deepsleep survives creating refresh",
			stored:   storage.StatusDeepSleep,
			observed: storage.StatusCreating,
			want:     storage.StatusDeepSleep,
		},
		{
			name:     "paused survives stopped refresh",
			stored:   storage.StatusPaused,
			observed: storage.StatusStopped,
			want:     storage.StatusPaused,
		},
		{
			name:     "live container wins over deepsleep (wake in flight)",
			stored:   storage.StatusDeepSleep,
			observed: storage.StatusRunning,
			want:     storage.StatusRunning,
		},
		{
			name:     "ordinary servers still refresh normally",
			stored:   storage.StatusRunning,
			observed: storage.StatusStopped,
			want:     storage.StatusStopped,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, preserveStoredStatus(tt.stored, tt.observed))
		})
	}
}
