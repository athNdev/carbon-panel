package ws

import (
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
	"google.golang.org/protobuf/proto"
)

func newHandlerTestClient() *Client {
	return &Client{
		hub:           &Hub{log: logger.New()},
		send:          make(chan []byte, 16),
		subscriptions: make(map[string]*subscription),
	}
}

func drainClient(t *testing.T, c *Client) []*v1.WebSocketServerMessage {
	t.Helper()
	var out []*v1.WebSocketServerMessage
	for {
		select {
		case data := <-c.send:
			msg := &v1.WebSocketServerMessage{}
			if err := proto.Unmarshal(data, msg); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			out = append(out, msg)
		default:
			return out
		}
	}
}

func TestHandleMessage_InvalidAndUnknown(t *testing.T) {
	c := newHandlerTestClient()

	c.handleMessage([]byte("not-proto"))
	msgs := drainClient(t, c)
	if len(msgs) != 1 || msgs[0].GetError() == nil {
		t.Fatalf("invalid bytes = %v", msgs)
	}

	c.handleMessage(mustMarshalMsg(t, &v1.WebSocketClientMessage{}))
	msgs = drainClient(t, c)
	if len(msgs) != 1 || msgs[0].GetError() == nil {
		t.Fatalf("empty message = %v", msgs)
	}
}

func mustMarshalMsg(t *testing.T, msg *v1.WebSocketClientMessage) []byte {
	t.Helper()
	data, err := proto.Marshal(msg)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	return data
}

func TestHandleMessage_Dispatches(t *testing.T) {
	c := newHandlerTestClient()
	c.authenticated = true

	// PING -> PONG without any store/docker.
	c.handleMessage(mustMarshalMsg(t, &v1.WebSocketClientMessage{
		Type: v1.WSMessageType_WS_MESSAGE_TYPE_PING,
	}))
	msgs := drainClient(t, c)
	if len(msgs) != 1 || msgs[0].Type != v1.WSMessageType_WS_MESSAGE_TYPE_PONG {
		t.Fatalf("ping = %v", msgs)
	}

	// UNSUBSCRIBE with no subscription still acks.
	c.handleMessage(mustMarshalMsg(t, &v1.WebSocketClientMessage{
		Type:    v1.WSMessageType_WS_MESSAGE_TYPE_UNSUBSCRIBE,
		Payload: &v1.WebSocketClientMessage_Unsubscribe{Unsubscribe: &v1.UnsubscribeMessage{ServerId: "s1"}},
	}))
	msgs = drainClient(t, c)
	if len(msgs) != 1 || msgs[0].GetUnsubscribed() == nil {
		t.Fatalf("unsubscribe = %v", msgs)
	}

	// SUBSCRIBE while unauthenticated is rejected (no store touched).
	c.authenticated = false
	c.handleMessage(mustMarshalMsg(t, &v1.WebSocketClientMessage{
		Type:    v1.WSMessageType_WS_MESSAGE_TYPE_SUBSCRIBE,
		Payload: &v1.WebSocketClientMessage_Subscribe{Subscribe: &v1.SubscribeMessage{ServerId: "s1"}},
	}))
	msgs = drainClient(t, c)
	if len(msgs) != 1 || msgs[0].GetError() == nil {
		t.Fatalf("unauth subscribe = %v", msgs)
	}
}

func TestHandleCommand_Guards(t *testing.T) {
	c := newHandlerTestClient()

	// Unauthenticated.
	c.handleCommand(&v1.CommandMessage{ServerId: "s1", Command: "list"})
	msgs := drainClient(t, c)
	if len(msgs) != 1 || msgs[0].GetError() == nil {
		t.Fatalf("unauth command = %v", msgs)
	}

	// Authenticated but invalid payload.
	c.authenticated = true
	c.handleCommand(nil)
	c.handleCommand(&v1.CommandMessage{})
	msgs = drainClient(t, c)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 errors, got %v", msgs)
	}

	// No enforcer -> permission denied without touching docker.
	c.handleCommand(&v1.CommandMessage{ServerId: "s1", Command: "list"})
	msgs = drainClient(t, c)
	if len(msgs) != 1 || msgs[0].GetCommandResult() == nil {
		t.Fatalf("no-enforcer command = %v", msgs)
	}
	if !msgs[0].GetCommandResult().GetSuccess() == false {
		t.Errorf("expected failure result: %+v", msgs[0].GetCommandResult())
	}
}

func TestHandleUnsubscribe_AcksWithoutSubscription(t *testing.T) {
	c := newHandlerTestClient()
	c.authenticated = true

	c.handleUnsubscribe(&v1.UnsubscribeMessage{ServerId: "s1"})
	msgs := drainClient(t, c)
	if len(msgs) != 1 || msgs[0].GetUnsubscribed() == nil {
		t.Fatalf("unsubscribe = %v", msgs)
	}

	c.handleUnsubscribe(nil)
	msgs = drainClient(t, c)
	if len(msgs) != 1 || msgs[0].GetError() == nil {
		t.Fatalf("nil unsubscribe = %v", msgs)
	}
}
