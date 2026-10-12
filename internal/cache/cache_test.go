// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


// Merged in the MINE-172 integration: this file existed independently on
// both lines of development, so the union of both test sets is kept.

package cache

import (
	"testing"
	"time"
)

func TestTTLCache_GetSetDelete(t *testing.T) {
	c := NewTTLCache[string, string]()

	if _, ok := c.Get("missing"); ok {
		t.Error("missing key hit")
	}

	c.Set("k", "v", time.Minute)
	if got, ok := c.Get("k"); !ok || got != "v" {
		t.Errorf("get = %q, %v", got, ok)
	}

	c.Delete("k")
	if _, ok := c.Get("k"); ok {
		t.Error("deleted key still hit")
	}
	// Deleting a missing key must not panic.
	c.Delete("missing")
}

func TestTTLCache_Expiry(t *testing.T) {
	c := NewTTLCache[string, int]()

	c.Set("short", 1, 20*time.Millisecond)
	c.Set("long", 2, time.Hour)

	time.Sleep(50 * time.Millisecond)
	if _, ok := c.Get("short"); ok {
		t.Error("expired entry hit")
	}
	if got, ok := c.Get("long"); !ok || got != 2 {
		t.Errorf("valid entry = %d, %v", got, ok)
	}

	c.Set("gone", 3, time.Nanosecond)
	time.Sleep(10 * time.Millisecond)
	c.CleanExpired()
}

func TestTTLCache_Clear(t *testing.T) {
	c := NewTTLCache[int, string]()
	c.Set(1, "a", time.Minute)
	c.Set(2, "b", time.Minute)
	c.Clear()
	if _, ok := c.Get(1); ok {
		t.Error("entry survived Clear")
	}
	if _, ok := c.Get(2); ok {
		t.Error("entry survived Clear")
	}
}

// --- tests present only on the pre-merge line of development ---

func TestSetGetRoundTrip(t *testing.T) {
	c := NewTTLCache[string, int]()
	if _, ok := c.Get("missing"); ok {
		t.Fatal("missing key must miss")
	}
	c.Set("a", 42, time.Minute)
	if v, ok := c.Get("a"); !ok || v != 42 {
		t.Fatalf("v=%d ok=%v", v, ok)
	}
	// Overwrite.
	c.Set("a", 7, time.Minute)
	if v, ok := c.Get("a"); !ok || v != 7 {
		t.Fatalf("v=%d ok=%v", v, ok)
	}
}

func TestExpiry(t *testing.T) {
	c := NewTTLCache[string, string]()
	c.Set("old", "x", 20*time.Millisecond)
	if _, ok := c.Get("old"); !ok {
		t.Fatal("fresh entry must hit")
	}
	time.Sleep(50 * time.Millisecond)
	if _, ok := c.Get("old"); ok {
		t.Fatal("expired entry must miss")
	}
	// Zero TTL expires immediately.
	c.Set("zero", "x", 0)
	time.Sleep(time.Millisecond)
	if _, ok := c.Get("zero"); ok {
		t.Fatal("zero-TTL entry must miss")
	}
}

func TestDelete(t *testing.T) {
	c := NewTTLCache[int, string]()
	c.Set(1, "one", time.Minute)
	c.Delete(1)
	if _, ok := c.Get(1); ok {
		t.Fatal("deleted entry must miss")
	}
	c.Delete(999) // no-op, must not panic
}

func TestClear(t *testing.T) {
	c := NewTTLCache[string, int]()
	c.Set("a", 1, time.Minute)
	c.Set("b", 2, time.Minute)
	c.Clear()
	if _, ok := c.Get("a"); ok {
		t.Fatal("cleared entry must miss")
	}
	if _, ok := c.Get("b"); ok {
		t.Fatal("cleared entry must miss")
	}
}

func TestCleanExpired(t *testing.T) {
	c := NewTTLCache[string, int]()
	c.Set("fresh", 1, time.Minute)
	c.Set("stale", 2, 20*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	c.CleanExpired()
	if _, ok := c.Get("fresh"); !ok {
		t.Fatal("fresh entry must survive")
	}
	c.mu.RLock()
	_, leaked := c.items["stale"]
	c.mu.RUnlock()
	if leaked {
		t.Fatal("stale entry must be evicted from the map")
	}
}

func TestGenericValueTypes(t *testing.T) {
	c := NewTTLCache[string, []string]()
	c.Set("k", []string{"a", "b"}, time.Minute)
	if v, ok := c.Get("k"); !ok || len(v) != 2 {
		t.Fatalf("v=%v ok=%v", v, ok)
	}
}
