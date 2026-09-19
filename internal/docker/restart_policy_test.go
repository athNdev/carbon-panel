// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package docker

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// MINE-122 anti-flap pin: every Carbon Panel container must default to
// on-failure:5. If this fails, someone changed the default — update the
// reconciler settle logic + this test deliberately, not by accident.
func TestDefaultRestartPolicy(t *testing.T) {
	if DefaultRestartPolicy.Name != "on-failure" {
		t.Fatalf("DefaultRestartPolicy.Name = %q, want %q", DefaultRestartPolicy.Name, "on-failure")
	}
	if DefaultRestartPolicy.MaximumRetryCount != 5 {
		t.Fatalf("DefaultRestartPolicy.MaximumRetryCount = %d, want 5", DefaultRestartPolicy.MaximumRetryCount)
	}
}

func TestRestartPolicy_ApplyOverridesPreservesDefault(t *testing.T) {
	hostConfig := &container.HostConfig{
		RestartPolicy: DefaultRestartPolicy,
	}
	config := &container.Config{}

	// No override: default survives ApplyOverrides.
	ApplyOverrides(&v1.DockerOverrides{}, config, hostConfig)
	if hostConfig.RestartPolicy.Name != DefaultRestartPolicy.Name || hostConfig.RestartPolicy.MaximumRetryCount != DefaultRestartPolicy.MaximumRetryCount {
		t.Fatalf("empty overrides changed restart policy to %+v", hostConfig.RestartPolicy)
	}

	// Explicit override wins (operator escape hatch).
	ApplyOverrides(&v1.DockerOverrides{RestartPolicy: "no"}, config, hostConfig)
	if hostConfig.RestartPolicy.Name != "no" {
		t.Fatalf("override not applied, got %+v", hostConfig.RestartPolicy)
	}
}
