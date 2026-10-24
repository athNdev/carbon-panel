// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/internal/db"
)

type fakeIdleStore struct {
	mu      sync.Mutex
	servers []*db.Server
	err     error
	calls   int
}

func (f *fakeIdleStore) ListServers(context.Context) ([]*db.Server, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	return f.servers, f.err
}

func (f *fakeIdleStore) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func idleRunnerTestMode() idleMode {
	return idleMode{
		DefaultTimeoutMinutes: 10,
		Eligible:              func(*db.Server) bool { return true },
		TimeoutMinutes:        func(*db.Server) int { return 10 },
		Sleep:                 func(context.Context, string) error { return nil },
		IdleLogFormat:         "idle %s %s %v %s",
		ErrorLogFormat:        "failed %s %v",
		SweepLogFormat:        "sweep: %v",
	}
}

func TestIdleRunnerSkipsWhenNotReady(t *testing.T) {
	store := &fakeIdleStore{}
	mode := idleRunnerTestMode()
	mode.Ready = func() bool { return false }
	runner := newIdleRunner(store, newIdleTracker(), mode, nil)

	if err := runner.Check(context.Background()); err != nil {
		t.Fatalf("Check() = %v, want nil", err)
	}
	if got := store.count(); got != 0 {
		t.Fatalf("ListServers calls = %d, want 0 while the mode is not ready", got)
	}
}

func TestIdleRunnerPropagatesStoreError(t *testing.T) {
	store := &fakeIdleStore{err: errors.New("db is down")}
	runner := newIdleRunner(store, newIdleTracker(), idleRunnerTestMode(), nil)

	err := runner.Check(context.Background())
	if err == nil {
		t.Fatal("Check() = nil, want the store error")
	}
	if !errors.Is(err, store.err) {
		t.Fatalf("Check() error = %v, want it to wrap %v", err, store.err)
	}
}

func TestIdleRunnerStartStopHaltsSweeps(t *testing.T) {
	store := &fakeIdleStore{}
	runner := newIdleRunner(store, newIdleTracker(), idleRunnerTestMode(), nil)

	runner.Start(context.Background(), 5*time.Millisecond)

	deadline := time.Now().Add(2 * time.Second)
	for store.count() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("runner never swept after Start")
		}
		time.Sleep(2 * time.Millisecond)
	}

	runner.Stop()
	// Let an in-flight sweep finish before taking the baseline.
	time.Sleep(50 * time.Millisecond)
	after := store.count()
	time.Sleep(60 * time.Millisecond)
	if got := store.count(); got != after {
		t.Fatalf("sweeps kept running after Stop: %d -> %d", after, got)
	}
}
