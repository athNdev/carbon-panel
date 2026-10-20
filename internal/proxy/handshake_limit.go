package proxy

import (
	"net"
	"sync"
	"time"
)

// handshakeLimiter spaces handshakes from one client IP. A zero interval
// allows every handshake. Denial does not move the timestamp, so a quiet
// period of one interval lets the next handshake through.
type handshakeLimiter struct {
	interval time.Duration
	mu       sync.Mutex
	last     map[string]time.Time
	now      func() time.Time
}

func newHandshakeLimiter(interval time.Duration) *handshakeLimiter {
	return &handshakeLimiter{
		interval: interval,
		last:     make(map[string]time.Time),
		now:      time.Now,
	}
}

func (l *handshakeLimiter) allow(ip string) bool {
	if l == nil || l.interval <= 0 || ip == "" {
		return true
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if prev, ok := l.last[ip]; ok && now.Sub(prev) < l.interval {
		return false
	}
	l.last[ip] = now
	if len(l.last) > 1024 {
		for key, seen := range l.last {
			if now.Sub(seen) >= l.interval {
				delete(l.last, key)
			}
		}
	}
	return true
}

func clientIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
