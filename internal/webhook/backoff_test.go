package webhook

import (
	"context"
	"math"
	"testing"
	"time"
)

// TestComputeBackoffNeverOverflows is the regression test for the retry-storm
// defect.
//
// The delay used to be `base * 2^(attempt-1)` evaluated as a Go shift. Past
// attempt 63 the shift overflows int64 and the duration goes NEGATIVE;
// time.After(negative) fires immediately, so the exponential backoff became a
// tight retry loop hammering the target endpoint.
func TestComputeBackoffNeverOverflows(t *testing.T) {
	// The boundary matters: 1<<62 is the largest power of two that still fits
	// in a positive int64, so 1<<63 is where the old expression went negative.
	for _, attempt := range []int{1, 2, 30, 62, 63, 64, 65, 100, 1000, math.MaxInt32} {
		t.Run("attempt "+itoa(attempt), func(t *testing.T) {
			for _, base := range []int{1, 1000, 60000} {
				d := computeBackoff(attempt, base)
				if d < 0 {
					t.Fatalf("attempt %d base %d: negative delay %v", attempt, base, d)
				}
				if d > maxBackoff {
					t.Fatalf("attempt %d base %d: delay %v exceeds cap %v", attempt, base, d, maxBackoff)
				}
			}
		})
	}
}

// TestComputeBackoffIsMonotonic confirms the delay never shrinks as attempts
// grow, which is the whole point of a backoff.
func TestComputeBackoffIsMonotonic(t *testing.T) {
	prev := time.Duration(0)
	for attempt := 1; attempt <= 40; attempt++ {
		d := computeBackoff(attempt, 1000)
		if d < prev {
			t.Fatalf("attempt %d: delay decreased from %v to %v", attempt, prev, d)
		}
		prev = d
	}
	// Once capped it must stay capped.
	if got := computeBackoff(60, 1000); got != maxBackoff {
		t.Fatalf("expected cap at high attempts, got %v", got)
	}
}

// TestComputeBackoffDefaultsAndZeroBase guards against a zero or negative
// configured base producing a busy loop.
func TestComputeBackoffDefaultsAndZeroBase(t *testing.T) {
	for _, base := range []int{0, -1, -1000} {
		d := computeBackoff(1, base)
		if d != time.Duration(defaultRetryBaseMs)*time.Millisecond {
			t.Fatalf("base %d: expected default base delay, got %v", base, d)
		}
		if d <= 0 {
			t.Fatalf("base %d: expected positive delay, got %v", base, d)
		}
	}
}

// TestDeliverClampsMaxRetries proves an absurd MaxRetries cannot turn into an
// unbounded number of delivery attempts.
//
// No live HTTP target is used: the SSRF guard in utils.NewSafeHTTPClient
// refuses loopback, so each attempt fails immediately without a request. That
// is exactly the shape needed to prove the loop converges - before the clamp
// the loop body was a no-op iteration, so MaxInt32 retries would spin for a
// very long time with zero network traffic.
func TestDeliverClampsMaxRetries(t *testing.T) {
	for _, retries := range []int{math.MaxInt32, math.MaxInt, 1000, 1000000} {
		t.Run(itoa(retries), func(t *testing.T) {
			cfg := Config{
				URL:          "http://127.0.0.1:1/never-reached",
				MaxRetries:   retries,
				RetryDelayMs: 1,
			}
			start := time.Now()
			res := Deliver(context.Background(), cfg, &Payload{Event: "test"})
			elapsed := time.Since(start)

			if res.Attempts > MaxRetryAttempts {
				t.Fatalf("MaxRetries=%d produced %d attempts, want <= %d", retries, res.Attempts, MaxRetryAttempts)
			}
			if res.Attempts < 1 {
				t.Fatal("expected at least one attempt")
			}
			// A retry storm would burn through this many no-op iterations long
			// before finishing.
			if elapsed > 10*time.Second {
				t.Fatalf("MaxRetries=%d: Deliver took %v; retry loop did not converge", retries, elapsed)
			}
		})
	}
}

// TestDeliverRespectsContextCancellation ensures the bounded backoff still
// aborts promptly when the caller cancels, instead of sitting out the delay.
func TestDeliverRespectsContextCancellation(t *testing.T) {
	cfg := Config{
		URL: "http://127.0.0.1:1/never-reached", MaxRetries: 5,
		RetryDelayMs: 30000,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	res := Deliver(ctx, cfg, &Payload{Event: "test"})
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Deliver ignored context cancellation (took %v)", elapsed)
	}
	if res.ErrorMessage == "" {
		t.Fatal("expected a cancellation error to be reported")
	}
}

// TestBackoffJitterVaries guards against the classic mistake of seeding a
// package-level rand deterministically, which would make jitter constant and
// pointless.
func TestBackoffJitterVaries(t *testing.T) {
	seen := make(map[float64]struct{})
	for i := 0; i < 100; i++ {
		j := backoffJitter()
		if j < 0 || j >= 1 {
			t.Fatalf("jitter %v out of range [0,1)", j)
		}
		seen[j] = struct{}{}
	}
	if len(seen) < 50 {
		t.Fatalf("jitter looks deterministic: only %d distinct values in 100 draws", len(seen))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		return "-" + string(b)
	}
	return string(b)
}
