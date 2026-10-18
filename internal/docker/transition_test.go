package docker

import (
	"context"
	"testing"
	"time"
)

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

func TestWaitForServerTransitionWaitsUntilReleased(t *testing.T) {
	if !TryBeginServerTransition("srv-wait") {
		t.Fatal("transition should be acquired")
	}
	defer EndServerTransition("srv-wait")

	done := make(chan error, 1)
	go func() { done <- WaitForServerTransition(context.Background(), "srv-wait") }()
	select {
	case err := <-done:
		t.Fatalf("wait returned before transition release: %v", err)
	case <-time.After(20 * time.Millisecond):
	}

	EndServerTransition("srv-wait")
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
	if !TryBeginServerTransition("srv-cancel") {
		t.Fatal("transition should be acquired")
	}
	defer EndServerTransition("srv-cancel")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := WaitForServerTransition(ctx, "srv-cancel"); err != context.Canceled {
		t.Fatalf("WaitForServerTransition error = %v, want context canceled", err)
	}
}
