package ws

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

func newStopTestHub() *Hub {
	return &Hub{
		log:        logger.New(),
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		quit:       make(chan struct{}),
		// Keep the bounded-wait tests fast; production uses the 5s default.
		registerTimeout: 150 * time.Millisecond,
	}
}

// TestSendRegisterDoesNotBlockWithoutRunLoop is the regression test for the hang.
//
// ServeHTTP did `h.register <- client` on an UNBUFFERED channel with no escape.
// If the Run loop was not running - or had exited - that send never completed and
// the HTTP handler goroutine hung forever, leaking a goroutine and an upgraded
// connection on every such request.
func TestSendRegisterDoesNotBlockWithoutRunLoop(t *testing.T) {
	h := newStopTestHub()
	// NOTE: Run() deliberately NOT started.

	client := &Client{hub: h, done: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		if h.sendRegister(context.Background(), client) {
			t.Error("a client must not be accepted when no Run loop is consuming")
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("register blocked with no Run loop; ServeHTTP would hang the handler")
	}
}

// TestSendRegisterRespectsCancelledRequestContext covers the third arm of the
// select: a client that disconnects mid-handshake must not be left parked.
func TestSendRegisterRespectsCancelledRequestContext(t *testing.T) {
	h := newStopTestHub() // no Run loop
	client := &Client{hub: h, done: make(chan struct{})}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		if h.sendRegister(ctx, client) {
			t.Error("expected sendRegister to report failure on a cancelled request")
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sendRegister ignored request cancellation")
	}
}

// TestSendRegisterReturnsAfterStop covers the other direction: the loop was
// running, then Stop is called. Producers must be released, not stranded.
func TestSendRegisterReturnsAfterStop(t *testing.T) {
	h := newStopTestHub()
	go h.Run()
	time.Sleep(50 * time.Millisecond)
	h.Stop()

	client := &Client{hub: h, done: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.sendRegister(context.Background(), client)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("register blocked after Stop; producers are stranded")
	}
}

// TestSendUnregisterReturnsAfterStop covers the readPump teardown send, which
// had the identical blocking hazard.
func TestSendUnregisterReturnsAfterStop(t *testing.T) {
	h := newStopTestHub()
	go h.Run()
	time.Sleep(50 * time.Millisecond)
	h.Stop()

	client := &Client{hub: h, done: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.sendUnregister(client)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("unregister blocked after Stop")
	}
}

// TestStopIsIdempotent guards the sync.Once: repeated Stop must not panic on a
// second close of an already-closed channel.
func TestStopIsIdempotent(t *testing.T) {
	h := newStopTestHub()
	go h.Run()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h.Stop()
		}()
	}
	wg.Wait()
}

// TestStopClosesConnectedClients verifies Stop actually releases the client
// pumps rather than only closing the quit channel.
func TestStopClosesConnectedClients(t *testing.T) {
	h := newStopTestHub()
	go h.Run()

	c := &Client{
		hub:  h,
		send: make(chan []byte, 1),
		done: make(chan struct{}),
	}

	select {
	case h.register <- c:
	case <-time.After(5 * time.Second):
		t.Fatal("could not register client")
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		h.clientsMu.RLock()
		_, ok := h.clients[c]
		h.clientsMu.RUnlock()
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("client was never registered")
		}
		time.Sleep(5 * time.Millisecond)
	}

	h.Stop()

	select {
	case <-c.done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not close the client's done channel; pumps would linger")
	}
}

// TestRunReturnsAfterStop asserts the loop itself actually exits.
func TestRunReturnsAfterStop(t *testing.T) {
	h := newStopTestHub()
	exited := make(chan struct{})
	go func() {
		h.Run()
		close(exited)
	}()
	time.Sleep(50 * time.Millisecond)
	h.Stop()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after Stop")
	}
}
