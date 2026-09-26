// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package logger

import (
	"strings"
	"testing"
	"time"

	v1 "github.com/athNdev/carbon-panel/pkg/proto/carbonpanel/v1"
)

func TestShouldFilterLine(t *testing.T) {
	ls := NewLogStreamer(nil, New(), 100)

	for _, spam := range []string{
		"Thread RCON Client /127.0.0.1 handling",
		"[RCON Listener #1/INFO]: Thread RCON Client started",
		"Rcon connection from /1.2.3.4",
	} {
		if !ls.shouldFilterLine(spam) {
			t.Errorf("spam not filtered: %q", spam)
		}
	}
	for _, legit := range []string{
		"[Server thread/INFO]: Done (1.23s)!",
		"Player joined the game",
		"",
	} {
		if ls.shouldFilterLine(legit) {
			t.Errorf("legit line filtered: %q", legit)
		}
	}
}

func TestCommandEntryOutputAndTail(t *testing.T) {
	ls := NewLogStreamer(nil, New(), 10)
	now := time.Now()

	ls.AddCommandEntry("c1", "list", now)
	ls.AddCommandOutput("c1", "alice, bob", true, now)

	logs := ls.GetLogs("c1", 0)
	if len(logs) != 2 {
		t.Fatalf("logs = %d entries, want 2", len(logs))
	}
	if !logs[0].IsCommand || logs[0].Message != "\u001b[0m"+"list" {
		t.Errorf("command entry = %+v", logs[0])
	}
	if logs[1].IsCommand || !strings.Contains(logs[1].Message, "alice, bob") {
		t.Errorf("output entry = %+v", logs[1])
	}

	tail := ls.GetLogs("c1", 1)
	if len(tail) != 1 || !strings.Contains(tail[0].Message, "alice, bob") {
		t.Errorf("tail = %+v", tail)
	}

	if got := ls.GetLogs("missing", 0); len(got) != 0 {
		t.Errorf("missing container logs = %v", got)
	}
}

func TestSubscribeBroadcastUnsubscribe(t *testing.T) {
	ls := NewLogStreamer(nil, New(), 100)

	ch := ls.Subscribe("c1")
	entry := &v1.LogEntry{Message: "hello"}
	ls.broadcast("c1", entry)

	select {
	case got := <-ch:
		if got.Message != "hello" {
			t.Errorf("broadcast = %q", got.Message)
		}
	default:
		t.Fatal("subscriber received nothing")
	}

	// Broadcast to no subscribers must not panic.
	ls.broadcast("nobody", entry)

	ls.Unsubscribe("c1", ch)
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("unsubscribed channel still open with data")
		}
	default:
		// Closed channels read zero immediately; an open-but-empty channel
		// would also hit default — assert closed explicitly below.
	}
	if _, ok := <-ch; ok {
		t.Error("unsubscribed channel not closed")
	}
	// Double unsubscribe must not panic.
	ls.Unsubscribe("c1", ch)
}

func TestRemoveStreamAndCleanup(t *testing.T) {
	ls := NewLogStreamer(nil, New(), 100)
	now := time.Now()

	ls.AddCommandEntry("c1", "say hi", now)
	ls.RemoveStream("c1")
	if got := ls.GetLogs("c1", 0); len(got) != 0 {
		t.Errorf("logs after RemoveStream = %d", len(got))
	}

	ls.AddCommandEntry("c2", "say hi", now)
	ls.CleanupContainer("c2")
	if got := ls.GetLogs("c2", 0); len(got) != 0 {
		t.Errorf("logs after CleanupContainer = %d", len(got))
	}

	// Unknown containers are safe no-ops.
	ls.RemoveStream("missing")
	ls.CleanupContainer("missing")

	var nilResolver LogClientResolver
	ls.SetClientResolver(nilResolver)
}
