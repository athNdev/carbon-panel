// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package main

import (
	"testing"

	storage "github.com/athNdev/carbon-panel/internal/db"
)

// TestShouldAutoStartOnBoot_SkipsDeepSleeping is the MINE-172 regression guard.
// A deeply-asleep server has a deliberately stopped container, so the old
// "auto_start && !detached" rule resurrected it on every panel restart — burning
// exactly the RAM deep sleep exists to release, and leaving the operator with a
// server they believed was asleep but was in fact running.
func TestShouldAutoStartOnBoot_SkipsDeepSleeping(t *testing.T) {
	srv := &storage.Server{
		Name:          "vanilla-1",
		AutoStart:     true,
		AutoDeepSleep: true,
		Status:        storage.StatusDeepSleep,
	}
	if shouldAutoStartOnBoot(srv) {
		t.Fatal("a deeply-asleep server must never be auto-started on boot")
	}
}

// TestShouldAutoStartOnBoot_StartsOrdinaryServers guards against over-correction:
// the deep-sleep exclusion must not disable auto-start for normal servers.
func TestShouldAutoStartOnBoot_StartsOrdinaryServers(t *testing.T) {
	cases := []struct {
		name string
		srv  *storage.Server
		want bool
	}{
		{"stopped with auto-start enabled", &storage.Server{AutoStart: true, Status: storage.StatusStopped}, true},
		{"running with auto-start enabled", &storage.Server{AutoStart: true, Status: storage.StatusRunning}, true},
		{"starting with auto-start enabled", &storage.Server{AutoStart: true, Status: storage.StatusStarting}, true},
		{"creating is left to the in-flight create", &storage.Server{AutoStart: true, Status: storage.StatusCreating}, false},
		{"auto-start disabled", &storage.Server{AutoStart: false, Status: storage.StatusStopped}, false},
		{"detached servers are left alone", &storage.Server{AutoStart: true, Detached: true, Status: storage.StatusStopped}, false},
		{"paused servers still auto-start", &storage.Server{AutoStart: true, Status: storage.StatusPaused}, true},
		{"nil server is safe", nil, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAutoStartOnBoot(tc.srv); got != tc.want {
				t.Fatalf("shouldAutoStartOnBoot() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestShouldAutoStartOnBoot_PausedIsNotExcluded documents why paused is treated
// differently from deep sleep: a paused server's container still exists and is
// only cgroup-frozen, so restarting the panel should bring it back, whereas a
// deep-slept container is gone.
func TestShouldAutoStartOnBoot_PausedIsNotExcluded(t *testing.T) {
	srv := &storage.Server{AutoStart: true, Status: storage.StatusPaused}
	if !shouldAutoStartOnBoot(srv) {
		t.Fatal("paused servers must remain eligible for auto-start")
	}
}
