// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.

package docker

import (
	"context"
	"errors"
	"sync"
)

// ErrTransitionInFlight reports that another lifecycle operation already owns
// the server.
var ErrTransitionInFlight = errors.New("server transition already in progress")

// ErrTransitionServerID reports a lifecycle operation that supplied no server
// ID to own.
var ErrTransitionServerID = errors.New("server transition requires a server ID")

// serverTransitions serializes lifecycle operations across the RPC handlers,
// deep-sleep manager, and freezer manager in this process.
var serverTransitions = struct {
	sync.Mutex
	active map[string]chan struct{}
}{active: make(map[string]chan struct{})}

// Transition is one lifecycle operation's claim on a server. It is the only
// handle that can release the claim, so a caller cannot clear a transition it
// never acquired. Release is idempotent and safe to defer.
type Transition struct {
	serverID string
	done     chan struct{}
	once     sync.Once
}

// TryBeginServerTransition claims the lifecycle transition for serverID.
func TryBeginServerTransition(serverID string) (*Transition, error) {
	if serverID == "" {
		return nil, ErrTransitionServerID
	}
	serverTransitions.Lock()
	defer serverTransitions.Unlock()
	if _, exists := serverTransitions.active[serverID]; exists {
		return nil, ErrTransitionInFlight
	}
	done := make(chan struct{})
	serverTransitions.active[serverID] = done
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
		if current, exists := serverTransitions.active[t.serverID]; exists && current == t.done {
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
		done, exists := serverTransitions.active[serverID]
		serverTransitions.Unlock()
		if !exists {
			return nil
		}
		select {
		case <-done:
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
