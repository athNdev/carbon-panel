package auth

import (
	"sync"
	"time"
)

// LoginThrottle is a small in-memory limiter that slows online password
// guessing. Callers key it by client IP and by username; a key is blocked for
// a cooldown once it exceeds the failure threshold inside the window.
//
// It is intentionally in-memory: the panel is a single process, and a restart
// clearing the counters is an acceptable trade-off against adding write load to
// the database on every failed login.
type LoginThrottle struct {
	mu      sync.Mutex
	entries map[string]*throttleEntry

	max     int
	window  time.Duration
	lockout time.Duration

	// now is injectable for tests.
	now func() time.Time
}

type throttleEntry struct {
	failures     int
	windowStart  time.Time
	blockedUntil time.Time
}

// maxThrottleEntries bounds memory: a flood of unique keys must not grow the
// map without limit. When exceeded the table is cleared, which can only make
// the limiter more permissive, never less.
const maxThrottleEntries = 100000

// NewLoginThrottle returns a throttle allowing 5 failures per 5 minutes before
// a 15 minute lockout.
func NewLoginThrottle() *LoginThrottle {
	return &LoginThrottle{
		entries: make(map[string]*throttleEntry),
		max:     5,
		window:  5 * time.Minute,
		lockout: 15 * time.Minute,
		now:     time.Now,
	}
}

// Allow reports whether an attempt for key may proceed and, if not, the time
// remaining in the lockout.
func (t *LoginThrottle) Allow(key string) (bool, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()

	e := t.entries[key]
	if e == nil {
		return true, 0
	}
	now := t.now()
	if now.Before(e.blockedUntil) {
		return false, e.blockedUntil.Sub(now)
	}
	return true, 0
}

// Failure records a failed attempt for key and returns the lockout duration if
// the key was just blocked.
func (t *LoginThrottle) Failure(key string) time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.entries) >= maxThrottleEntries {
		t.clearExpiredLocked(t.now())
		if len(t.entries) >= maxThrottleEntries {
			t.entries = make(map[string]*throttleEntry)
		}
	}

	now := t.now()
	e := t.entries[key]
	if e == nil || now.Sub(e.windowStart) > t.window {
		e = &throttleEntry{windowStart: now}
		t.entries[key] = e
	}

	e.failures++
	if e.failures >= t.max {
		e.blockedUntil = now.Add(t.lockout)
		e.failures = 0
		e.windowStart = now
		return t.lockout
	}
	return 0
}

// Reset clears the failure state for key after a successful login.
func (t *LoginThrottle) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.entries, key)
}

func (t *LoginThrottle) clearExpiredLocked(now time.Time) {
	for k, e := range t.entries {
		if now.After(e.blockedUntil) && now.Sub(e.windowStart) > t.window {
			delete(t.entries, k)
		}
	}
}
