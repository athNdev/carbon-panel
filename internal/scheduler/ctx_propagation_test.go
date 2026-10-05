package scheduler

import (
	"context"
	"testing"
	"time"

	storage "github.com/athNdev/carbon-panel/internal/db"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

// TestExecuteTaskRespectsCallerCancellation is the regression test for the
// dropped context.
//
// executeTaskAtDepth built its own context.Background(), so the caller's ctx
// never reached the task body. A manual TriggerTask from an RPC therefore could
// not be cancelled: the work outlived the request that started it, and a
// client disconnect had no effect.
func TestExecuteTaskRespectsCallerCancellation(t *testing.T) {
	s, store, ctx, server := setupChainTest(t)

	parent := mkTask(server.ID, "parent", storage.TaskTypeCommand)
	parent.RequireOnline = false
	if err := store.CreateScheduledTask(ctx, parent); err != nil {
		t.Fatal(err)
	}
	// One child with a 10 minute inter-step offset. Without a cancellable wait
	// the chain sleeps it out regardless of cancellation.
	child := mkTask(server.ID, "child", storage.TaskTypeCommand)
	child.RequireOnline = false
	pid := parent.ID
	child.ParentTaskID = &pid
	child.TimeOffsetSecs = 600
	if err := store.CreateScheduledTask(ctx, child); err != nil {
		t.Fatal(err)
	}

	cancelCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = s.executeTask(cancelCtx, parent, "manual", v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_UNSPECIFIED, nil)
	}()

	// Let the chain reach the inter-step wait, then cancel.
	time.Sleep(400 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("task chain ignored context cancellation; it is still running")
	}

	// The cancelled child must not have executed.
	if n := countExecutions(t, store, ctx, child.ID); n != 0 {
		t.Fatalf("cancelled child ran %d time(s); cancellation did not stop the chain", n)
	}
}

// TestRunChildChainStopsOnCancelledContext covers the wait in isolation: an
// already-cancelled context must abandon the pending offset immediately.
func TestRunChildChainStopsOnCancelledContext(t *testing.T) {
	s, _, ctx, server := setupChainTest(t)

	parent := &storage.ScheduledTask{ID: "p1", ServerID: server.ID, Name: "p"}

	cancelled, cancel := context.WithCancel(ctx)
	cancel() // already cancelled

	start := time.Now()
	s.runChildChain(cancelled, parent, "manual", v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_UNSPECIFIED, nil, 0)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("runChildChain took %v on an already-cancelled context", elapsed)
	}
}

// TestTriggerTaskThreadsCallerContext asserts TriggerTask no longer detaches
// from its caller. A cancelled context must abort before any execution record
// is written, rather than running to completion regardless.
func TestTriggerTaskThreadsCallerContext(t *testing.T) {
	s, store, ctx, server := setupChainTest(t)

	task := mkTask(server.ID, "cancelled-trigger", storage.TaskTypeCommand)
	task.RequireOnline = false
	if err := store.CreateScheduledTask(ctx, task); err != nil {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	if _, err := s.TriggerTask(cancelled, task.ID); err == nil {
		t.Fatal("expected TriggerTask to fail on a cancelled context")
	}

	// Give any stray background work a moment; it must not record an execution.
	time.Sleep(300 * time.Millisecond)
	if n := countExecutions(t, store, ctx, task.ID); n != 0 {
		t.Fatalf("cancelled TriggerTask recorded %d execution(s)", n)
	}
}
