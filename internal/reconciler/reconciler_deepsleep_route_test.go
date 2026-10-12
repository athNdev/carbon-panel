package reconciler

import (
	"testing"

	"github.com/athNdev/carbon-panel/internal/db"
)

// TestDecide_DeepSleepAlwaysReconcilesRoute is the MINE-172 follow-up
// regression: decide() used to return a zero decision for deep-asleep
// servers, so a Down route lost to a missed refresh (or a proxy restart)
// was never repaired and the knock silently did nothing. The route must
// always be reconciled, while heal/recreate/status actions stay unset —
// the DeepSleepManager owns that server.
func TestDecide_DeepSleepAlwaysReconcilesRoute(t *testing.T) {
	tests := []struct {
		name     string
		obs      observedState
		detached bool
		selfHeal bool
	}{
		{
			name:     "exited container is expected state",
			obs:      observedState{Exists: true, ContainerID: "c1", Status: db.StatusStopped, ExitCode: 0},
			selfHeal: true,
		},
		{
			name:     "missing container with self-heal on still takes no action",
			obs:      observedState{Exists: false},
			selfHeal: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decide(db.StatusDeepSleep, tt.obs, tt.detached, tt.selfHeal)
			if !got.reconcileRoute {
				t.Fatalf("reconcileRoute = false, want true")
			}
			if got.action != actNone {
				t.Fatalf("action = %v, want no action (%v)", got.action, actNone)
			}
			if got.status != "" {
				t.Fatalf("status = %q, want unchanged (\"\")", got.status)
			}
		})
	}
}
