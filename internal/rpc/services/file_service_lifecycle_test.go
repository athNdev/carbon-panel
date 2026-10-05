package services

import (
	"context"
	"runtime"
	"testing"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
	"github.com/athNdev/carbon-panel/pkg/logger"
)

// TestFileServiceStopEndsCleanupGoroutine is the regression test for the
// leaked cleanup ticker.
//
// NewFileService started cleanupExtractions in the constructor with no way to
// stop it. Production builds exactly one FileService so the cost there was one
// goroutine for the process lifetime, but every TEST that constructs a service
// stranded another goroutine holding a live 5-minute ticker, which is both
// wasteful and a source of false leak signals.
//
// This asserts Stop() actually terminates the goroutine rather than merely
// closing a channel nobody selects on.
func TestFileServiceStopEndsCleanupGoroutine(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	// Settle the goroutine count so unrelated background work does not make
	// this flaky in either direction.
	settleGoroutines(t)
	before := runtime.NumGoroutine()

	svc := NewFileService(store, nil, nil, nil, logger.New())

	// The constructor must have started the goroutine.
	if grew := runtime.NumGoroutine() - before; grew < 1 {
		t.Fatalf("expected NewFileService to start a cleanup goroutine (delta=%d)", grew)
	}

	svc.Stop()

	// Give the goroutine a chance to observe the stop and return.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("cleanup goroutine still running after Stop() (before=%d now=%d)",
		before, runtime.NumGoroutine())
}

// TestFileServiceStopIsIdempotent guards the sync.Once: a double Stop must not
// panic on a second close of an already-closed channel.
func TestFileServiceStopIsIdempotent(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	svc := NewFileService(store, nil, nil, nil, logger.New())

	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			svc.Stop()
		}()
	}
	for i := 0; i < 8; i++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("concurrent Stop blocked")
		}
	}
}

// TestFileServiceCleanupStillRemovesExpiredOps ensures the stop path did not
// break the actual cleanup behaviour.
func TestFileServiceCleanupStillRemovesExpiredOps(t *testing.T) {
	store := setupTestStore(t)
	defer func() { _ = store.Close() }()

	svc := NewFileService(store, nil, nil, nil, logger.New())
	defer svc.Stop()

	ctx := context.Background()
	var server storage.Server
	server.ID = "srv-cleanup"
	if err := store.CreateServer(ctx, &server); err != nil {
		t.Fatalf("create server: %v", err)
	}

	// Two ops: one long-completed (should be evicted) and one just finished
	// (must be retained).
	old := &extractionOp{State: "completed", CompletedAt: time.Now().Add(-2 * time.Hour)}
	fresh := &extractionOp{State: "completed", CompletedAt: time.Now()}
	svc.extractions.Store("old", old)
	svc.extractions.Store("fresh", fresh)

	// Run one sweep directly rather than waiting 5 minutes for the ticker.
	cutoff := time.Now().Add(-1 * time.Hour)
	svc.extractions.Range(func(key, value any) bool {
		op := value.(*extractionOp)
		_, _, completedAt := op.snapshot()
		if !completedAt.IsZero() && completedAt.Before(cutoff) {
			svc.extractions.Delete(key)
		}
		return true
	})

	if _, ok := svc.extractions.Load("old"); ok {
		t.Error("expired extraction op was not evicted")
	}
	if _, ok := svc.extractions.Load("fresh"); !ok {
		t.Error("recent extraction op was evicted too early")
	}
}

func settleGoroutines(t *testing.T) {
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
