package ws

import (
	"strings"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"google.golang.org/protobuf/proto"
)

func newDropTestClient() *Client {
	return &Client{
		hub:  &Hub{log: logger.New()},
		send: make(chan []byte, 256),
	}
}

func recvServerMessage(t *testing.T, ch chan []byte) *v1.WebSocketServerMessage {
	t.Helper()
	select {
	case data := <-ch:
		msg := &v1.WebSocketServerMessage{}
		if err := proto.Unmarshal(data, msg); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
		return nil
	}
}

func logText(msg *v1.WebSocketServerMessage) string {
	if msg.Type != v1.WSMessageType_WS_MESSAGE_TYPE_LOG {
		return ""
	}
	return msg.GetLog().GetLog().GetMessage()
}

func TestSendLogDelivers(t *testing.T) {
	c := newDropTestClient()
	c.sendLog("s1", &v1.LogEntry{Message: "hello"})
	if got := logText(recvServerMessage(t, c.send)); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if n := c.droppedLogs.Load(); n != 0 {
		t.Fatalf("dropped=%d", n)
	}
}

func TestSendLogCountsDropsAndEmitsSentinel(t *testing.T) {
	c := newDropTestClient()
	// Fill the channel with control messages (priority path blocks, so fill
	// it directly to simulate a congested client).
	for i := 0; i < 256; i++ {
		c.send <- []byte{byte(i)}
	}
	// Three log lines arrive while congested: all counted, none delivered.
	for i := 0; i < 3; i++ {
		c.sendLog("s1", &v1.LogEntry{Message: "lost"})
	}
	if n := c.droppedLogs.Load(); n != 3 {
		t.Fatalf("dropped=%d want 3", n)
	}
	if got := len(c.send); got != 256 {
		t.Fatalf("channel len=%d want 256", got)
	}

	// Drain everything, then the next line flushes the sentinel first.
	for i := 0; i < 256; i++ {
		<-c.send
	}
	c.sendLog("s1", &v1.LogEntry{Message: "back"})
	first := logText(recvServerMessage(t, c.send))
	if !strings.Contains(first, "3 log lines skipped") {
		t.Fatalf("sentinel=%q", first)
	}
	if got := logText(recvServerMessage(t, c.send)); got != "back" {
		t.Fatalf("after sentinel got %q", got)
	}
	if n := c.droppedLogs.Load(); n != 0 {
		t.Fatalf("dropped=%d want reset", n)
	}
}

func TestSendLogSentinelDefersWhileCongested(t *testing.T) {
	c := newDropTestClient()
	for i := 0; i < 256; i++ {
		c.send <- []byte{byte(i)}
	}
	c.sendLog("s1", &v1.LogEntry{Message: "a"})
	// Still full: sentinel cannot flush, count grows to include this line.
	c.sendLog("s1", &v1.LogEntry{Message: "b"})
	if n := c.droppedLogs.Load(); n != 2 {
		t.Fatalf("dropped=%d want 2", n)
	}
}

func TestControlMessageWaitsForRoom(t *testing.T) {
	c := newDropTestClient()
	for i := 0; i < 256; i++ {
		c.send <- []byte{byte(i)}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.sendPong()
	}()
	// Free one slot; the blocked control send must claim it.
	<-c.send
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("control message did not take priority path")
	}
	// Drain the remaining filler; the pong was enqueued after them.
	for i := 0; i < 255; i++ {
		<-c.send
	}
	msg := recvServerMessage(t, c.send)
	if msg.Type != v1.WSMessageType_WS_MESSAGE_TYPE_PONG {
		t.Fatalf("type=%v want PONG", msg.Type)
	}
}

func TestControlMessageTimeoutIsBounded(t *testing.T) {
	old := controlSendTimeout
	controlSendTimeout = 50 * time.Millisecond
	defer func() { controlSendTimeout = old }()

	c := newDropTestClient()
	for i := 0; i < 256; i++ {
		c.send <- []byte{byte(i)}
	}
	start := time.Now()
	c.sendError("boom") // no reader: must return after ~50ms, not hang
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("control send blocked %v", elapsed)
	}
	if got := len(c.send); got != 256 {
		t.Fatalf("channel len=%d want 256 (nothing delivered)", got)
	}
}
