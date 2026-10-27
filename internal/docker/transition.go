// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// ErrTransitionInFlight reports that another lifecycle operation already owns
// the server.
var ErrTransitionInFlight = errors.New("server transition already in progress")

// ErrTransitionServerID reports a lifecycle operation that supplied no server
// ID to own.
var ErrTransitionServerID = errors.New("server transition requires a server ID")

// transitionState tracks who owns a lifecycle transition and since when, so a
// stuck lock ("server transition already in progress" forever) is observable
// instead of silent.
type transitionState struct {
	done   chan struct{}
	since  time.Time
	caller string
}

// serverTransitions serializes lifecycle operations across the RPC handlers,
// deep-sleep manager, and freezer manager in this process.
var serverTransitions = struct {
	sync.Mutex
	active map[string]transitionState
}{active: make(map[string]transitionState)}

// Transition is one lifecycle operation's claim on a server. It is the only
// handle that can release the claim, so a caller cannot clear a transition it
// never acquired. Release is idempotent and safe to defer.
type Transition struct {
	serverID string
	done     chan struct{}
	once     sync.Once
}

// slowTransitionWarnAt is how long a transition may be held before contention
// reports it as a possible stuck lock.
const slowTransitionWarnAt = 2 * time.Minute

// transitionWarner receives contention diagnostics. It is a variable so tests
// can capture warnings and the server process can install its logger.
var transitionWarner = func(string, ...any) {}

// SetTransitionWarner installs the callback used to warn about long-held
// lifecycle transitions. Passing nil restores the silent default.
func SetTransitionWarner(fn func(format string, args ...any)) {
	serverTransitions.Lock()
	defer serverTransitions.Unlock()
	if fn == nil {
		transitionWarner = func(string, ...any) {}
		return
	}
	transitionWarner = fn
}

// TryBeginServerTransition claims the lifecycle transition for serverID.
func TryBeginServerTransition(serverID string) (*Transition, error) {
	if serverID == "" {
		return nil, ErrTransitionServerID
	}
	_, file, line, _ := runtime.Caller(1)
	caller := fmt.Sprintf("%s:%d", filepath.Base(file), line)

	serverTransitions.Lock()
	defer serverTransitions.Unlock()
	if st, exists := serverTransitions.active[serverID]; exists {
		if held := time.Since(st.since); held > slowTransitionWarnAt {
			transitionWarner("lifecycle transition for %s still held after %v (started at %s); %s is blocked", serverID, held.Round(time.Second), st.caller, caller)
		}
		return nil, ErrTransitionInFlight
	}
	done := make(chan struct{})
	serverTransitions.active[serverID] = transitionState{done: done, since: time.Now(), caller: caller}
	return &Transition{serverID: serverID, done: done}, nil
}

// ServerID is the server this transition owns.
func (t *Transition) ServerID() string {
	if t == nil {
		return ""
	}
	return t.serverID
}

// Done is closed once the transition is released.
func (t *Transition) Done() <-chan struct{} {
	if t == nil {
		return nil
	}
	return t.done
}

// Release ends the transition. It removes the registration only while this
// handle still owns it, so releasing a stale handle cannot evict a later owner
// that has since claimed the server.
func (t *Transition) Release() {
	if t == nil {
		return
	}
	t.once.Do(func() {
		serverTransitions.Lock()
		if current, exists := serverTransitions.active[t.serverID]; exists && current.done == t.done {
			delete(serverTransitions.active, t.serverID)
		}
		close(t.done)
		serverTransitions.Unlock()
	})
}

// WaitForServerTransition waits for the current lifecycle operation on serverID
// to finish. It returns immediately when no operation is in flight.
func WaitForServerTransition(ctx context.Context, serverID string) error {
	for {
		serverTransitions.Lock()
		st, exists := serverTransitions.active[serverID]
		serverTransitions.Unlock()
		if !exists {
			return nil
		}
		select {
		case <-st.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ServerTransitionInFlight reports whether a lifecycle operation owns serverID.
func ServerTransitionInFlight(serverID string) bool {
	serverTransitions.Lock()
	_, exists := serverTransitions.active[serverID]
	serverTransitions.Unlock()
	return exists
}

// TransitionDebug describes the current holder of serverID's lifecycle
// transition, for diagnostics. It returns "" when no transition is in flight.
func TransitionDebug(serverID string) string {
	serverTransitions.Lock()
	defer serverTransitions.Unlock()
	st, exists := serverTransitions.active[serverID]
	if !exists {
		return ""
	}
	return fmt.Sprintf("held %v by %s", time.Since(st.since).Round(time.Second), st.caller)
}
