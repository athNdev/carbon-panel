package reconciler

import (
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// TestDebouncer_CoalescesBurst verifies rapid enqueues for one server collapse
// into a single convergence pass after quiet window (MINE-137).
func TestDebouncer_CoalescesBurst(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDebouncer(300 * time.Millisecond)
		defer d.stop()

		var calls atomic.Int32
		fire := func() { calls.Add(1) }

		d.enqueue("srv-1", fire)
		d.enqueue("srv-1", fire)
		d.enqueue("srv-1", fire)

		time.Sleep(100 * time.Millisecond)
		synctest.Wait()
		if got := calls.Load(); got != 0 {
			t.Fatalf("fired inside debounce window: got %d calls, want 0", got)
		}

		time.Sleep(300 * time.Millisecond)
		synctest.Wait()
		if got := calls.Load(); got != 1 {
			t.Fatalf("burst coalescing: got %d calls, want 1", got)
		}
	})
}

// TestDebouncer_DistinctKeysFireIndependently verifies different servers each
// get their own pass.
func TestDebouncer_DistinctKeysFireIndependently(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		d := newDebouncer(100 * time.Millisecond)
		defer d.stop()

		var mu sync.Mutex
		fired := map[string]int{}
		d.enqueue("a", func() {
			mu.Lock()
			fired["a"]++
			mu.Unlock()
		})
		d.enqueue("b", func() {
			mu.Lock()
			fired["b"]++
			mu.Unlock()
		})

		time.Sleep(200 * time.Millisecond)
		synctest.Wait()

		mu.Lock()
		defer mu.Unlock()
		if fired["a"] != 1 || fired["b"] != 1 {
			t.Fatalf("distinct keys: got %v, want map[a:1 b:1]", fired)
		}
	})
}
