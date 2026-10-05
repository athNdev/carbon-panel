package ws

import (
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// newUnregisterTestHub builds a hub wired just enough for Run()'s register and
// unregister branches.
func newUnregisterTestHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		log:        logger.New(),
	}
}

func newUnregisterTestClient(h *Hub) *Client {
	return &Client{
		hub:           h,
		send:          make(chan []byte, 256),
		done:          make(chan struct{}),
		subscriptions: make(map[string]*subscription),
	}
}

// TestUnregisterThenSendDoesNotPanic is the regression test for the
// send-on-closed-channel defect.
//
// Hub.Run's unregister branch used to call close(client.send). Any log
// forwarder still draining the streamer buffer could then send on that closed
// channel, and a send on a closed channel panics even inside a select — taking
// down the entire process, not just the offending connection.
//
// The invariant is now: unregister closes done (once), never send, and every
// send path observes done before writing. This test drives the REAL unregister
// path through Hub.Run and then hammers every send path. Against the old code
// it panics; against the current code it must return cleanly.
func TestUnregisterThenSendDoesNotPanic(t *testing.T) {
	for attempt := 0; attempt < 20; attempt++ {
		h := newUnregisterTestHub()
		go h.Run()

		c := newUnregisterTestClient(h)
		h.register <- c

		// Confirm the hub registered the client before disconnecting; the
		// unregister branch is a no-op for an unknown client.
		deadline := time.Now().Add(2 * time.Second)
		for {
			h.clientsMu.RLock()
			_, ok := h.clients[c]
			h.clientsMu.RUnlock()
			if ok {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("attempt %d: client was never registered", attempt)
			}
			time.Sleep(time.Millisecond)
		}

		// Real disconnect path.
		h.unregister <- c

		// Now hammer every send path. With done closed these must all drop
		// promptly; with send closed the very first one panics.
		work := make(chan struct{})
		go func() {
			defer close(work)
			for i := 0; i < 500; i++ {
				c.sendLog("s1", &v1.LogEntry{Message: "after disconnect"})
				c.sendMessage(&v1.WebSocketServerMessage{
					Type:    v1.WSMessageType_WS_MESSAGE_TYPE_LOGS,
					Payload: &v1.WebSocketServerMessage_Logs{Logs: &v1.LogsMessage{ServerId: "s1"}},
				})
				c.sendLogs("s1", []*v1.LogEntry{{Message: "batch"}})
			}
		}()

		select {
		case <-work:
		case <-time.After(5 * time.Second):
			t.Fatalf("attempt %d: send path blocked after unregister", attempt)
		}

		// A forwarder started after the disconnect must exit, not linger.
		logCh := make(chan *v1.LogEntry)
		exited := make(chan struct{})
		go func() {
			defer close(exited)
			c.forwardLogs("s1", logCh)
		}()
		select {
		case <-exited:
		case <-time.After(2 * time.Second):
			t.Fatalf("attempt %d: forwardLogs did not exit after unregister", attempt)
		}
	}
}

// TestUnregisterUnderConcurrentForwardingDoesNotPanic overlaps the disconnect
// with an active log forwarder, which is the exact production interleaving that
// produced the crash. Run under -race.
func TestUnregisterUnderConcurrentForwardingDoesNotPanic(t *testing.T) {
	h := newUnregisterTestHub()
	go h.Run()

	c := newUnregisterTestClient(h)
	h.register <- c

	logCh := make(chan *v1.LogEntry, 256)
	forwarderDone := make(chan struct{})
	go func() {
		defer close(forwarderDone)
		c.forwardLogs("s1", logCh)
	}()

	// Keep feeding entries while the disconnect lands.
	feedDone := make(chan struct{})
	go func() {
		defer close(feedDone)
		for i := 0; i < 5000; i++ {
			select {
			case logCh <- &v1.LogEntry{Message: "line"}:
			case <-c.done:
				// Disconnect won; keep pushing past it to prove the
				// forwarder cannot write to a closed send channel.
				for j := 0; j < 200; j++ {
					c.sendLog("s1", &v1.LogEntry{Message: "post-disconnect"})
				}
				return
			case <-time.After(200 * time.Millisecond):
				return
			}
		}
	}()

	// Disconnect concurrently with the feed.
	disconnectDone := make(chan struct{})
	go func() {
		defer close(disconnectDone)
		time.Sleep(2 * time.Millisecond)
		h.unregister <- c
	}()

	select {
	case <-disconnectDone:
	case <-time.After(5 * time.Second):
		t.Fatal("unregister blocked")
	}
	select {
	case <-feedDone:
	case <-time.After(10 * time.Second):
		t.Fatal("log feeder blocked after unregister")
	}
	select {
	case <-forwarderDone:
	case <-time.After(5 * time.Second):
		t.Fatal("forwardLogs goroutine leaked after unregister")
	}
}

// TestCloseOnceIsIdempotent guards the sync.Once: repeated unregister must not
// panic on a second close of an already-closed channel.
func TestCloseOnceIsIdempotent(t *testing.T) {
	c := newUnregisterTestClient(newUnregisterTestHub())

	done := make(chan struct{})
	for i := 0; i < 32; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			c.closeOnce.Do(func() { close(c.done) })
		}()
	}
	for i := 0; i < 32; i++ {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent closeOnce goroutine blocked")
		}
	}
}
