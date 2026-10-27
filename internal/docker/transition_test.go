// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestServerTransitionsAreExclusivePerServer(t *testing.T) {
	first, err := TryBeginServerTransition("srv-a")
	if err != nil {
		t.Fatalf("first transition should acquire the server: %v", err)
	}
	if _, err := TryBeginServerTransition("srv-a"); !errors.Is(err, ErrTransitionInFlight) {
		t.Fatalf("concurrent transition error = %v, want ErrTransitionInFlight", err)
	}
	if !ServerTransitionInFlight("srv-a") {
		t.Fatal("server should be reported as transitioning")
	}
	second, err := TryBeginServerTransition("srv-b")
	if err != nil {
		t.Fatalf("different server should transition independently: %v", err)
	}
	first.Release()
	if ServerTransitionInFlight("srv-a") {
		t.Fatal("released server should no longer be transitioning")
	}
	if _, err := TryBeginServerTransition("srv-a"); err != nil {
		t.Fatalf("released server should be acquirable again: %v", err)
	}
	second.Release()
}

func TestTryBeginServerTransitionRejectsEmptyID(t *testing.T) {
	if _, err := TryBeginServerTransition(""); !errors.Is(err, ErrTransitionServerID) {
		t.Fatalf("empty server ID error = %v, want ErrTransitionServerID", err)
	}
	if ServerTransitionInFlight("") {
		t.Fatal("empty server ID must not register a transition")
	}
}

func TestTransitionReleaseIsIdempotent(t *testing.T) {
	transition, err := TryBeginServerTransition("srv-idempotent")
	if err != nil {
		t.Fatalf("transition should be acquired: %v", err)
	}
	transition.Release()
	transition.Release()
	if ServerTransitionInFlight("srv-idempotent") {
		t.Fatal("released server should no longer be transitioning")
	}
	if _, err := TryBeginServerTransition("srv-idempotent"); err != nil {
		t.Fatalf("server should be acquirable after repeated release: %v", err)
	}
}

// TestStaleReleaseKeepsLaterOwner pins the ownership rule: a handle that has
// already been released must not clear a claim taken by a later operation.
func TestStaleReleaseKeepsLaterOwner(t *testing.T) {
	stale, err := TryBeginServerTransition("srv-stale")
	if err != nil {
		t.Fatalf("first transition should be acquired: %v", err)
	}
	stale.Release()

	owner, err := TryBeginServerTransition("srv-stale")
	if err != nil {
		t.Fatalf("released server should be acquirable: %v", err)
	}
	stale.Release()

	if !ServerTransitionInFlight("srv-stale") {
		t.Fatal("stale release cleared the current owner's transition")
	}
	if _, err := TryBeginServerTransition("srv-stale"); !errors.Is(err, ErrTransitionInFlight) {
		t.Fatalf("current owner was evicted: error = %v", err)
	}
	owner.Release()
	if ServerTransitionInFlight("srv-stale") {
		t.Fatal("current owner release should clear the transition")
	}
}

func TestWaitForServerTransitionWaitsUntilReleased(t *testing.T) {
	transition, err := TryBeginServerTransition("srv-wait")
	if err != nil {
		t.Fatalf("transition should be acquired: %v", err)
	}
	defer transition.Release()

	done := make(chan error, 1)
	go func() { done <- WaitForServerTransition(context.Background(), "srv-wait") }()
	select {
	case err := <-done:
		t.Fatalf("wait returned before transition release: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	transition.Release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("wait returned error after release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("wait did not complete after transition release")
	}
}

func TestWaitForServerTransitionHonorsContext(t *testing.T) {
	transition, err := TryBeginServerTransition("srv-cancel")
	if err != nil {
		t.Fatalf("transition should be acquired: %v", err)
	}
	defer transition.Release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WaitForServerTransition(ctx, "srv-cancel"); err != context.Canceled {
		t.Fatalf("WaitForServerTransition error = %v, want context canceled", err)
	}
}

func TestTransitionDiagnosticsWarnOnStuckLock(t *testing.T) {
	var warned []string
	SetTransitionWarner(func(format string, args ...any) {
		warned = append(warned, fmt.Sprintf(format, args...))
	})
	t.Cleanup(func() { SetTransitionWarner(nil) })

	held, err := TryBeginServerTransition("srv-stuck")
	if err != nil {
		t.Fatalf("transition should be acquired: %v", err)
	}
	defer held.Release()

	// A fresh holder must not warn on contention.
	if _, err := TryBeginServerTransition("srv-stuck"); err == nil {
		t.Fatal("second claim should be rejected")
	}
	if len(warned) != 0 {
		t.Fatalf("unexpected warning for a fresh holder: %v", warned)
	}

	// Backdate the holder past the slow threshold; contention must warn and
	// name the holder.
	serverTransitions.Lock()
	st := serverTransitions.active["srv-stuck"]
	st.since = time.Now().Add(-slowTransitionWarnAt - time.Minute)
	serverTransitions.active["srv-stuck"] = st
	serverTransitions.Unlock()

	if _, err := TryBeginServerTransition("srv-stuck"); err == nil {
		t.Fatal("second claim should be rejected")
	}
	if len(warned) != 1 {
		t.Fatalf("want exactly one stuck-lock warning, got %v", warned)
	}
	if !strings.Contains(warned[0], "srv-stuck") || !strings.Contains(warned[0], "held") {
		t.Fatalf("warning lacks holder details: %q", warned[0])
	}

	debug := TransitionDebug("srv-stuck")
	if !strings.Contains(debug, "held") {
		t.Fatalf("TransitionDebug = %q, want holder details", debug)
	}
	held.Release()
	if got := TransitionDebug("srv-stuck"); got != "" {
		t.Fatalf("TransitionDebug after release = %q, want empty", got)
	}
}
