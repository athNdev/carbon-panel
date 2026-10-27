// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"testing"

	models "github.com/athNdev/carbon-panel/internal/db"
	"github.com/moby/moby/api/types/container"
)

// TestMapContainerStateVocabulary locks the Docker-state → ServerStatus
// translation so the panel never reports a stopped container as "crashed" or
// loses a paused state (RT-04).
func TestMapContainerStateVocabulary(t *testing.T) {
	tests := []struct {
		name  string
		state *container.State
		want  models.ServerStatus
	}{
		{"nil state is an error", nil, models.StatusError},
		{"running with no health is running", &container.State{Status: "running"}, models.StatusRunning},
		{"running healthy", &container.State{Status: "running", Health: &container.Health{Status: "healthy"}}, models.StatusRunning},
		{"running starting health", &container.State{Status: "running", Health: &container.Health{Status: "starting"}}, models.StatusStarting},
		{"running unhealthy health", &container.State{Status: "running", Health: &container.Health{Status: "unhealthy"}}, models.StatusUnhealthy},
		{"running unknown health falls back to running", &container.State{Status: "running", Health: &container.Health{Status: "weird"}}, models.StatusRunning},
		{"restarting is starting", &container.State{Status: "restarting"}, models.StatusStarting},
		{"exited is stopped", &container.State{Status: "exited"}, models.StatusStopped},
		{"dead is stopped", &container.State{Status: "dead"}, models.StatusStopped},
		{"created is stopped", &container.State{Status: "created"}, models.StatusStopped},
		{"removing is stopped", &container.State{Status: "removing"}, models.StatusStopped},
		{"paused is paused", &container.State{Status: "paused"}, models.StatusPaused},
		{"unknown status is an error", &container.State{Status: "nonsense"}, models.StatusError},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapContainerState(tt.state); got != tt.want {
				t.Errorf("mapContainerState(%v) = %v, want %v", tt.state, got, tt.want)
			}
		})
	}
}
