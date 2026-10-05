package events

import (
	"context"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

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
