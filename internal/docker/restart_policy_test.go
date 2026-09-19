package docker

import (
	"testing"

	"github.com/docker/docker/api/types/container"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// MINE-122 anti-flap pin: every Carbon Panel container must default to
// unless-stopped. If this fails, someone changed the default — update the
// reconciler settle logic + this test deliberately, not by accident.
func TestDefaultRestartPolicy(t *testing.T) {
	if DefaultRestartPolicy != container.RestartPolicyMode("unless-stopped") {
		t.Fatalf("DefaultRestartPolicy = %q, want %q", DefaultRestartPolicy, "unless-stopped")
	}
}

func TestRestartPolicy_ApplyOverridesPreservesDefault(t *testing.T) {
	hostConfig := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: DefaultRestartPolicy},
	}
	config := &container.Config{}

	// No override: default survives ApplyOverrides.
	ApplyOverrides(&v1.DockerOverrides{}, config, hostConfig)
	if hostConfig.RestartPolicy.Name != DefaultRestartPolicy {
		t.Fatalf("empty overrides changed restart policy to %q", hostConfig.RestartPolicy.Name)
	}

	// Explicit override wins (operator escape hatch).
	ApplyOverrides(&v1.DockerOverrides{RestartPolicy: "no"}, config, hostConfig)
	if hostConfig.RestartPolicy.Name != container.RestartPolicyMode("no") {
		t.Fatalf("override not applied, got %q", hostConfig.RestartPolicy.Name)
	}
}
