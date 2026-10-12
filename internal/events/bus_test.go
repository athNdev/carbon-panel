// Merged in the MINE-172 integration: this file existed independently on
// both lines of development, so the union of both test sets is kept.

package events

import (
	"context"
	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"sync"
	"testing"
)

func TestBus_FanOutInOrder(t *testing.T) {
	b := NewBus(logger.New())

	var mu sync.Mutex
	var got []string
	mk := func(name string) Handler {
		return func(_ context.Context, e Event) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, name+":"+e.ServerID)
		}
	}
	b.Subscribe(mk("a"))
	b.Subscribe(mk("b"))
	b.Subscribe(nil) // nil handlers are ignored

	b.Emit(context.Background(), Event{ServerID: "srv-1"})

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != "a:srv-1" || got[1] != "b:srv-1" {
		t.Errorf("fan-out = %v, want [a:srv-1 b:srv-1]", got)
	}
}

func TestBus_NoHandlersIsNoOp(t *testing.T) {
	b := NewBus(nil) // nil logger must not panic
	b.Emit(context.Background(), Event{ServerID: "srv-1"})
}

func TestBus_EmitPayloadPassthrough(t *testing.T) {
	b := NewBus(logger.New())

	var received Event
	b.Subscribe(func(_ context.Context, e Event) { received = e })
	b.Emit(context.Background(), Event{ServerID: "s", Data: map[string]any{"k": "v"}})

	if received.ServerID != "s" || received.Data["k"] != "v" {
		t.Errorf("payload = %+v", received)
	}
}

// --- tests present only on the pre-merge line of development ---

func TestSubscribeNilIgnored(t *testing.T) {
	b := NewBus(logger.New())
	b.Subscribe(nil)
	b.Emit(context.Background(), Event{Type: v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_START})
}

func TestEmitFansOutInOrder(t *testing.T) {
	b := NewBus(logger.New())
	var order []int
	for i := 0; i < 3; i++ {
		i := i
		b.Subscribe(func(_ context.Context, _ Event) { order = append(order, i) })
	}
	ev := Event{
		Type:     v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_PLAYER_JOIN,
		ServerID: "srv-1",
		Data:     map[string]any{"player": "steve"},
	}
	b.Emit(context.Background(), ev)
	if len(order) != 3 || order[0] != 0 || order[1] != 1 || order[2] != 2 {
		t.Fatalf("handler order=%v want [0 1 2]", order)
	}
}

func TestEmitDeliversEventPayload(t *testing.T) {
	b := NewBus(logger.New())
	var got []Event
	b.Subscribe(func(_ context.Context, e Event) { got = append(got, e) })
	b.Subscribe(func(_ context.Context, e Event) { got = append(got, e) })
	ev := Event{Type: v1.TriggeredEventType_TRIGGERED_EVENT_TYPE_SERVER_STOP, ServerID: "srv-9"}
	b.Emit(context.Background(), ev)
	if len(got) != 2 {
		t.Fatalf("deliveries=%d want 2", len(got))
	}
	for _, e := range got {
		if e.Type != ev.Type || e.ServerID != ev.ServerID {
			t.Fatalf("payload=%+v want %+v", e, ev)
		}
	}
}

func TestEmitNoHandlersNoPanic(t *testing.T) {
	b := NewBus(logger.New())
	b.Emit(context.Background(), Event{ServerID: "srv-1"})
}

func TestEmitNilLogger(t *testing.T) {
	b := NewBus(nil)
	called := false
	b.Subscribe(func(_ context.Context, _ Event) { called = true })
	b.Emit(context.Background(), Event{ServerID: "srv-1"})
	if !called {
		t.Fatal("handler should fire even with nil logger")
	}
}
