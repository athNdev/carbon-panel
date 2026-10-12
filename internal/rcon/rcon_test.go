// Merged in the MINE-172 integration: this file existed independently on
// both lines of development, so the union of both test sets is kept.

package rcon

import (
	"context"
	"testing"
	"time"
)

// The wrapper's only job is timeout handling around the blocking client,
// so pin both exit paths without needing a real server.
func TestSendCommand_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	_, err := SendCommand(ctx, "127.0.0.1", 25575, "pw", "list")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("cancelled call took %v", time.Since(start))
	}
}

func TestSendCommand_UnreachableHostFails(t *testing.T) {
	// Nothing listens here: connection refused must surface as an error,
	// not a hang (bounded by the dial timeout inside the client or ours).
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := SendCommand(ctx, "127.0.0.1", 1, "pw", "list"); err == nil {
		t.Error("expected error dialing closed port")
	}
}

func TestSendCommand_TimeoutWinsOverHang(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, _ = SendCommand(ctx, "10.255.255.1", 25575, "pw", "list")
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Errorf("call outlived context by far: %v", elapsed)
	}
}

// --- tests present only on the pre-merge line of development ---

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
