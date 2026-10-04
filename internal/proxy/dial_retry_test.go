package proxy

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// blackholeAddr returns an address that will refuse/never complete a TCP
// connection, so the retry path is exercised.
func blackholeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	// Closing the listener leaves a port nothing is listening on, so dial
	// fails fast with connection refused.
	if err := l.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}
	return addr
}

// TestDialBackendWithRetryAbortsOnCancelledContext is the regression test.
//
// The retry loop used a plain time.Sleep between attempts and never consulted
// the proxy context, so a shutdown left the per-connection goroutine parked for
// the entire retry budget (5 attempts x 2s dial + 4 x 100ms) before giving up.
func TestDialBackendWithRetryAbortsOnCancelledContext(t *testing.T) {
	addr := blackholeAddr(t)

	for _, tc := range []struct {
		name string
		// cancelAfter delays cancellation so it lands mid-retry rather than
		// before the first attempt.
		cancelAfter time.Duration
	}{
		{"cancelled before first attempt", 0},
		{"cancelled mid-retry", 50 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancelAfter > 0 {
				go func() {
					time.Sleep(tc.cancelAfter)
					cancel()
				}()
			} else {
				cancel()
			}

			start := time.Now()
			conn, err := dialBackendWithRetry(ctx, addr, 5, 2*time.Second, 100*time.Millisecond)
			elapsed := time.Since(start)

			if conn != nil {
				_ = conn.Close()
				t.Fatal("expected no connection")
			}
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context.Canceled, got %v", err)
			}
			// The full budget would be ~10.4s; cancellation must cut it short.
			if elapsed > 3*time.Second {
				t.Fatalf("dial retry ignored cancellation for %v", elapsed)
			}
		})
	}
}

// TestDialBackendWithRetryReturnsOnDeadline covers a shutdown that uses a
// deadline rather than an explicit cancel.
func TestDialBackendWithRetryReturnsOnDeadline(t *testing.T) {
	addr := blackholeAddr(t)

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	start := time.Now()
	conn, err := dialBackendWithRetry(ctx, addr, 5, 2*time.Second, 100*time.Millisecond)
	elapsed := time.Since(start)

	if conn != nil {
		_ = conn.Close()
		t.Fatal("expected no connection")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if elapsed > 3*time.Second {
		t.Fatalf("dial retry ignored the deadline for %v", elapsed)
	}
}

// TestDialBackendWithRetrySucceedsOnFirstAttempt is the positive control: a
// real listener must be dialled successfully without wasted retries.
func TestDialBackendWithRetrySucceedsOnFirstAttempt(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()

	accepted := make(chan struct{}, 1)
	go func() {
		if c, err := l.Accept(); err == nil {
			accepted <- struct{}{}
			_ = c.Close()
		}
	}()

	start := time.Now()
	conn, err := dialBackendWithRetry(context.Background(), l.Addr().String(), 5, 2*time.Second, 100*time.Millisecond)
	if err != nil {
		t.Fatalf("expected a successful dial, got %v", err)
	}
	defer conn.Close()

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("first-attempt success took %v; retries were not skipped", elapsed)
	}
	select {
	case <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("backend never saw the connection")
	}
}

// TestDialBackendWithRetryRecoversAfterFailure verifies the retry actually
// retries: a listener that only starts listening after the first failed attempt
// must still be reached.
func TestDialBackendWithRetryRecoversAfterFailure(t *testing.T) {
	addr := blackholeAddr(t)

	// Bring the listener up shortly after the first attempt fails.
	later := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		l, err := net.Listen("tcp", addr)
		if err == nil {
			close(later)
			go func() {
				for {
					c, err := l.Accept()
					if err != nil {
						return
					}
					_ = c.Close()
				}
			}()
			time.Sleep(500 * time.Millisecond)
			_ = l.Close()
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	conn, err := dialBackendWithRetry(ctx, addr, 5, 100*time.Millisecond, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("expected the retry to recover, got %v", err)
	}
	_ = conn.Close()

	select {
	case <-later:
	default:
		t.Fatal("listener never came up")
	}
}

// TestDialBackendWithRetryGivesUpAfterBudget confirms it still terminates and
// reports the last dial error when nothing ever listens.
func TestDialBackendWithRetryGivesUpAfterBudget(t *testing.T) {
	addr := blackholeAddr(t)

	start := time.Now()
	conn, err := dialBackendWithRetry(context.Background(), addr, 3, 50*time.Millisecond, 20*time.Millisecond)
	elapsed := time.Since(start)

	if conn != nil {
		_ = conn.Close()
		t.Fatal("expected no connection")
	}
	if err == nil {
		t.Fatal("expected an error")
	}
	// 3 attempts x 50ms dial + 2 x 20ms backoff = ~190ms.
	if elapsed > 2*time.Second {
		t.Fatalf("retry budget took %v; backoff was not applied per attempt", elapsed)
	}
}

// TestDialBackendWithRetryClampsAttempts guards the attempts floor.
func TestDialBackendWithRetryClampsAttempts(t *testing.T) {
	addr := blackholeAddr(t)
	start := time.Now()
	// attempts=0 would otherwise mean "never try".
	conn, err := dialBackendWithRetry(context.Background(), addr, 0, 50*time.Millisecond, 10*time.Millisecond)
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil {
		t.Fatal("expected an error for an unreachable address")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("zero attempts should clamp to one, took %v", elapsed)
	}
}
