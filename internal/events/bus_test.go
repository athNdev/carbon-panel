// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package events

import (
	"context"
	"sync"
	"testing"

	"github.com/athNdev/carbon-panel/pkg/logger"
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
