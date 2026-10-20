package proxy

import (
	"net"
	"testing"
	"time"
)

func TestHandshakeLimiterPerIP(t *testing.T) {
	lim := newHandshakeLimiter(3 * time.Second)
	start := time.Unix(1_000, 0)
	lim.now = func() time.Time { return start }

	if !lim.allow("10.0.0.1") {
		t.Fatal("first handshake must be allowed")
	}
	if lim.allow("10.0.0.1") {
		t.Fatal("second handshake from the same IP must wait")
	}
	if !lim.allow("10.0.0.2") {
		t.Fatal("a different IP has its own budget")
	}

	lim.now = func() time.Time { return start.Add(3 * time.Second) }
	if !lim.allow("10.0.0.1") {
		t.Fatal("the same IP is allowed after the gap")
	}
}

func TestHandshakeLimiterDisabled(t *testing.T) {
	lim := newHandshakeLimiter(0)
	if !lim.allow("10.0.0.1") || !lim.allow("10.0.0.1") {
		t.Fatal("a zero interval must not limit handshakes")
	}
}

func TestClientIP(t *testing.T) {
	addr, err := net.ResolveTCPAddr("tcp", "192.0.2.10:40000")
	if err != nil {
		t.Fatal(err)
	}
	if got := clientIP(addr); got != "192.0.2.10" {
		t.Fatalf("client IP = %q", got)
	}
}
