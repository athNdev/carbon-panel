// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
)

func idleTestServer() *db.Server {
	return &db.Server{
		ID:                      "srv-idle",
		Name:                    "idle",
		Status:                  db.StatusRunning,
		ContainerID:             "container-1",
		AutoHibernate:           true,
		AutoDeepSleep:           true,
		IdleTimeoutMinutes:      10,
		DeepSleepTimeoutMinutes: 30,
	}
}

func idleTestMode(slept *[]string) idleMode {
	return idleMode{
		DefaultTimeoutMinutes: 10,
		Eligible:              func(s *db.Server) bool { return s.AutoHibernate },
		TimeoutMinutes:        func(s *db.Server) int { return s.IdleTimeoutMinutes },
		Sleep: func(_ context.Context, serverID string) error {
			*slept = append(*slept, serverID)
			return nil
		},
		IdleLogFormat:  "Server %s (%s) idle for %v, container %s",
		ErrorLogFormat: "Failed to sleep server %s: %v",
	}
}

func TestIdleSweepWaitsOneIntervalBeforeSleeping(t *testing.T) {
	tracker := newIdleTracker()
	srv := idleTestServer()
	var slept []string

	tracker.sweep(context.Background(), []*db.Server{srv}, idleTestMode(&slept), nil)
	if len(slept) != 0 {
		t.Fatalf("first observation slept %v, want nothing", slept)
	}

	tracker.sweep(context.Background(), []*db.Server{srv}, idleTestMode(&slept), nil)
	if len(slept) != 0 {
		t.Fatalf("second observation slept %v before the timeout elapsed", slept)
	}

	tracker.markIdleSince(srv.ID, time.Now().Add(-11*time.Minute))
	tracker.sweep(context.Background(), []*db.Server{srv}, idleTestMode(&slept), nil)
	if len(slept) != 1 || slept[0] != srv.ID {
		t.Fatalf("slept = %v, want [%s]", slept, srv.ID)
	}
}

func TestIdleSweepUsesDefaultTimeout(t *testing.T) {
	tracker := newIdleTracker()
	srv := idleTestServer()
	srv.IdleTimeoutMinutes = 0
	var slept []string

	tracker.markIdleSince(srv.ID, time.Now().Add(-11*time.Minute))
	tracker.sweep(context.Background(), []*db.Server{srv}, idleTestMode(&slept), nil)
	if len(slept) != 1 {
		t.Fatalf("slept = %v, want one sleep from the default timeout", slept)
	}
}

func TestIdleSweepSkipsServersWithPlayers(t *testing.T) {
	tracker := newIdleTracker()
	tracker.SetPlayerCountProvider(func(string) int { return 2 })
	srv := idleTestServer()
	var slept []string

	tracker.markIdleSince(srv.ID, time.Now().Add(-24*time.Hour))
	tracker.sweep(context.Background(), []*db.Server{srv}, idleTestMode(&slept), nil)
	if len(slept) != 0 {
		t.Fatalf("slept = %v, want nothing while players are online", slept)
	}

	// Dropping to zero players restarts the clock rather than sleeping at once.
	tracker.SetPlayerCountProvider(func(string) int { return 0 })
	tracker.sweep(context.Background(), []*db.Server{srv}, idleTestMode(&slept), nil)
	if len(slept) != 0 {
		t.Fatalf("slept = %v immediately after players left, want one idle interval", slept)
	}
}

func TestIdleSweepForgetsIneligibleServers(t *testing.T) {
	tracker := newIdleTracker()
	srv := idleTestServer()
	var slept []string

	tracker.markIdleSince(srv.ID, time.Now().Add(-24*time.Hour))
	srv.Status = db.StatusStopped
	tracker.sweep(context.Background(), []*db.Server{srv}, idleTestMode(&slept), nil)
	if len(slept) != 0 {
		t.Fatalf("slept = %v, want nothing for a stopped server", slept)
	}

	srv.Status = db.StatusRunning
	tracker.sweep(context.Background(), []*db.Server{srv}, idleTestMode(&slept), nil)
	if len(slept) != 0 {
		t.Fatalf("slept = %v, want the clock reset after an ineligible sweep", slept)
	}
}

// TestIdleTransitionsAreExclusiveAcrossModes pins the reason both modes share
// one tracker: freeze and deep sleep must not own the same server at once.
func TestIdleTransitionsAreExclusiveAcrossModes(t *testing.T) {
	freeze := newIdleTracker()
	deepSleep := newIdleTracker()

	if !freeze.tryBegin("srv-shared", "hibernate", nil) {
		t.Fatal("freeze should claim the server")
	}
	if deepSleep.tryBegin("srv-shared", "sleep", nil) {
		t.Fatal("deep sleep claimed a server the freezer owns")
	}
	if !freeze.transitioning("srv-shared") {
		t.Fatal("freeze should still own the server")
	}

	freeze.end("srv-shared")
	if freeze.transitioning("srv-shared") {
		t.Fatal("released server is still transitioning")
	}
	if !deepSleep.tryBegin("srv-shared", "sleep", nil) {
		t.Fatal("deep sleep should claim the server after the freeze released it")
	}
	deepSleep.end("srv-shared")
}

func TestIdleTransitionReleaseIsIdempotent(t *testing.T) {
	tracker := newIdleTracker()
	if !tracker.tryBegin("srv-once", "sleep", nil) {
		t.Fatal("tracker should claim the server")
	}
	tracker.end("srv-once")
	tracker.end("srv-once")
	if tracker.transitioning("srv-once") {
		t.Fatal("released server is still transitioning")
	}
	if !tracker.tryBegin("srv-once", "sleep", nil) {
		t.Fatal("server should be claimable after repeated release")
	}
	tracker.end("srv-once")
}
