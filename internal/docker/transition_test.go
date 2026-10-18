package docker

import "testing"

func TestServerTransitionsAreExclusivePerServer(t *testing.T) {
	if !TryBeginServerTransition("srv-a") {
		t.Fatal("first transition should acquire the server")
	}
	if TryBeginServerTransition("srv-a") {
		t.Fatal("concurrent transition should be rejected")
	}
	if !ServerTransitionInFlight("srv-a") {
		t.Fatal("server should be reported as transitioning")
	}
	if !TryBeginServerTransition("srv-b") {
		t.Fatal("different server should transition independently")
	}
	EndServerTransition("srv-a")
	if ServerTransitionInFlight("srv-a") {
		t.Fatal("released server should no longer be transitioning")
	}
	if !TryBeginServerTransition("srv-a") {
		t.Fatal("released server should be acquirable again")
	}
	EndServerTransition("srv-a")
	EndServerTransition("srv-b")
}

func TestTryBeginServerTransitionRejectsEmptyID(t *testing.T) {
	if TryBeginServerTransition("") {
		t.Fatal("empty server ID must not acquire a transition")
	}
}
