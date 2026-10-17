// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


package logger

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestLogger_MaxBufferEnforced(t *testing.T) {
	cfg := &Config{
		MaxBuffer: 5,
		BufferTTL: time.Hour,
	}
	l := NewWithConfig(cfg)

	for i := 1; i <= 10; i++ {
		l.Info("line %d", i)
	}

	recent := l.GetRecentLogs()
	if len(recent) != 5 {
		t.Fatalf("expected 5 buffered logs, got %d", len(recent))
	}

	// Verify last entry is line 10
	if want := "line 10"; !containsSubstring(recent[4], want) {
		t.Errorf("expected last log to contain %q, got %q", want, recent[4])
	}
}

func TestLogger_TTLEviction(t *testing.T) {
	cfg := &Config{
		MaxBuffer: 100,
		BufferTTL: 50 * time.Millisecond,
	}
	l := NewWithConfig(cfg)

	l.Info("old message")
	time.Sleep(75 * time.Millisecond)

	l.Info("new message")
	recent := l.GetRecentLogs()

	if len(recent) != 1 {
		t.Fatalf("expected 1 recent log after TTL expiry, got %d: %v", len(recent), recent)
	}
	if !containsSubstring(recent[0], "new message") {
		t.Errorf("expected recent log to be 'new message', got %q", recent[0])
	}
}

func TestLogger_GetRecentLogsSince(t *testing.T) {
	cfg := &Config{
		MaxBuffer: 100,
		BufferTTL: time.Hour,
	}
	l := NewWithConfig(cfg)

	l.Info("first")
	time.Sleep(40 * time.Millisecond)
	l.Info("second")

	logs := l.GetRecentLogsSince(25 * time.Millisecond)
	if len(logs) != 1 {
		t.Fatalf("expected 1 log in last 25ms, got %d: %v", len(logs), logs)
	}
	if !containsSubstring(logs[0], "second") {
		t.Errorf("expected 'second', got %q", logs[0])
	}
}

func TestLogger_SetBufferTTL(t *testing.T) {
	l := New()
	l.SetBufferTTL(50 * time.Millisecond)

	l.Info("message to expire")
	time.Sleep(60 * time.Millisecond)

	if got := len(l.GetRecentLogs()); got != 0 {
		t.Errorf("expected 0 logs after TTL expiration, got %d", got)
	}
}

func TestLogger_Concurrency(t *testing.T) {
	l := NewWithConfig(&Config{
		MaxBuffer: 50,
		BufferTTL: 500 * time.Millisecond,
	})

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				l.Info("worker %d message %d", workerID, j)
				_ = l.GetRecentLogs()
			}
		}(i)
	}
	wg.Wait()

	recent := l.GetRecentLogs()
	if len(recent) > 50 {
		t.Errorf("expected at most 50 logs, got %d", len(recent))
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || fmt.Sprintf("%v", s) != "" && stringContains(s, sub))
}

func stringContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
