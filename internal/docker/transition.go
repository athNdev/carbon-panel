// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package docker

import (
	"context"
	"sync"
)

// serverTransitions serializes lifecycle operations across the RPC handlers,
// deep-sleep manager, and freezer manager in this process.
var serverTransitions = struct {
	sync.Mutex
	active map[string]chan struct{}
}{active: make(map[string]chan struct{})}

// TryBeginServerTransition claims the lifecycle transition for serverID.
func TryBeginServerTransition(serverID string) bool {
	if serverID == "" {
		return false
	}
	serverTransitions.Lock()
	defer serverTransitions.Unlock()
	if _, exists := serverTransitions.active[serverID]; exists {
		return false
	}
	serverTransitions.active[serverID] = make(chan struct{})
	return true
}

// EndServerTransition releases a lifecycle transition claimed by
// TryBeginServerTransition.
func EndServerTransition(serverID string) {
	serverTransitions.Lock()
	if done, exists := serverTransitions.active[serverID]; exists {
		close(done)
	}
	delete(serverTransitions.active, serverID)
	serverTransitions.Unlock()
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
