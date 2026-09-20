package metrics

import (
	"testing"
	"time"
)

func TestPruneStale(t *testing.T) {
	c := &Collector{metrics: map[string]*ServerMetrics{
		"fresh": {ServerID: "fresh", LastUpdated: time.Now()},
		"stale": {ServerID: "stale", LastUpdated: time.Now().Add(-3 * time.Hour)},
	}}
	if n := c.PruneStale(2 * time.Hour); n != 1 {
		t.Fatalf("expected 1 evicted, got %d", n)
	}
	if _, ok := c.metrics["fresh"]; !ok {
		t.Fatalf("fresh entry wrongly evicted")
	}
	if _, ok := c.metrics["stale"]; ok {
		t.Fatalf("stale entry not evicted")
	}
}
