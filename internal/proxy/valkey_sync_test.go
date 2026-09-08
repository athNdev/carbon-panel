package proxy

import (
	"bufio"
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRESP_FormatAndParse(t *testing.T) {
	cmd := formatRESPCommand("SET", "foo", "bar")
	expected := "*3\r\n$3\r\nSET\r\n$3\r\nfoo\r\n$3\r\nbar\r\n"
	if cmd != expected {
		t.Fatalf("expected %q, got %q", expected, cmd)
	}

	reader := bufio.NewReader(strings.NewReader(expected))
	val, err := parseRESP(reader)
	if err != nil {
		t.Fatalf("failed to parse RESP: %v", err)
	}

	arr, ok := val.([]any)
	if !ok || len(arr) != 3 {
		t.Fatalf("expected array of 3 items, got %v", val)
	}
	if arr[0] != "SET" || arr[1] != "foo" || arr[2] != "bar" {
		t.Fatalf("unexpected items: %v", arr)
	}
}

func TestValkeySyncManager_BroadcastAndReceive(t *testing.T) {
	client := NewMockValkeyClient()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Manager on Node 1
	mgr1 := NewValkeySyncManager(client, "node-1", nil)
	defer mgr1.Close()

	// Manager on Node 2
	mgr2 := NewValkeySyncManager(client, "node-2", nil)
	defer mgr2.Close()

	var receivedEvents []*RoutingEvent
	var mu sync.Mutex
	eventReceived := make(chan struct{}, 5)

	mgr2.SetRemoteHandler(func(event *RoutingEvent) {
		mu.Lock()
		receivedEvents = append(receivedEvents, event)
		mu.Unlock()
		eventReceived <- struct{}{}
	})

	if err := mgr2.Start(ctx); err != nil {
		t.Fatalf("failed to start mgr2: %v", err)
	}

	// mgr1 broadcasts an "add" route event
	err := mgr1.BroadcastRouteAdd(ctx, "srv-1", "play.example.com", "10.0.0.5", 25565, 25565)
	if err != nil {
		t.Fatalf("failed to broadcast route add: %v", err)
	}

	// Verify route is stored in KV
	route, err := client.GetRoute(ctx, "play.example.com")
	if err != nil {
		t.Fatalf("failed to get stored route: %v", err)
	}
	if route.ServerID != "srv-1" || route.BackendHost != "10.0.0.5" {
		t.Errorf("unexpected route in KV: %+v", route)
	}

	// Wait for mgr2 to receive the event
	select {
	case <-eventReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for routing event on mgr2")
	}

	mu.Lock()
	if len(receivedEvents) != 1 {
		mu.Unlock()
		t.Fatalf("expected 1 received event, got %d", len(receivedEvents))
	}
	ev := receivedEvents[0]
	mu.Unlock()

	if ev.Action != "add" || ev.Hostname != "play.example.com" || ev.OriginNode != "node-1" {
		t.Errorf("unexpected event payload: %+v", ev)
	}

	// Test broadcast removal
	err = mgr1.BroadcastRouteRemove(ctx, "play.example.com", 25565)
	if err != nil {
		t.Fatalf("failed to broadcast route remove: %v", err)
	}

	// Verify route is deleted from KV
	_, err = client.GetRoute(ctx, "play.example.com")
	if err == nil {
		t.Errorf("expected route to be deleted from KV, but found it")
	}

	// Wait for remove event on mgr2
	select {
	case <-eventReceived:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for remove event on mgr2")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(receivedEvents) != 2 {
		t.Fatalf("expected 2 received events, got %d", len(receivedEvents))
	}
	if receivedEvents[1].Action != "remove" || receivedEvents[1].Hostname != "play.example.com" {
		t.Errorf("unexpected remove event: %+v", receivedEvents[1])
	}
}

func TestValkeySyncManager_OriginEchoFilter(t *testing.T) {
	client := NewMockValkeyClient()
	defer client.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Manager on Node 1
	mgr1 := NewValkeySyncManager(client, "node-1", nil)
	defer mgr1.Close()

	called := false
	mgr1.SetRemoteHandler(func(event *RoutingEvent) {
		called = true
	})

	if err := mgr1.Start(ctx); err != nil {
		t.Fatalf("failed to start mgr1: %v", err)
	}

	// mgr1 broadcasts an event originating from itself
	_ = mgr1.BroadcastRouteAdd(ctx, "srv-1", "echo.example.com", "127.0.0.1", 25565, 25565)

	time.Sleep(100 * time.Millisecond)
	if called {
		t.Errorf("expected mgr1 to filter out events originating from itself")
	}
}
