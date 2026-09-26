// SPDX-License-Identifier: MPL-2.0
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at https://mozilla.org/MPL/2.0/.


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
