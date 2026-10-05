package rcon

import (
	"context"
	"testing"
	"time"
)

func TestSendCommandCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Unroutable host: dial blocks, so the already-cancelled context wins.
	_, err := SendCommand(ctx, "10.255.255.1", 25575, "pw", "list")
	if err != context.Canceled {
		t.Fatalf("err=%v want context.Canceled", err)
	}
}

func TestSendCommandRefusedHostErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Nothing listens on port 1: dial fails fast with a wrapped error.
	if _, err := SendCommand(ctx, "127.0.0.1", 1, "pw", "list"); err == nil {
		t.Fatal("want error for connection-refused host")
	}
}
