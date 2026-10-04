package module

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

// TestSleepCtxIsCancellable is the regression test for the hook delay. Hooks
// used to wait with an uninterruptible time.Sleep, so a hook configured with a
// long DelaySeconds pinned a goroutine for that entire duration and ignored
// shutdown and cancellation completely.
func TestSleepCtxIsCancellable(t *testing.T) {
	t.Run("returns true after the delay", func(t *testing.T) {
		start := time.Now()
		if !sleepCtx(context.Background(), 20*time.Millisecond) {
			t.Fatal("expected sleep to complete")
		}
		if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
			t.Fatalf("returned too early after %v", elapsed)
		}
	})

	t.Run("returns false immediately when cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		start := time.Now()
		if sleepCtx(ctx, time.Hour) {
			t.Fatal("expected sleep to report cancellation")
		}
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("cancelled sleep blocked for %v; it was not interruptible", elapsed)
		}
	})

	t.Run("zero and negative durations do not block", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if !sleepCtx(ctx, 0) {
			t.Fatal("zero duration should be a no-op success")
		}
		if !sleepCtx(ctx, -time.Second) {
			t.Fatal("negative duration should be a no-op success")
		}
	})

	t.Run("does not leak a timer on early return", func(t *testing.T) {
		// Repeated early cancellation must not accumulate armed timers.
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		for i := 0; i < 200; i++ {
			sleepCtx(ctx, time.Hour)
		}
	})
}

// TestHookSemaphoreBoundsConcurrency proves the shared slot actually caps
// concurrent module lifecycle actions, and that waiting callers are released
// rather than dropped.
func TestHookSemaphoreBoundsConcurrency(t *testing.T) {
	m := &Manager{hookSem: make(chan struct{}, maxConcurrentHookActions)}

	// Fill every slot.
	for i := 0; i < maxConcurrentHookActions; i++ {
		if !m.acquireHookSlot(context.Background()) {
			t.Fatalf("slot %d should have been free", i)
		}
	}

	// The next acquire must block until something is released.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if m.acquireHookSlot(ctx) {
		t.Fatal("acquired a slot beyond the concurrency cap")
	}

	// Releasing one lets exactly one waiter through.
	m.releaseHookSlot()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer waitCancel()
	if !m.acquireHookSlot(waitCtx) {
		t.Fatal("a released slot was not handed to a waiter")
	}

	m.releaseHookSlot()
	for i := 0; i < maxConcurrentHookActions; i++ {
		m.releaseHookSlot()
	}
}

// TestHookSemaphoreReleasesAreSafe guards releaseHookSlot against
// over-draining: it must never block or panic when called more often than
// acquire.
func TestHookSemaphoreReleasesAreSafe(t *testing.T) {
	m := &Manager{hookSem: make(chan struct{}, 2)}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 10; i++ {
			m.releaseHookSlot()
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("releaseHookSlot blocked when over-drained")
	}

	// The semaphore must still be usable afterwards.
	if !m.acquireHookSlot(context.Background()) {
		t.Fatal("semaphore unusable after over-draining")
	}
}

// TestHookSemaphoreNilIsSafe covers a Manager built without the semaphore
// (defensive path) - it must not panic.
func TestHookSemaphoreNilIsSafe(t *testing.T) {
	m := &Manager{}
	if !m.acquireHookSlot(context.Background()) {
		t.Fatal("nil semaphore should allow the action")
	}
	m.releaseHookSlot()
}

// TestHookSlotRespectsCancellation asserts a cancelled context does not leave a
// caller parked on the semaphore.
func TestHookSlotRespectsCancellation(t *testing.T) {
	m := &Manager{hookSem: make(chan struct{}, 1)}
	if !m.acquireHookSlot(context.Background()) {
		t.Fatal("first acquire should succeed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	if m.acquireHookSlot(ctx) {
		t.Fatal("acquired while cancelled")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("acquireHookSlot ignored cancellation for %v", elapsed)
	}
	m.releaseHookSlot()
}

// TestHookFanoutDoesNotLeakGoroutines exercises the concurrent path the way a
// burst of events would, checking goroutine count returns to baseline.
func TestHookFanoutDoesNotLeakGoroutines(t *testing.T) {
	m := &Manager{hookSem: make(chan struct{}, maxConcurrentHookActions)}

	settleModuleGoroutines(t)
	before := runtime.NumGoroutine()

	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := context.Background()
			if !m.acquireHookSlot(ctx) {
				return
			}
			defer m.releaseHookSlot()
			_ = sleepCtx(ctx, time.Millisecond)
		}()
	}
	wg.Wait()

	settleModuleGoroutines(t)
	if after := runtime.NumGoroutine(); after > before+8 {
		t.Fatalf("goroutine count grew from %d to %d", before, after)
	}
}

func settleModuleGoroutines(t *testing.T) {
	t.Helper()
	prev := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		runtime.GC()
		time.Sleep(20 * time.Millisecond)
		if runtime.NumGoroutine() <= prev {
			return
		}
		prev = runtime.NumGoroutine()
	}
}
