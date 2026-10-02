package proxy

import (
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
)

// TestGetOrCreateSessionRace hammers one session key concurrently. On the
// pre-fix code every racer that missed the read-lock lookup created its own
// backend socket and the losers' sockets (plus their response goroutines)
// leaked; the fixed code re-checks under the write lock so exactly one
// socket survives and every loser socket is closed.
func TestGetOrCreateSessionRace(t *testing.T) {
	origListen := udpListen
	defer func() { udpListen = origListen }()

	var created atomic.Int32
	var mu sync.Mutex
	var conns []*net.UDPConn
	udpListen = func(network string, laddr *net.UDPAddr) (*net.UDPConn, error) {
		c, err := origListen(network, laddr)
		if err != nil {
			return nil, err
		}
		created.Add(1)
		mu.Lock()
		conns = append(conns, c)
		mu.Unlock()
		return c, nil
	}

	p := NewUDPProxy(&Config{ListenAddr: "127.0.0.1:0", Logger: logger.New()})
	p.AddRoute("srv", "udp", "127.0.0.1", 19132)
	defer p.cancel()

	clientAddr, err := net.ResolveUDPAddr("udp", "127.0.0.1:50000")
	if err != nil {
		t.Fatal(err)
	}

	const workers = 32
	const iters = 25
	results := make([][]*udpSession, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < iters; i++ {
				s, err := p.getOrCreateSession(clientAddr)
				if err != nil {
					t.Errorf("getOrCreateSession: %v", err)
					return
				}
				results[w] = append(results[w], s)
			}
		}(w)
	}
	wg.Wait()

	// Exactly one session, and every caller got the same pointer.
	p.sessionsMu.RLock()
	sessionCount := len(p.sessions)
	stored := p.sessions[clientAddr.String()]
	p.sessionsMu.RUnlock()
	if sessionCount != 1 {
		t.Fatalf("sessions map has %d entries, want exactly 1", sessionCount)
	}
	for w := 0; w < workers; w++ {
		for i, s := range results[w] {
			if s == nil {
				t.Fatalf("worker %d iter %d got nil session", w, i)
			}
			if s != stored {
				t.Fatalf("worker %d iter %d got a losing session that should have been discarded", w, i)
			}
		}
	}

	// Every created socket except the winner's must already be closed by the
	// loser path: a second Close on a closed socket errors, while closing the
	// live winner succeeds.
	mu.Lock()
	defer mu.Unlock()
	if len(conns) == 0 {
		t.Fatal("no backend sockets were created at all")
	}
	t.Logf("backend sockets created for %d racing lookups: %d", workers*iters, len(conns))
	losers := 0
	for _, c := range conns {
		if c == stored.backendConn {
			continue
		}
		losers++
		if err := c.Close(); err == nil {
			t.Errorf("loser backend socket still open: race loser did not clean up")
		}
	}
	if losers != len(conns)-1 {
		t.Errorf("winner socket accounting wrong: %d conns, %d losers", len(conns), losers)
	}

	// The winner's socket must still be live until session removal.
	if err := stored.backendConn.Close(); err != nil {
		t.Errorf("winner backend socket should be open, Close failed: %v", err)
	}
	p.removeSession(clientAddr.String())
}

// TestGetOrCreateSessionNoBackend ensures the error path creates no socket.
func TestGetOrCreateSessionNoBackend(t *testing.T) {
	origListen := udpListen
	defer func() { udpListen = origListen }()
	calls := 0
	udpListen = func(network string, laddr *net.UDPAddr) (*net.UDPConn, error) {
		calls++
		return origListen(network, laddr)
	}

	p := NewUDPProxy(&Config{ListenAddr: "127.0.0.1:0", Logger: logger.New()})
	defer p.cancel()
	addr, _ := net.ResolveUDPAddr("udp", fmt.Sprintf("127.0.0.1:50001"))
	if _, err := p.getOrCreateSession(addr); err == nil {
		t.Fatal("expected no-backend error")
	}
	if calls != 0 {
		t.Errorf("created %d sockets despite missing backend", calls)
	}
}
