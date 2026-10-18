package docker

import "sync"

// serverTransitions serializes lifecycle operations across the RPC handlers,
// deep-sleep manager, and freezer manager in this process.
var serverTransitions = struct {
	sync.Mutex
	active map[string]struct{}
}{active: make(map[string]struct{})}

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
	serverTransitions.active[serverID] = struct{}{}
	return true
}

// EndServerTransition releases a lifecycle transition claimed by
// TryBeginServerTransition.
func EndServerTransition(serverID string) {
	serverTransitions.Lock()
	delete(serverTransitions.active, serverID)
	serverTransitions.Unlock()
}

// ServerTransitionInFlight reports whether a lifecycle operation owns serverID.
func ServerTransitionInFlight(serverID string) bool {
	serverTransitions.Lock()
	_, exists := serverTransitions.active[serverID]
	serverTransitions.Unlock()
	return exists
}
