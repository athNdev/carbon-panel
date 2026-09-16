package ws

import (
	"testing"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestHubMigrateServerContainerUpdatesSubscriptions(t *testing.T) {
	h := &Hub{clients: make(map[*Client]bool)}
	c := &Client{hub: h, subscriptions: map[string]*subscription{}}
	h.clients[c] = true

	ch := make(chan *v1.LogEntry, 1)
	c.subscriptions["s1"] = &subscription{ch: ch, containerID: "old-container"}

	h.MigrateServerContainer("s1", "new-container")

	if got := c.subscriptions["s1"].containerID; got != "new-container" {
		t.Fatalf("subscription containerID = %q, want %q", got, "new-container")
	}
}

func TestHubMigrateServerContainerLeavesOtherServersAlone(t *testing.T) {
	h := &Hub{clients: make(map[*Client]bool)}
	c := &Client{hub: h, subscriptions: map[string]*subscription{}}
	h.clients[c] = true

	c.subscriptions["s2"] = &subscription{ch: make(chan *v1.LogEntry, 1), containerID: "s2-container"}

	h.MigrateServerContainer("s1", "new-container")

	if got := c.subscriptions["s2"].containerID; got != "s2-container" {
		t.Fatalf("unrelated subscription containerID = %q, want unchanged", got)
	}
}

func TestHubMigrateServerContainerIsSafeWithoutSubscription(t *testing.T) {
	h := &Hub{clients: make(map[*Client]bool)}
	c := &Client{hub: h, subscriptions: map[string]*subscription{}}
	h.clients[c] = true

	// Must be a no-op rather than a panic for a client with no subscription.
	h.MigrateServerContainer("s1", "new-container")
	h.MigrateServerContainer("", "new-container")
	h.MigrateServerContainer("s1", "")
}
