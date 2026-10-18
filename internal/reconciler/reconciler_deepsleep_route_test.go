// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package reconciler

import (
	"context"
	"testing"

	"github.com/moby/moby/api/types/container"

	"github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/internal/docker"
)

// TestReconcileServer_DeepSleepRefreshesProxyRoute ensures that a convergence
// pass for a deeply-asleep server refreshes its proxy route even when the
// status and container ID are already converged, so a dropped or missing
// down-route is re-armed for knock-to-wake (MINE-121 / MINE-172).
func TestReconcileServer_DeepSleepRefreshesProxyRoute(t *testing.T) {
	store := newFakeStore(&db.Server{
		ID:            "srv-sleep",
		Name:          "Sleepy",
		ContainerID:   "cid-sleep",
		Status:        db.StatusDeepSleep,
		ProxyHostname: "sleepy.example.com",
	})
	cli := &fakeClient{
		summary: &container.Summary{ID: "cid-sleep"},
		state:   &docker.ContainerState{ContainerID: "cid-sleep", Status: db.StatusStopped, HasExited: true, ExitCode: 0},
	}
	proxy := &fakeProxy{}

	rec := newTestReconciler(t, store, cli, proxy, nil)
	if err := rec.ReconcileServer(context.Background(), "srv-sleep"); err != nil {
		t.Fatalf("ReconcileServer error: %v", err)
	}

	got, err := store.GetServer(context.Background(), "srv-sleep")
	if err != nil {
		t.Fatalf("GetServer error: %v", err)
	}
	if got.Status != db.StatusDeepSleep {
		t.Fatalf("status = %q, want %q", got.Status, db.StatusDeepSleep)
	}
	if proxy.count() != 1 {
		t.Fatalf("proxy route refreshes = %d, want 1", proxy.count())
	}
}
