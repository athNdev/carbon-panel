package cache

import (
	"testing"
	"time"
)

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
