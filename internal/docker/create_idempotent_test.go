package docker

import (
	"errors"
	"fmt"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
)

// TestIsConflictError verifies isConflictError uses the containerd errdefs.IsConflict
// convention (the same helper Docker's own daemon uses to distinguish a "name already in
// use" 409 from other errors), rather than fragile string matching.
func TestIsConflictError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"conflict sentinel", errdefs.ErrConflict, true},
		{"wrapped conflict", fmt.Errorf("create container: %w", errdefs.ErrConflict), true},
		{"not found is not conflict", errdefs.ErrNotFound, false},
		{"plain error", errors.New("boom"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isConflictError(tt.err); got != tt.want {
				t.Errorf("isConflictError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// TestIsAliveContainerState verifies the adopt-vs-remove-stale decision boundary: running,
// restarting, and paused containers are adopted; everything else (created, exited, dead,
// removing, unknown) is treated as a stale leftover to remove.
func TestIsAliveContainerState(t *testing.T) {
	alive := []container.ContainerState{
		container.StateRunning,
		container.StateRestarting,
		container.StatePaused,
	}
	for _, s := range alive {
		if !isAliveContainerState(s) {
			t.Errorf("isAliveContainerState(%q) = false, want true", s)
		}
	}

	stale := []container.ContainerState{
		container.StateCreated,
		container.StateExited,
		container.StateDead,
		container.StateRemoving,
		container.ContainerState("unknown"),
		container.ContainerState(""),
	}
	for _, s := range stale {
		if isAliveContainerState(s) {
			t.Errorf("isAliveContainerState(%q) = true, want false", s)
		}
	}
}
